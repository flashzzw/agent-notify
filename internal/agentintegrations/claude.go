package agentintegrations

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/flashzzw/agent-notify/internal/claudehooks"
	"github.com/flashzzw/agent-notify/internal/common"
)

// ClaudeIntegration implements Integration for Claude Code.
type ClaudeIntegration struct{}

// NewClaudeIntegration creates a new Claude Code integration.
func NewClaudeIntegration() *ClaudeIntegration {
	return &ClaudeIntegration{}
}

// Name returns the display name for Claude Code.
func (c *ClaudeIntegration) Name() string {
	return "Claude Code"
}

// DetectInstalled checks if the claude CLI (or CCB) is installed.
func (c *ClaudeIntegration) DetectInstalled() bool {
	if _, err := exec.LookPath("claude"); err == nil {
		return true
	}
	_, err := exec.LookPath("ccb")
	return err == nil
}

// SettingsPath returns the path to Claude Code's settings.json file.
func (c *ClaudeIntegration) SettingsPath(scope string) (string, error) {
	switch scope {
	case "user":
		if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
			return filepath.Join(dir, "settings.json"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".claude", "settings.json"), nil
	case "project":
		return filepath.Join(".claude", "settings.json"), nil
	default:
		return "", fmt.Errorf("unsupported scope: %s", scope)
	}
}

// Install configures Claude Code to use agent-notify by setting up hooks.
// 已存在 agent-notify hook 的事件会被跳过；用户挂载的其他 hook 原样保留。
func (c *ClaudeIntegration) Install(settingsPath, binaryPath string, configPath ...string) error {
	return claudehooks.Install(settingsPath, common.ResolveBinaryPath(binaryPath), configPath...)
}

// Uninstall removes only the hook entries written by agent-notify from
// Claude Code's settings.json. User-defined hooks are preserved.
func (c *ClaudeIntegration) Uninstall(settingsPath string) error {
	return claudehooks.Uninstall(settingsPath)
}

// IsHookInstalled checks if agent-notify hooks are installed in the settings file.
func (c *ClaudeIntegration) IsHookInstalled(settingsPath string) (bool, error) {
	return claudehooks.IsInstalled(settingsPath)
}
