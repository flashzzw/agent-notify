package config

import (
	"errors"
	"fmt"
	"github.com/flashzzw/agent-notify/internal/common"
	"gopkg.in/yaml.v3"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const currentConfigVersion = 3

var Agents = []string{"claude", "codex", "omo"}

type Config struct {
	Version  int            `yaml:"version"`
	Agent    AgentConfig    `yaml:"agent"`
	Notify   NotifyConfig   `yaml:"notify"`
	Behavior BehaviorConfig `yaml:"behavior"`
	// Legacy fields round-trip without being executable in the first-release runtime.
	Extra map[string]any `yaml:",inline"`
}
type AgentConfig struct {
	ClaudeCode AgentTargetConfig `yaml:"claude_code"`
	Codex      AgentTargetConfig `yaml:"codex"`
	OMO        AgentTargetConfig `yaml:"omo"`
	Extra      map[string]any    `yaml:",inline"`
}
type AgentTargetConfig struct {
	Enabled        bool     `yaml:"enabled"`
	InstallScope   string   `yaml:"install_scope"`
	InstalledPaths []string `yaml:"installed_paths,omitempty"`
	BinaryPath     string   `yaml:"binary_path,omitempty"`
}
type NotifyConfig struct {
	ClaudeCode AgentNotifyConfig `yaml:"claude_code"`
	Codex      AgentNotifyConfig `yaml:"codex"`
	OMO        AgentNotifyConfig `yaml:"omo"`
	Extra      map[string]any    `yaml:",inline"`
}
type AgentNotifyConfig struct {
	Events   []string       `yaml:"events"`
	Channels ChannelsConfig `yaml:"channels"`
}
type ChannelsConfig struct {
	System     SystemChannelConfig     `yaml:"system"`
	WechatWork WechatWorkChannelConfig `yaml:"wechat_work"`
	Extra      map[string]any          `yaml:",inline"`
}
type SystemChannelConfig struct {
	Enabled bool `yaml:"enabled"`
}
type WechatWorkChannelConfig struct {
	Enabled    bool   `yaml:"enabled"`
	WebhookURL string `yaml:"webhook_url,omitempty"`
}
type BehaviorConfig struct {
	DedupeSeconds      int    `yaml:"dedupe_seconds"`
	SendTimeoutSeconds int    `yaml:"send_timeout_seconds"`
	Locale             string `yaml:"locale"`
}

func AgentName(agent string) string {
	switch agent {
	case "claude", "claude_code":
		return "Claude Code"
	case "codex":
		return "Codex"
	case "omo", "opencode":
		return "Oh My OpenCode"
	default:
		return agent
	}
}
func NormalizeAgent(agent string) string {
	switch strings.ToLower(strings.TrimSpace(agent)) {
	case "claude", "claude_code":
		return "claude"
	case "codex":
		return "codex"
	case "omo", "opencode":
		return "omo"
	default:
		return ""
	}
}

// Target centralizes aliases and prevents an unknown agent falling back to Claude.
func (c *Config) Target(agent string) (*AgentTargetConfig, *AgentNotifyConfig, error) {
	switch NormalizeAgent(agent) {
	case "claude":
		return &c.Agent.ClaudeCode, &c.Notify.ClaudeCode, nil
	case "codex":
		return &c.Agent.Codex, &c.Notify.Codex, nil
	case "omo":
		return &c.Agent.OMO, &c.Notify.OMO, nil
	default:
		return nil, nil, fmt.Errorf("unsupported agent %q (claude, codex, omo)", agent)
	}
}
func Events(agent string) []string {
	if NormalizeAgent(agent) == "codex" {
		return []string{"permission_required", "run_completed"}
	}
	return []string{"permission_required", "input_required", "run_completed", "run_failed"}
}
func Default() Config {
	c := Config{Version: currentConfigVersion, Behavior: BehaviorConfig{DedupeSeconds: 10, SendTimeoutSeconds: 10, Locale: "zh-CN"}}
	for _, id := range Agents {
		a, n, _ := c.Target(id)
		a.InstallScope = "user"
		n.Events = Events(id)
	}
	return c
}
func RecordInstalledPath(paths []string, path string) []string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if !slices.Contains(paths, abs) {
		paths = append(paths, abs)
	}
	return paths
}
func dataPath(name string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".agent-notify", name), nil
}
func DefaultPath() (string, error) { return dataPath("config.yaml") }
func StatePath() (string, error)   { return dataPath("state.json") }
func LogPath() (string, error)     { return dataPath("agent-notify.log") }

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, err
	}
	c := Default()
	if err := yaml.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("配置 YAML 无效: %w", err)
	}
	if c.Version > currentConfigVersion {
		return Config{}, fmt.Errorf("配置版本 %d 高于当前支持版本 %d", c.Version, currentConfigVersion)
	}
	// Existing OpenCode installations become OMO; explicit new fields win.
	var raw struct {
		Agent  map[string]yaml.Node
		Notify map[string]yaml.Node
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return Config{}, err
	}
	if _, ok := raw.Agent["omo"]; !ok {
		if old, ok := raw.Agent["opencode"]; ok {
			if err := old.Decode(&c.Agent.OMO); err != nil {
				return Config{}, err
			}
		}
	}
	if _, ok := raw.Notify["omo"]; !ok {
		if old, ok := raw.Notify["opencode"]; ok {
			if err := old.Decode(&c.Notify.OMO); err != nil {
				return Config{}, err
			}
		}
	}
	delete(c.Agent.Extra, "opencode")
	delete(c.Notify.Extra, "opencode")
	c.Version = currentConfigVersion
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}
func (c Config) Validate() error {
	if c.Version != currentConfigVersion {
		return fmt.Errorf("不支持的配置版本 %d", c.Version)
	}
	if c.Behavior.DedupeSeconds < 0 || c.Behavior.DedupeSeconds > 3600 {
		return fmt.Errorf("dedupe_seconds 必须在 0–3600 之间")
	}
	if c.Behavior.SendTimeoutSeconds < 1 || c.Behavior.SendTimeoutSeconds > 60 {
		return fmt.Errorf("send_timeout_seconds 必须在 1–60 之间")
	}
	for _, id := range Agents {
		a, n, _ := c.Target(id)
		if a.InstallScope != "user" && a.InstallScope != "project" {
			return fmt.Errorf("%s install_scope 必须为 user 或 project", id)
		}
		for _, event := range n.Events {
			if !slices.Contains(Events(id), event) {
				return fmt.Errorf("%s 不支持事件 %q", id, event)
			}
		}
		if n.Channels.WechatWork.Enabled {
			if err := ValidateWebhook(n.Channels.WechatWork.WebhookURL); err != nil {
				return fmt.Errorf("%s: %w", id, err)
			}
		}
	}
	return nil
}

// URL values are never interpolated into validation errors because query strings contain secrets.
// HTTP is accepted only for loopback test servers.
func ValidateWebhook(value string) error {
	u, err := url.Parse(value)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" {
		return fmt.Errorf("企业微信 Webhook URL 无效")
	}
	if u.Scheme == "https" && u.Hostname() == "qyapi.weixin.qq.com" && u.Path == "/cgi-bin/webhook/send" && u.Query().Get("key") != "" {
		return nil
	}
	if u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1") {
		return nil
	}
	return fmt.Errorf("请填写企业微信群机器人 HTTPS Webhook（qyapi.weixin.qq.com/cgi-bin/webhook/send?key=…）")
}
func Save(path string, c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	// The generic writer inherits source permissions. Tighten both existing
	// files before copying credentials, including backups left by older versions.
	for _, privatePath := range []string{path, path + common.BackupSuffix} {
		if err := os.Chmod(privatePath, 0600); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("protect configuration permissions: %w", err)
		}
	}
	if err := common.WriteFileAtomicWithBackup(path, data, 0600); err != nil {
		return err
	}
	return os.Chmod(path, 0600)
}

// Redacted is the safe representation for terminals and diagnostics, including legacy fields.
func (c Config) Redacted() ([]byte, error) {
	data, err := yaml.Marshal(c)
	if err != nil {
		return nil, err
	}
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, err
	}
	var walk func(*yaml.Node)
	walk = func(n *yaml.Node) {
		if n.Kind == yaml.MappingNode {
			for i := 0; i < len(n.Content); i += 2 {
				key, val := n.Content[i], n.Content[i+1]
				k := strings.ToLower(key.Value)
				if strings.Contains(k, "url") || strings.Contains(k, "token") || strings.Contains(k, "secret") || strings.Contains(k, "key") {
					if val.Kind != yaml.ScalarNode || val.Value != "" {
						*val = yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "[已隐藏]"}
					}
				} else {
					walk(val)
				}
			}
		} else {
			for _, child := range n.Content {
				walk(child)
			}
		}
	}
	walk(&node)
	return yaml.Marshal(&node)
}
