package agentintegrations

import (
	"fmt"
	"github.com/flashzzw/agent-notify/internal/common"
	"github.com/flashzzw/agent-notify/internal/senpihooks"
	"os"
	"os/exec"
	"path/filepath"
)

// OpenCodeIntegration is kept as the Go type name for config compatibility;
// current OMO runs this Senpi extension.
type OpenCodeIntegration struct{}

func NewOpenCodeIntegration() *OpenCodeIntegration { return &OpenCodeIntegration{} }
func (o *OpenCodeIntegration) Name() string        { return "Oh My OpenCode" }
func (o *OpenCodeIntegration) DetectInstalled() bool {
	_, a := exec.LookPath("omo")
	_, b := exec.LookPath("senpi")
	return a == nil || b == nil
}
func senpiHome() string {
	for _, key := range []string{"OMO_CODING_AGENT_DIR", "SENPI_CODING_AGENT_DIR", "SENPI_HOME"} {
		if p := os.Getenv(key); p != "" {
			return p
		}
	}
	h, _ := os.UserHomeDir()
	p := filepath.Join(h, ".omo", "agent")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return filepath.Join(h, ".senpi", "agent")
}
func (o *OpenCodeIntegration) SettingsPath(scope string) (string, error) {
	switch scope {
	case "user":
		return filepath.Join(senpiHome(), "extensions", "agent-notify.ts"), nil
	case "project":
		return filepath.Join(".senpi", "extensions", "agent-notify.ts"), nil
	default:
		return "", fmt.Errorf("unsupported scope: %s", scope)
	}
}
func (o *OpenCodeIntegration) Install(path, binary string, configPath ...string) error {
	cfg := ""
	if len(configPath) > 0 {
		cfg = configPath[0]
	}
	return senpihooks.Install(path, common.ResolveBinaryPath(binary), cfg)
}
func (o *OpenCodeIntegration) Uninstall(path string) error { return senpihooks.Uninstall(path) }
func (o *OpenCodeIntegration) IsHookInstalled(path string) (bool, error) {
	return senpihooks.IsInstalled(path)
}
