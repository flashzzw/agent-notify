package codexhooks

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/flashzzw/agent-notify/internal/common"
	"github.com/flashzzw/agent-notify/internal/notify"
)

// payload 描述 Codex hooks 通过 stdin 投递的事件 JSON。
// 字段与 Codex 官方 hook schema 对齐，未使用的字段也保留以便排查。
type payload struct {
	HookEventName        string          `json:"hook_event_name"`
	SessionID            string          `json:"session_id"`
	CWD                  string          `json:"cwd"`
	Model                string          `json:"model"`
	PermissionMode       string          `json:"permission_mode"`
	TurnID               string          `json:"turn_id"`
	ToolName             string          `json:"tool_name"`
	AgentID              string          `json:"agent_id"`
	AgentType            string          `json:"agent_type"`
	StopHookActive       json.RawMessage `json:"stop_hook_active"` // 容错:接受 bool 或 "true"/"false"
	LastAssistantMessage string          `json:"last_assistant_message"`
}

func ParseMessage(stdin io.Reader) (notify.Message, error) {
	var p payload
	if err := common.DecodeHookPayload(stdin, &p); err != nil {
		return notify.Message{}, err
	}

	switch p.HookEventName {
	case "SessionStart":
		// 兼容旧版 SessionStart 事件；当前首期 Dispatch 会忽略它。
		return notify.Message{
			Agent:     "codex",
			Event:     "session_start",
			SessionID: p.SessionID,
			Workspace: p.CWD,
			Title:     notify.FormatTitle("codex", "session_start"),
			Body:      notify.DefaultBody("session_start"),
		}, nil
	case "PermissionRequest":
		return notify.Message{
			Agent:     "codex",
			Event:     "permission_required",
			SessionID: p.SessionID,
			Workspace: p.CWD,
			Title:     notify.FormatTitle("codex", "permission_required"),
			Body:      fmt.Sprintf("工具: %s\n操作需要您的授权许可", fallbackToolName(p.ToolName)),
			Tool:      p.ToolName,
			Model:     p.Model,
		}, nil
	case "UserPromptSubmit":
		// 回合开始：只在「转发给 magpie 插件」时订阅，供插件计算用时；提问内容不外传。
		return notify.Message{
			Agent:     "codex",
			Event:     "turn_started",
			SessionID: p.SessionID,
			Workspace: p.CWD,
			Model:     p.Model,
		}, nil
	case "Stop":
		body := notify.DefaultBody("run_completed")
		if hint := truncateMessage(strings.TrimSpace(p.LastAssistantMessage), 200); hint != "" {
			body = hint
		}
		return notify.Message{
			Agent:     "codex",
			Event:     "run_completed",
			SessionID: p.SessionID,
			Workspace: p.CWD,
			Title:     notify.FormatTitle("codex", "run_completed"),
			Body:      body,
			Detail:    strings.TrimSpace(p.LastAssistantMessage),
			Model:     p.Model,
		}, nil
	case "Interrupt":
		// 用户 ESC 中断活跃 turn；统一归入 run_interrupted。
		return notify.Message{
			Agent:     "codex",
			Event:     "run_interrupted",
			SessionID: p.SessionID,
			Workspace: p.CWD,
			Title:     notify.FormatTitle("codex", "run_interrupted"),
			Body:      notify.DefaultBody("run_interrupted"),
			Model:     p.Model,
		}, nil
	case "SubagentStop":
		body := notify.DefaultBody("subagent_completed")
		if hint := truncateMessage(strings.TrimSpace(p.LastAssistantMessage), 200); hint != "" {
			body = hint
		}
		return notify.Message{
			Agent:     "codex",
			Event:     "subagent_completed",
			SessionID: p.SessionID,
			Workspace: p.CWD,
			Title:     notify.FormatTitle("codex", "subagent_completed"),
			Body:      body,
			Detail:    strings.TrimSpace(p.LastAssistantMessage),
			Model:     p.Model,
		}, nil
	default:
		return notify.Message{}, fmt.Errorf("unsupported hook event: %s", p.HookEventName)
	}
}

func fallbackToolName(name string) string {
	if name == "" {
		return "未知工具"
	}
	return name
}

// truncateMessage 按 rune 截断,CJK 不产生半个字符(issue #33)。
func truncateMessage(msg string, limit int) string {
	return common.TruncateRunes(msg, limit)
}
