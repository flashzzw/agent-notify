package magpieforward

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/flashzzw/agent-notify/internal/common"
	"github.com/flashzzw/agent-notify/internal/notify"
)

// Windows 与 WSL 的回环地址可以各自有监听。通过端点文件所在的系统选择发起请求的位置。
func (e Endpoint) viaWindows(ctx context.Context) (bool, error) {
	if !notify.IsWSL() || e.File == "" {
		return false, nil
	}
	path, err := filepath.Abs(e.File)
	if err != nil {
		return false, fmt.Errorf("magpie: 无法定位端点文件: %w", err)
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return false, fmt.Errorf("magpie: 无法定位端点文件: %w", err)
	}
	out, err := exec.CommandContext(ctx, "wslpath", "-w", path).Output()
	if err != nil {
		return false, fmt.Errorf("magpie: wslpath 无法识别端点文件所在的系统: %w", err)
	}
	path = strings.ToLower(strings.TrimSpace(string(out)))
	if strings.HasPrefix(path, `\\wsl.localhost\`) || strings.HasPrefix(path, `\\wsl$\`) {
		return false, nil
	}
	if (len(path) >= 3 && path[1:3] == `:\`) || strings.HasPrefix(path, `\\`) {
		return true, nil
	}
	return false, fmt.Errorf("magpie: wslpath 返回了无法识别的端点文件路径")
}

// requestWindows 通过 WSL 互操作从 Windows 侧访问插件。body 为 nil 时只检查 /health。
func (e Endpoint) requestWindows(ctx context.Context, body []byte) (int, error) {
	if e.pid <= 0 {
		return 0, fmt.Errorf("magpie: Windows 端点文件缺少插件进程 ID（%s）", e.File)
	}
	request := struct {
		Port  int    `json:"port"`
		PID   int    `json:"pid"`
		Token string `json:"token,omitempty"`
		Body  string `json:"body,omitempty"`
	}{Port: e.Port, PID: e.pid}
	if body != nil {
		request.Body = string(body)
		request.Token = e.Token
	}
	input, err := json.Marshal(request)
	if err != nil {
		return 0, fmt.Errorf("magpie: encode Windows request: %w", err)
	}
	cmd := exec.CommandContext(ctx, notify.PowerShellPath(), "-NoLogo", "-NoProfile", "-NonInteractive",
		"-EncodedCommand", common.EncodePowerShell(windowsRequestScript))
	cmd.Stdin = strings.NewReader(base64.StdEncoding.EncodeToString(input))
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		// PowerShell 的错误输出可能包含请求参数，只返回退出状态。
		return 0, fmt.Errorf("magpie: 无法通过 Windows 互操作连接插件服务 127.0.0.1:%d: %w", e.Port, err)
	}
	result := strings.TrimSpace(string(out))
	if result == "no-listener" {
		return 0, fmt.Errorf("magpie: Windows 端口 %d 未由端点文件中的插件进程监听（%s）", e.Port, e.File)
	}
	if result == "not-plugin" {
		return 0, fmt.Errorf("magpie: Windows 端口 %d 未返回插件健康标识", e.Port)
	}
	status, err := strconv.Atoi(result)
	if err != nil || status < 100 || status > 599 {
		return 0, fmt.Errorf("magpie: Windows 插件探测返回无效结果")
	}
	return status, nil
}

// Windows 的 localhost 可能由 wslrelay 转发回 WSL。先通过 netstat 核对监听者，
// 再建立 HTTP 连接，防止旧的 systemd 转发 socket 把连接重新送回来。
const windowsRequestScript = `$ErrorActionPreference = 'Stop'
$request = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String([Console]::In.ReadToEnd())) | ConvertFrom-Json
$address = '127.0.0.1:' + $request.port
$listener = & (Join-Path $env:SystemRoot 'System32\netstat.exe') -ano -p tcp |
    Where-Object {
        $columns = $_.Trim() -split '\s+'
        $columns.Count -eq 5 -and $columns[1] -eq $address -and $columns[2] -eq '0.0.0.0:0' -and $columns[4] -eq [string]$request.pid
    } | Select-Object -First 1
if (-not $listener) { [Console]::WriteLine('no-listener'); exit }

Add-Type -AssemblyName System.Net.Http
$handler = [Net.Http.HttpClientHandler]::new()
$handler.UseProxy = $false
$handler.AllowAutoRedirect = $false
$client = [Net.Http.HttpClient]::new($handler)
$client.Timeout = [TimeSpan]::FromSeconds(1)
$url = 'http://' + $address
if ($request.body) {
    $http = [Net.Http.HttpRequestMessage]::new([Net.Http.HttpMethod]::Post, $url + '/event')
    $http.Headers.Add('x-agent-notify-token', $request.token)
    $http.Content = [Net.Http.StringContent]::new($request.body, [Text.Encoding]::UTF8, 'application/json')
} else {
    $http = [Net.Http.HttpRequestMessage]::new([Net.Http.HttpMethod]::Get, $url + '/health')
}
try {
    $response = $client.SendAsync($http, [Net.Http.HttpCompletionOption]::ResponseHeadersRead).GetAwaiter().GetResult()
    try {
        if (-not $response.Headers.Contains('x-agent-notify') -or $response.Headers.GetValues('x-agent-notify') -notcontains '1') {
            [Console]::WriteLine('not-plugin')
        } else {
            [Console]::WriteLine([int]$response.StatusCode)
        }
    } finally { $response.Dispose() }
} finally {
    $http.Dispose()
    $client.Dispose()
}
`
