package common

import "encoding/json"

// InstallOptions 是三个 Agent 接入共用的安装选项。
type InstallOptions struct {
	// ConfigPath 写进 hook 命令的 --config；留空则使用默认配置位置。
	ConfigPath string
	// TurnEvents 额外订阅「回合开始」（Claude / Codex 的 UserPromptSubmit、
	// OMO 的 agent_start）。只有转发给 magpie 插件时才有用：每次提问都会
	// 起一个进程，没人消费时纯属浪费，所以默认不订阅。
	TurnEvents bool
}

func ConfigArgument(paths []string) string {
	if len(paths) == 0 || paths[0] == "" {
		return ""
	}
	return " --config " + QuotePathForShell(paths[0])
}

// RemoveManagedEvent retires one subscription while retaining the user's other handlers.
func RemoveManagedEvent(events *OrderedObject, event, marker string) error {
	raw, ok := events.Get(event)
	if !ok {
		return nil
	}
	entries, err := HookEntries(raw)
	if err != nil {
		return err
	}
	kept := []json.RawMessage{}
	for _, entry := range entries {
		if stripped, keep := StripManagedHooks(entry, marker); keep {
			kept = append(kept, stripped)
		}
	}
	if len(kept) == 0 {
		events.Delete(event)
		return nil
	}
	encoded, err := json.Marshal(kept)
	if err != nil {
		return err
	}
	events.Set(event, encoded)
	return nil
}
