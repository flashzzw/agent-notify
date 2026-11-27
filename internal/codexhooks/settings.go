package codexhooks

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/flashzzw/agent-notify/internal/common"
)

// hookCommandMarker 用于识别本插件写入的 Codex hook。
const hookCommandMarker = "handle-codex-hook"

// managedEvents 是本插件托管的 Codex 事件列表。
// PermissionRequest / Stop 对应项目里的 permission_required / run_completed。
// 旧版配置可能仍包含 SessionStart；当前首期安装不再订阅它。
// Interrupt / SubagentStop 为 v0.2 新增订阅：已安装用户需重跑 install 增量补齐。
var managedEvents = []string{
	"PermissionRequest",
	"Stop",
	"Interrupt",
	"SubagentStop",
}

// BuildHookSettings 生成 Codex hooks.json 所需的 settings 结构。
// turnEvent 只在「转发给 magpie 插件」时订阅：它标记回合开始，让插件能算出
// 回合用时。未开启转发时每次提问白起一个进程，所以安装时会主动摘掉它。
const turnEvent = "UserPromptSubmit"

func BuildHookSettings(binaryPath string) map[string]any {
	binaryPath = common.ResolveBinaryPath(binaryPath)
	command := buildHookCommand(binaryPath, "")

	buildEntry := func() []map[string]any {
		return []map[string]any{
			{
				"hooks": []map[string]any{
					{
						"type":    "command",
						"command": command,
					},
				},
			},
		}
	}

	hooks := map[string]any{}
	for _, event := range managedEvents {
		hooks[event] = buildEntry()
	}
	return map[string]any{"hooks": hooks}
}

// Install 以增量方式写入 hooks.json：已存在的 agent-notify hook 会更新命令，
// 不覆盖用户自己挂载的其他 hook。同时确保 config.toml 中 [features] hooks = true。
func Install(path string, binaryPath string, configPath ...string) error {
	opts := common.InstallOptions{}
	if len(configPath) > 0 {
		opts.ConfigPath = configPath[0]
	}
	return InstallWithOptions(path, binaryPath, opts)
}

// InstallWithOptions 与 Install 相同，另按 opts.TurnEvents 订阅或摘掉回合开始事件。
func InstallWithOptions(path string, binaryPath string, opts common.InstallOptions) error {
	settings, err := common.ReadOrderedSettings(path)
	if err != nil {
		return err
	}

	binaryPath = common.ResolveBinaryPath(binaryPath)
	command := buildHookCommand(binaryPath, opts.ConfigPath)

	hooks, err := common.ChildObject(settings, "hooks")
	if err != nil {
		return err
	}
	if err := common.RemoveManagedEvent(&hooks, "SessionStart", hookCommandMarker); err != nil {
		return err
	}
	events := managedEvents
	if opts.TurnEvents {
		events = append(append([]string(nil), managedEvents...), turnEvent)
	} else if err := common.RemoveManagedEvent(&hooks, turnEvent, hookCommandMarker); err != nil {
		return err
	}
	if err := common.InstallManagedHooks(&hooks, events, hookCommandMarker, command,
		common.RefuseNonArrayEvent("hooks")); err != nil {
		return err
	}
	if err := common.SetChildObject(&settings, "hooks", hooks); err != nil {
		return err
	}

	if err := common.WriteOrderedSettings(path, settings); err != nil {
		return err
	}

	// 同步启用 config.toml 中的 [features] hooks = true。
	// 失败必须显式上抛:features.hooks 未开启时 Codex 不会执行任何 hook,
	// 静默吞掉会让向导显示「安装成功」而集成实际不可用(issue #31)。
	configTomlPath := filepath.Join(filepath.Dir(path), "config.toml")
	return EnableHooksFeature(configTomlPath)
}

func buildHookCommand(binaryPath, configPath string) string {
	quotePath := common.QuotePathForShell
	prefix := ""
	if runtime.GOOS == "windows" {
		quotePath = quotePowerShellPath
		prefix = "& "
	}
	command := prefix + quotePath(binaryPath) + " " + hookCommandMarker
	if configPath != "" {
		command += " --config " + quotePath(configPath)
	}
	return command
}

var powerShellSingleQuotes = strings.NewReplacer("'", "''", "‘", "‘‘", "’", "’’", "‚", "‚‚", "‛", "‛‛")

func quotePowerShellPath(path string) string {
	return "'" + powerShellSingleQuotes.Replace(path) + "'"
}

// IsInstalled 检查 hooks.json 中是否已挂载 agent-notify 的 hook。
func IsInstalled(path string) (bool, error) {
	settings, err := common.ReadOrderedSettings(path)
	if err != nil {
		return false, err
	}
	hooks, err := common.ChildObject(settings, "hooks")
	if err != nil {
		return false, nil
	}
	return common.HasManagedHook(hooks, managedEvents, hookCommandMarker), nil
}

// Uninstall 仅移除本插件写入的 hook 条目（command 含 handle-codex-hook）。
// config.toml 中的 [features] hooks 开关不动 —— 那是通用开关，可能被其他 hook 使用。
// 文件不存在时是 no-op。
func Uninstall(path string) error {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	}

	settings, err := common.ReadOrderedSettings(path)
	if err != nil {
		return err
	}
	if _, ok := settings.Get("hooks"); !ok {
		return nil
	}
	hooks, err := common.ChildObject(settings, "hooks")
	if err != nil {
		return nil
	}

	if err := common.UninstallManagedHooks(&hooks, hookCommandMarker); err != nil {
		return err
	}

	if hooks.Len() == 0 {
		settings.Delete("hooks")
	} else if err := common.SetChildObject(&settings, "hooks", hooks); err != nil {
		return err
	}

	return common.WriteOrderedSettings(path, settings)
}
