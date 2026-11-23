// Package app owns the configuration/install lifecycle used by both CLI and TUI.
package app

import (
	"fmt"
	"github.com/flashzzw/agent-notify/internal/agentintegrations"
	"github.com/flashzzw/agent-notify/internal/common"
	"github.com/flashzzw/agent-notify/internal/config"
	"os"
	"path/filepath"
)

func Integration(agent string) (agentintegrations.Integration, error) {
	switch config.NormalizeAgent(agent) {
	case "claude":
		return agentintegrations.NewClaudeIntegration(), nil
	case "codex":
		return agentintegrations.NewCodexIntegration(), nil
	case "omo":
		return agentintegrations.NewOpenCodeIntegration(), nil
	default:
		return nil, fmt.Errorf("unsupported agent %q (claude, codex, omo)", agent)
	}
}

type Setup struct {
	Agent        string
	Scope        string
	SettingsPath string
	BinaryPath   string
	Events       []string
	Channels     config.ChannelsConfig
	// Magpie 为 nil 时沿用现有的转发开关；非 nil 时显式开启或关闭。
	Magpie *bool
	// MagpieEndpointFile 非空时写入 magpie.endpoint_file（WSL 下指向 Windows 侧）。
	MagpieEndpointFile string
}

func Configure(configPath string, req Setup) (string, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return "", err
	}
	integration, err := Integration(req.Agent)
	if err != nil {
		return "", err
	}
	a, n, _ := cfg.Target(req.Agent)
	if req.Scope == "" {
		req.Scope = "user"
	}
	path := req.SettingsPath
	if path == "" {
		path, err = integration.SettingsPath(req.Scope)
		if err != nil {
			return "", err
		}
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", err
	}
	binaryPath, err := filepath.Abs(common.ResolveBinaryPath(req.BinaryPath))
	if err != nil {
		return "", err
	}
	if st, err := os.Stat(binaryPath); err != nil || st.IsDir() {
		return "", fmt.Errorf("程序不存在或不是文件: %s", binaryPath)
	}
	a.InstallScope = req.Scope
	a.BinaryPath = binaryPath
	a.InstalledPaths = config.RecordInstalledPath(a.InstalledPaths, path)
	n.Events = req.Events
	n.Channels = req.Channels
	if req.Magpie != nil {
		cfg.Magpie.Enabled = *req.Magpie
	}
	if req.MagpieEndpointFile != "" {
		cfg.Magpie.EndpointFile = req.MagpieEndpointFile
	}
	// Record the intended path before installing, so a partial failure remains discoverable/uninstallable.
	a.Enabled = false
	if err := config.Save(configPath, cfg); err != nil {
		return "", err
	}
	// 回合开始事件只有转发给 magpie 插件时才有人消费；关掉转发后重装会摘掉它。
	opts := common.InstallOptions{ConfigPath: configPath, TurnEvents: cfg.Magpie.Enabled}
	if err := integration.InstallWithOptions(path, binaryPath, opts); err != nil {
		return "", fmt.Errorf("安装未完成（已记录路径，可重试或卸载）: %w", err)
	}
	a.Enabled = true
	if err := config.Save(configPath, cfg); err != nil {
		return "", fmt.Errorf("接入已写入但启用配置未保存: %w", err)
	}
	return path, nil
}
func Uninstall(configPath, agent string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	ids := config.Agents
	if agent != "" && agent != "all" {
		if config.NormalizeAgent(agent) == "" {
			return fmt.Errorf("unsupported agent %q", agent)
		}
		ids = []string{agent}
	}
	for _, id := range ids {
		integration, _ := Integration(id)
		a, _, _ := cfg.Target(id)
		paths := append([]string(nil), a.InstalledPaths...)
		if len(paths) == 0 {
			if a.InstallScope == "project" && a.Enabled {
				return fmt.Errorf("%s 旧配置缺少项目路径，请使用 install --scope project 重新登记后卸载", id)
			}
			path, err := integration.SettingsPath("user")
			if err != nil {
				return err
			}
			paths = []string{path}
		}
		for _, path := range paths {
			if err := integration.Uninstall(path); err != nil {
				return fmt.Errorf("卸载 %s (%s): %w", id, path, err)
			}
		}
		a.Enabled = false
		a.InstalledPaths = nil
		a.BinaryPath = ""
		if err := config.Save(configPath, cfg); err != nil {
			return err
		}
	}
	return nil
}
