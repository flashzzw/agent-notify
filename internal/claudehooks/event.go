package claudehooks

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/flashzzw/agent-notify/internal/common"
	"github.com/flashzzw/agent-notify/internal/notify"
)

type payload struct {
	HookEventName string `json:"hook_event_name"`
	SessionID     string `json:"session_id"`
	CWD           string `json:"cwd"`
	Message       string `json:"message"`
	// Notification 的类型（permission_prompt、idle_prompt、agent_needs_input 等）。
	// 旧版本没有这个字段，此时回退到按英文文案判断。
	NotificationType string `json:"notification_type"`
	ToolName         string `json:"tool_name"`
	// tool_response / tool_input 依工具而异(对象/字符串/数组),用 RawMessage
	// 容错解析,单字段类型意外不丢整个事件(issue #32)
	ToolResponse json.RawMessage `json:"tool_response"`
	// PostToolUseFailure 的错误文本 / StopFailure 的错误类型，均为顶层字段。
	Error        json.RawMessage `json:"error"`
	ErrorDetails json.RawMessage `json:"error_details"`
	// Stop / SubagentStop / StopFailure 携带的最终模型输出
	LastAssistantMessage string `json:"last_assistant_message"`
}

func ParseMessage(stdin io.Reader) (notify.Message, error) {
	var p payload
	if err := common.DecodeHookPayload(stdin, &p); err != nil {
		return notify.Message{}, err
	}

	switch p.HookEventName {
	case "SessionStart":
		// 兼容旧版 SessionStart 事件；Dispatch 会忽略它，不产生通知。
		return notify.Message{
			Agent:     "claude_code",
			Event:     "session_start",
			SessionID: p.SessionID,
			Workspace: p.CWD,
			Title:     notify.FormatTitle("claude_code", "session_start"),
			Body:      notify.DefaultBody("session_start"),
		}, nil
	case "PermissionRequest":
		return notify.Message{
			Agent:     "claude_code",
			Event:     "permission_required",
			SessionID: p.SessionID,
			Workspace: p.CWD,
			Title:     notify.FormatTitle("claude_code", "permission_required"),
			Body:      fmt.Sprintf("工具: %s\n操作需要您的授权许可", p.ToolName),
			Tool:      p.ToolName,
		}, nil
	case "Notification":
		if isInputRequiredNotification(p.NotificationType, p.Message) {
			// Extract a cleaner hint from the message
			hint := extractInputHint(p.Message)
			return notify.Message{
				Agent:     "claude_code",
				Event:     "input_required",
				SessionID: p.SessionID,
				Workspace: p.CWD,
				Title:     notify.FormatTitle("claude_code", "input_required"),
				Body:      fmt.Sprintf("提示: %s", hint),
				Detail:    hint,
			}, nil
		}
		if p.NotificationType != "" {
			return notify.Message{}, fmt.Errorf("unsupported notification type: %s", p.NotificationType)
		}
		return notify.Message{}, fmt.Errorf("unsupported notification message: %s", p.Message)
	case "UserPromptSubmit":
		// 用户提交了新的提问：回合开始。只在「转发给 magpie 插件」时订阅，
		// 用来让插件计算回合用时；提问内容不外传。
		return notify.Message{
			Agent:     "claude_code",
			Event:     "turn_started",
			SessionID: p.SessionID,
			Workspace: p.CWD,
		}, nil
	case "Stop":
		return notify.Message{
			Agent:     "claude_code",
			Event:     "run_completed",
			SessionID: p.SessionID,
			Workspace: p.CWD,
			Title:     notify.FormatTitle("claude_code", "run_completed"),
			Body:      notify.DefaultBody("run_completed"),
			Detail:    strings.TrimSpace(p.LastAssistantMessage),
		}, nil
	case "StopFailure":
		// turn 因 API 错误终止，归入 run_failed。此时 last_assistant_message
		// 是界面上显示的错误文本，缺失时退回 error_details / error 类型。
		body := notify.DefaultBody("run_failed")
		if hint := truncateLastAssistantMessage(p.LastAssistantMessage); hint != "" {
			body = hint
		}
		detail := strings.TrimSpace(p.LastAssistantMessage)
		for _, raw := range []json.RawMessage{p.ErrorDetails, p.Error} {
			if detail == "" {
				detail = lenientString(raw)
			}
		}
		return notify.Message{
			Agent:     "claude_code",
			Event:     "run_failed",
			SessionID: p.SessionID,
			Workspace: p.CWD,
			Title:     notify.FormatTitle("claude_code", "run_failed"),
			Body:      body,
			Detail:    detail,
		}, nil
	case "SubagentStop":
		body := notify.DefaultBody("subagent_completed")
		if hint := truncateLastAssistantMessage(p.LastAssistantMessage); hint != "" {
			body = hint
		}
		return notify.Message{
			Agent:     "claude_code",
			Event:     "subagent_completed",
			SessionID: p.SessionID,
			Workspace: p.CWD,
			Title:     notify.FormatTitle("claude_code", "subagent_completed"),
			Body:      body,
			Detail:    strings.TrimSpace(p.LastAssistantMessage),
		}, nil
	case "PostToolUseFailure":
		// 单次工具失败不是回合失败（回合失败是 StopFailure），独立成 tool_failed，
		// 默认不订阅：模型通常会自己重试，逐条通知只是噪音。
		errMsg := common.TruncateRunes(lenientString(p.Error), 200)
		if errMsg == "" {
			errMsg = extractErrorMessage(common.LenientObject(p.ToolResponse))
		}
		return notify.Message{
			Agent:     "claude_code",
			Event:     "tool_failed",
			SessionID: p.SessionID,
			Workspace: p.CWD,
			Title:     notify.FormatTitle("claude_code", "tool_failed"),
			Body:      fmt.Sprintf("工具: %s\n错误: %s", p.ToolName, errMsg),
			Tool:      p.ToolName,
			Detail:    errMsg,
		}, nil
	default:
		return notify.Message{}, fmt.Errorf("unsupported hook event: %s", p.HookEventName)
	}
}

// truncateLastAssistantMessage 截取最终输出作为通知正文,rune 安全(issue #33)。
func truncateLastAssistantMessage(msg string) string {
	return common.TruncateRunes(strings.TrimSpace(msg), 200)
}

// lenientString 读取「应当是字符串」的字段；类型意外时返回空串而不是丢掉整个事件。
func lenientString(raw json.RawMessage) string {
	var s string
	if len(raw) == 0 || json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

// isInputRequiredNotification 判断一条 Notification 是否意味着「等你输入」。
// 有 notification_type 时按类型判断：permission_prompt 不算——等待授权已由
// PermissionRequest hook 覆盖，这里再报一次就是重复通知。没有类型字段的旧
// 版本回退到按英文文案匹配。
func isInputRequiredNotification(kind, msg string) bool {
	switch strings.TrimSpace(kind) {
	case "idle_prompt", "agent_needs_input", "elicitation_dialog":
		return true
	case "":
	default:
		return false
	}
	msg = strings.ToLower(strings.TrimSpace(msg))
	return strings.Contains(msg, "waiting for your input") ||
		strings.Contains(msg, "waiting for input") ||
		strings.HasPrefix(msg, "needs input")
}

func extractInputHint(msg string) string {
	// Try to extract meaningful content after common prefixes
	msg = strings.TrimSpace(msg)

	// Remove common prefixes
	prefixes := []string{
		"claude is waiting for your input",
		"waiting for your input: ",
		"waiting for input: ",
		"needs input: ",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(strings.ToLower(msg), prefix) {
			return strings.TrimSpace(msg[len(prefix):])
		}
	}

	// If message is too long, truncate it (rune-safe, issue #33)
	return common.TruncateRunes(msg, 100)
}

// extractErrorMessage extracts error message from tool response
func extractErrorMessage(response map[string]any) string {
	if response == nil {
		return "未知错误"
	}

	if err, ok := response["error"]; ok {
		if errStr, ok := err.(string); ok && errStr != "" {
			return common.TruncateRunes(errStr, 200)
		}
	}

	if err, ok := response["message"]; ok {
		if errStr, ok := err.(string); ok && errStr != "" {
			return common.TruncateRunes(errStr, 200)
		}
	}

	return "操作失败"
}
