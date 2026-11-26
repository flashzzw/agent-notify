// Package magpieforward 把归一化后的 hook 事件交给 magpie 插件的本地服务。
//
// 插件是通知的中枢：渠道、事件过滤、文案、去重与用时都在它那里。本包只做
// 三件事——找到端点（端口与口令）、上报一个事件、说明自己来自哪一侧。
package magpieforward

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/flashzzw/agent-notify/internal/common"
	"github.com/flashzzw/agent-notify/internal/config"
	"github.com/flashzzw/agent-notify/internal/notify"
)

const (
	protocolVersion = 1
	tokenHeader     = "x-agent-notify-token"
	sendTimeout     = 2 * time.Second
	// 模型回复可能很长；插件会再截断排版，这里只防止把整段长文塞进请求体。
	maxTextRunes     = 4000
	endpointFileName = "magpie-endpoint.json"
)

// ErrNoEndpoint 表示找不到可用的端点文件：插件没装、没启动，或路径不对。
var ErrNoEndpoint = errors.New("未找到 magpie 插件的端点文件（插件未启动，或 magpie.endpoint_file 不正确）")

// Event 是与插件约定的上报内容；空字段不出现在请求体里。
type Event struct {
	V       int    `json:"v"`
	Agent   string `json:"agent"`
	Event   string `json:"event"`
	Session string `json:"session,omitempty"`
	CWD     string `json:"cwd,omitempty"`
	Origin  string `json:"origin,omitempty"`
	Tool    string `json:"tool,omitempty"`
	Text    string `json:"text,omitempty"`
	Model   string `json:"model,omitempty"`
	At      int64  `json:"at"`
}

// Endpoint 是解析出的上报目标。Token 只进请求头，不进任何错误或日志。
type Endpoint struct {
	File  string
	Port  int
	Token string
	pid   int
}

// EventFromMessage 把解析器产出的消息转成上报事件。
func EventFromMessage(msg notify.Message) Event {
	return Event{
		V:       protocolVersion,
		Agent:   config.NormalizeAgent(msg.Agent),
		Event:   msg.Event,
		Session: msg.SessionID,
		CWD:     msg.Workspace,
		Origin:  Origin(),
		Tool:    msg.Tool,
		Text:    common.TruncateRunes(strings.TrimSpace(msg.Detail), maxTextRunes),
		Model:   msg.Model,
		At:      time.Now().UnixMilli(),
	}
}

// Origin 说明事件来自哪一侧，让同一个群或同一块屏幕上能分清 WSL 与 Windows。
func Origin() string {
	if runtime.GOOS == "windows" {
		return "windows"
	}
	if notify.IsWSL() {
		if distro := strings.TrimSpace(os.Getenv("WSL_DISTRO_NAME")); distro != "" {
			return "wsl:" + distro
		}
		return "wsl"
	}
	return "linux"
}

// Resolve 依次尝试配置里的端点文件与本机默认位置。端点文件里的端口是插件
// 实际监听的端口，优先于配置；文件里没有有效端口时才用配置的端口。
func Resolve(cfg config.MagpieConfig) (Endpoint, error) {
	var candidates []string
	if p := strings.TrimSpace(cfg.EndpointFile); p != "" {
		candidates = append(candidates, p)
	}
	if p, err := config.MagpieEndpointPath(); err == nil {
		candidates = append(candidates, p)
	}
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var file struct {
			Port  int    `json:"port"`
			Token string `json:"token"`
			PID   int    `json:"pid"`
		}
		if json.Unmarshal(data, &file) != nil || strings.TrimSpace(file.Token) == "" {
			continue
		}
		port := file.Port
		if port < 1 || port > 65535 {
			port = cfg.Port
		}
		if port < 1 || port > 65535 {
			port = config.DefaultMagpiePort
		}
		return Endpoint{File: path, Port: port, Token: strings.TrimSpace(file.Token), pid: file.PID}, nil
	}
	return Endpoint{}, ErrNoEndpoint
}

// Send 上报一个事件。任何失败都以不含口令的错误返回，由调用方决定是否降级。
func Send(ctx context.Context, cfg config.MagpieConfig, ev Event) error {
	endpoint, err := Resolve(cfg)
	if err != nil {
		return err
	}
	body, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("magpie: encode event: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	status, err := endpoint.send(ctx, body)
	if err != nil {
		return err
	}
	switch {
	case status >= 200 && status < 300:
		return nil
	case status == http.StatusUnauthorized:
		return fmt.Errorf("magpie: 插件拒绝了口令（端点文件可能已过期: %s）", endpoint.File)
	default:
		return fmt.Errorf("magpie: 插件服务返回状态 %d", status)
	}
}

func (endpoint Endpoint) send(ctx context.Context, body []byte) (int, error) {
	viaWindows, err := endpoint.viaWindows(ctx)
	if err != nil {
		return 0, err
	}
	if viaWindows {
		return endpoint.requestWindows(ctx, body)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.url("/event"), bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("magpie: invalid endpoint")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(tokenHeader, endpoint.Token)
	resp, err := localClient.Do(req)
	if err != nil {
		// 传输层错误只含本机地址；口令在请求头里，不会出现在这里。
		return 0, fmt.Errorf("magpie: 无法连接插件服务 127.0.0.1:%d: %w", endpoint.Port, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

// Reachable 探测插件是否可达，不发送任何事件；供 doctor 使用。
func Reachable(ctx context.Context, endpoint Endpoint) error {
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	viaWindows, err := endpoint.viaWindows(ctx)
	if err != nil {
		return err
	}
	if viaWindows {
		status, err := endpoint.requestWindows(ctx, nil)
		if err != nil {
			return err
		}
		if status != http.StatusOK {
			return fmt.Errorf("magpie: 插件健康检查返回状态 %d", status)
		}
		return nil
	}
	dialer := net.Dialer{Timeout: sendTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(endpoint.Port)))
	if err != nil {
		return err
	}
	return conn.Close()
}

// WindowsEndpointFile 在 WSL 里找出 Windows 用户目录下端点文件的路径：
// magpie 通常运行在 Windows 上，插件把端点文件写在那一侧的用户目录。
func WindowsEndpointFile(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "cmd.exe", "/c", "echo %USERPROFILE%").Output()
	if err != nil {
		return "", fmt.Errorf("无法通过 cmd.exe 读取 Windows 用户目录: %w", err)
	}
	profile := strings.TrimSpace(string(out))
	if profile == "" || strings.Contains(profile, "%") {
		return "", fmt.Errorf("Windows 用户目录为空")
	}
	out, err = exec.CommandContext(ctx, "wslpath", "-u", profile).Output()
	if err != nil {
		return "", fmt.Errorf("wslpath 无法转换 Windows 用户目录: %w", err)
	}
	home := strings.TrimSpace(string(out))
	if home == "" {
		return "", fmt.Errorf("wslpath 没有返回路径")
	}
	return filepath.Join(home, ".agent-notify", endpointFileName), nil
}

func (e Endpoint) url(path string) string {
	return "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(e.Port)) + path
}

// 本机回环不该经过任何代理；显式关掉，避免环境里的 HTTP_PROXY 影响 hook。
var localClient = &http.Client{
	Timeout:   sendTimeout,
	Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true},
}
