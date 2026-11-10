package agentintegrations

// Integration defines the interface for agent-specific integration logic.
// Each agent (Claude Code, Codex, etc.) implements this interface to provide
// detection, settings path resolution, and installation capabilities.
type Integration interface {
	// Name returns the agent's display name (e.g., "Claude Code", "Codex")
	Name() string

	// DetectInstalled checks if the agent CLI is installed on the system
	DetectInstalled() bool

	// SettingsPath returns the path to the agent's settings/config file
	// for the given scope ("user" or "project")
	SettingsPath(scope string) (string, error)

	// Install configures the agent to use agent-notify by modifying its
	// settings file at settingsPath with the given binaryPath
	Install(settingsPath, binaryPath string, configPath ...string) error

	// Uninstall removes only the hook entries written by agent-notify from
	// the settings file at settingsPath. Other hooks defined by the user are
	// preserved. Returns nil if the settings file does not exist.
	Uninstall(settingsPath string) error

	// IsHookInstalled checks if agent-notify hooks are installed in the settings file.
	// Returns true if the hooks are configured, false otherwise.
	IsHookInstalled(settingsPath string) (bool, error)
}
