package claudehooks

import (
	"errors"
	"os"

	"github.com/flashzzw/agent-notify/internal/common"
)

// hookCommandMarker 用于识别本插件写入的 hook。
// 卸载 / 增量安装时按此子串匹配 command 字段。
const hookCommandMarker = "handle-claude-hook"

// managedEvents 是本插件托管的 Claude Code 事件列表。
// 旧版配置可能仍包含 SessionStart；当前首期安装不再订阅它。
// StopFailure / SubagentStop 为 v0.2 新增订阅：已安装用户需重跑 install 增量补齐。
var managedEvents = []string{
	"PermissionRequest",
	"Notification",
	"Stop",
	"PostToolUseFailure",
	"StopFailure",
	"SubagentStop",
}

// turnEvent 只在「转发给 magpie 插件」时订阅：它标记回合开始，让插件能算出
// 回合用时。未开启转发时每次提问白起一个进程，所以安装时会主动摘掉它。
const turnEvent = "UserPromptSubmit"

func BuildHookSettings(binaryPath string) map[string]any {
	binaryPath = common.ResolveBinaryPath(binaryPath)
	command := common.QuotePathForShell(binaryPath) + " " + hookCommandMarker

	entry := func() []map[string]any {
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
	for _, name := range managedEvents {
		hooks[name] = entry()
	}
	return map[string]any{"hooks": hooks}
}

// Install 以增量方式写入 hooks：若某事件下已存在 agent-notify 的 hook 则跳过，
// 不覆盖用户自己挂载的其他 hook。
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
	command := common.QuotePathForShell(binaryPath) + " " + hookCommandMarker + common.ConfigArgument([]string{opts.ConfigPath})

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

	return common.WriteOrderedSettings(path, settings)
}

// IsInstalled 检查 settings 中是否已挂载 agent-notify 的 hook。
// 只要任一托管事件下存在标记命令就视为已安装。
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

// Uninstall 仅移除本插件写入的 hook 条目（command 含 handle-claude-hook）。
// 用户挂在同一事件下的其他 hook 原样保留。文件不存在时是 no-op。
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
		// hooks 不是对象:不是我们写的形态,不动它
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
