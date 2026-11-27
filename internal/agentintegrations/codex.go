package agentintegrations

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/flashzzw/agent-notify/internal/codexhooks"
	"github.com/flashzzw/agent-notify/internal/common"
)

// CodexIntegration implements Integration for Codex.
type CodexIntegration struct{}

// NewCodexIntegration creates a new Codex integration.
func NewCodexIntegration() *CodexIntegration {
	return &CodexIntegration{}
}

// Name returns the display name for Codex.
func (c *CodexIntegration) Name() string {
	return "Codex"
}

// DetectInstalled checks if the codex CLI is installed.
func (c *CodexIntegration) DetectInstalled() bool {
	_, err := exec.LookPath("codex")
	return err == nil
}

// SettingsPath returns the path to Codex's hooks.json file.
// Codex 同时支持 ~/.codex/hooks.json 与 ~/.codex/config.toml 内联 [hooks]；
// 这里统一使用 hooks.json，结构上与 Claude settings.json 对齐，便于维护。
func (c *CodexIntegration) SettingsPath(scope string) (string, error) {
	switch scope {
	case "user":
		if dir := os.Getenv("CODEX_HOME"); dir != "" {
			return filepath.Join(dir, "hooks.json"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".codex", "hooks.json"), nil
	case "project":
		return filepath.Join(".codex", "hooks.json"), nil
	default:
		return "", fmt.Errorf("unsupported scope: %s", scope)
	}
}

// Install 更新 Codex hooks.json 中由 agent-notify 管理的 hook。
func (c *CodexIntegration) Install(settingsPath, binaryPath string, configPath ...string) error {
	return codexhooks.Install(settingsPath, common.ResolveBinaryPath(binaryPath), configPath...)
}

// InstallWithOptions 同 Install，另可订阅回合开始事件。
func (c *CodexIntegration) InstallWithOptions(settingsPath, binaryPath string, opts common.InstallOptions) error {
	return codexhooks.InstallWithOptions(settingsPath, common.ResolveBinaryPath(binaryPath), opts)
}

// Uninstall removes only the hook entries written by agent-notify from
// Codex's hooks.json. User-defined hooks are preserved. The
// [features] hooks toggle in config.toml is NOT removed — it is a generic
// switch other hooks may depend on.
func (c *CodexIntegration) Uninstall(settingsPath string) error {
	return codexhooks.Uninstall(settingsPath)
}

// IsHookInstalled 检查 Codex hooks.json 中是否已经登记了 handle-codex-hook。
func (c *CodexIntegration) IsHookInstalled(settingsPath string) (bool, error) {
	return codexhooks.IsInstalled(settingsPath)
}
