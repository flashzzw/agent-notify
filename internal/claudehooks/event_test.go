package claudehooks

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flashzzw/agent-notify/internal/common"
	"github.com/flashzzw/agent-notify/internal/notify"
)

func TestParsePermissionRequest(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "hooks", "permission_request.json"))
	if err != nil {
		t.Fatal(err)
	}

	msg, err := parseMessageBytes(data)
	if err != nil {
		t.Fatalf("ParseMessage() error = %v", err)
	}
	if msg.Event != "permission_required" {
		t.Fatalf("Event = %q, want permission_required", msg.Event)
	}
	if msg.Agent != "claude_code" {
		t.Fatalf("Agent = %q, want claude_code", msg.Agent)
	}
}

func TestParseNotificationWaitingInput(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "hooks", "notification_waiting_input.json"))
	if err != nil {
		t.Fatal(err)
	}

	msg, err := parseMessageBytes(data)
	if err != nil {
		t.Fatalf("ParseMessage() error = %v", err)
	}
	if msg.Event != "input_required" {
		t.Fatalf("Event = %q, want input_required", msg.Event)
	}
	if msg.Agent != "claude_code" {
		t.Fatalf("Agent = %q, want claude_code", msg.Agent)
	}
	if msg.Body != "提示: " {
		t.Fatalf("Body = %q, want %q", msg.Body, "提示: ")
	}
}

func TestParseNotificationNeedsInputVariant(t *testing.T) {
	data := []byte(`{"hook_event_name":"Notification","session_id":"s1","cwd":"/tmp/project","message":"needs input: please confirm"}`)

	msg, err := parseMessageBytes(data)
	if err != nil {
		t.Fatalf("ParseMessage() error = %v", err)
	}
	if msg.Event != "input_required" {
		t.Fatalf("Event = %q, want input_required", msg.Event)
	}
	if msg.Agent != "claude_code" {
		t.Fatalf("Agent = %q, want claude_code", msg.Agent)
	}
	if msg.Body != "提示: please confirm" {
		t.Fatalf("Body = %q, want %q", msg.Body, "提示: please confirm")
	}
}

// issue #32:tool_response 依工具而异(MCP 返回数组、Bash 类返回字符串),
// 不能因为不是对象就丢掉整个事件。单次工具失败归入 tool_failed，不是回合失败。
func TestParseMessagePostToolUseFailureToleratesNonObjectToolResponse(t *testing.T) {
	cases := []struct {
		name string
		json string
	}{
		{"mcp content array", `{"hook_event_name":"PostToolUseFailure","session_id":"s1","cwd":"/w","tool_name":"mcp__search","tool_response":[{"type":"text","text":"boom"}]}`},
		{"string response", `{"hook_event_name":"PostToolUseFailure","session_id":"s1","cwd":"/w","tool_name":"Bash","tool_response":"command failed"}`},
		{"object with error still works", `{"hook_event_name":"PostToolUseFailure","session_id":"s1","cwd":"/w","tool_name":"Write","tool_response":{"error":"denied"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg, err := parseMessageBytes([]byte(tc.json))
			if err != nil {
				t.Fatalf("ParseMessage() error = %v, want tool_failed event", err)
			}
			if msg.Event != "tool_failed" {
				t.Fatalf("Event = %q, want tool_failed", msg.Event)
			}
		})
	}
}

// parseMessageBytes 让既有用例继续以字节数组驱动 ParseMessage,
// 后者现在从 io.Reader 流式解码(见 common.DecodeHookPayload)。
func parseMessageBytes(data []byte) (notify.Message, error) {
	return ParseMessage(bytes.NewReader(data))
}

// TestParseMessageRejectsOversizedPayload 是 issue #39 第 5 项的回归测试。
// 旧实现用 io.ReadAll 无界读取 stdin,而 hook 是每事件一进程:
// agent 传来带整份文件内容的 tool_response 时会把它整个缓冲进内存。
func TestParseMessageRejectsOversizedPayload(t *testing.T) {
	oversized := `{"hook_event_name":"Stop","tool_response":"` +
		strings.Repeat("x", common.MaxHookPayloadBytes+1024) + `"}`

	_, err := ParseMessage(strings.NewReader(oversized))

	if err == nil {
		t.Fatal("超过上限的 payload 应当被拒绝")
	}
	// 日志要指出真正的原因,而不是笼统的解析失败
	if !strings.Contains(err.Error(), "上限") {
		t.Fatalf("错误应点名上限,实际是: %v", err)
	}
}

// TestParseMessageIgnoresHugeUndeclaredToolInput tool_input 从未被读取,
// 现已从 payload 结构体移除,Decoder 会跳过它而不为其分配。
func TestParseMessageIgnoresHugeUndeclaredToolInput(t *testing.T) {
	src := `{"hook_event_name":"Stop","session_id":"s1","cwd":"/w","tool_input":{"content":"` +
		strings.Repeat("x", 1<<20) + `"}}`

	msg, err := ParseMessage(strings.NewReader(src))
	if err != nil {
		t.Fatalf("ParseMessage() error = %v", err)
	}
	if msg.Event != "run_completed" || msg.SessionID != "s1" || msg.Workspace != "/w" {
		t.Fatalf("大 tool_input 影响了其它字段: %+v", msg)
	}
}

func TestParseStopFailure(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "hooks", "stop_failure.json"))
	if err != nil {
		t.Fatal(err)
	}
	msg, err := parseMessageBytes(data)
	if err != nil {
		t.Fatalf("ParseMessage() error = %v", err)
	}
	if msg.Event != "run_failed" || msg.Agent != "claude_code" {
		t.Fatalf("Event = %q Agent = %q, want run_failed / claude_code", msg.Event, msg.Agent)
	}
	if !strings.Contains(msg.Body, "529 overloaded") {
		t.Fatalf("Body = %q, want last_assistant_message carried", msg.Body)
	}
}

func TestParseSubagentStop(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "hooks", "subagent_stop.json"))
	if err != nil {
		t.Fatal(err)
	}
	msg, err := parseMessageBytes(data)
	if err != nil {
		t.Fatalf("ParseMessage() error = %v", err)
	}
	if msg.Event != "subagent_completed" {
		t.Fatalf("Event = %q, want subagent_completed", msg.Event)
	}
	if !strings.Contains(msg.Body, "windows.go") {
		t.Fatalf("Body = %q, want last_assistant_message carried", msg.Body)
	}
	// 空 last_assistant_message 时回退默认文案。
	msg, err = parseMessageBytes([]byte(`{"hook_event_name":"SubagentStop","session_id":"s1","cwd":"/w"}`))
	if err != nil {
		t.Fatalf("ParseMessage() error = %v", err)
	}
	if msg.Event != "subagent_completed" || msg.Body != notify.DefaultBody("subagent_completed") {
		t.Fatalf("empty message should fall back to default body, got %+v", msg)
	}
}

// PostToolUseFailure 的错误文本是顶层 error 字段；工具名与错误要作为原始字段带出。
func TestParsePostToolUseFailureCarriesToolAndTopLevelError(t *testing.T) {
	msg, err := parseMessageBytes([]byte(`{"hook_event_name":"PostToolUseFailure","session_id":"s1","cwd":"/w","tool_name":"Bash","error":"Exit code 1\nError: Cannot find module 'express'","is_interrupt":false}`))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Event != "tool_failed" || msg.Tool != "Bash" || !strings.Contains(msg.Detail, "Cannot find module") {
		t.Fatalf("unexpected message %+v", msg)
	}
	if !strings.Contains(msg.Body, "工具: Bash") || !strings.Contains(msg.Body, "Exit code 1") {
		t.Fatalf("Body = %q, want tool and error", msg.Body)
	}
}

func TestParseNotificationByType(t *testing.T) {
	for _, kind := range []string{"idle_prompt", "agent_needs_input", "elicitation_dialog"} {
		msg, err := parseMessageBytes([]byte(`{"hook_event_name":"Notification","session_id":"s","cwd":"/w","message":"Claude 正在等你","notification_type":"` + kind + `"}`))
		if err != nil || msg.Event != "input_required" {
			t.Fatalf("%s: msg=%+v err=%v, want input_required", kind, msg, err)
		}
		if msg.Detail != "Claude 正在等你" {
			t.Fatalf("%s: Detail = %q, want the notification text", kind, msg.Detail)
		}
	}
	// 等待授权已由 PermissionRequest 覆盖；其余类型与输入无关。即使文案里
	// 恰好含有旧的匹配短语，也以类型为准。
	for _, kind := range []string{"permission_prompt", "auth_success", "agent_completed"} {
		if _, err := parseMessageBytes([]byte(`{"hook_event_name":"Notification","session_id":"s","cwd":"/w","message":"Claude is waiting for your input","notification_type":"` + kind + `"}`)); err == nil {
			t.Fatalf("%s should not produce a notification", kind)
		}
	}
	// 旧版本没有类型字段：回退到文案匹配。
	msg, err := parseMessageBytes([]byte(`{"hook_event_name":"Notification","session_id":"s","cwd":"/w","message":"Claude is waiting for your input"}`))
	if err != nil || msg.Event != "input_required" {
		t.Fatalf("legacy payload: msg=%+v err=%v", msg, err)
	}
}

func TestParseUserPromptSubmitStartsTurnWithoutPromptText(t *testing.T) {
	msg, err := parseMessageBytes([]byte(`{"hook_event_name":"UserPromptSubmit","session_id":"s9","cwd":"/w","prompt":"机密提问"}`))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Event != "turn_started" || msg.SessionID != "s9" || msg.Workspace != "/w" {
		t.Fatalf("unexpected message %+v", msg)
	}
	if strings.Contains(msg.Title+msg.Body+msg.Detail, "机密提问") {
		t.Fatal("prompt text must not leave the hook")
	}
}

func TestParseStopKeepsDefaultBodyAndCarriesFullReply(t *testing.T) {
	long := strings.Repeat("回复", 300)
	msg, err := parseMessageBytes([]byte(`{"hook_event_name":"Stop","session_id":"s","cwd":"/w","last_assistant_message":"` + long + `"}`))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Body != notify.DefaultBody("run_completed") {
		t.Fatalf("Body = %q, want the default body", msg.Body)
	}
	if msg.Detail != long {
		t.Fatalf("Detail has %d runes, want the untruncated reply", len([]rune(msg.Detail)))
	}
}

func TestParseStopFailureFallsBackToErrorFields(t *testing.T) {
	msg, err := parseMessageBytes([]byte(`{"hook_event_name":"StopFailure","session_id":"s","cwd":"/w","error":"rate_limit","error_details":"429 Too Many Requests"}`))
	if err != nil || msg.Event != "run_failed" || msg.Detail != "429 Too Many Requests" {
		t.Fatalf("msg=%+v err=%v", msg, err)
	}
}

// testdata/hooks/claude-2.1.322 是从真实 claude 2.1.322（`claude -p`，项目级 hook）
// 录下的载荷，只替换了会话 id 与本机路径。
func TestParseRecordedClaude2_1_322Payloads(t *testing.T) {
	read := func(name string) notify.Message {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "hooks", "claude-2.1.322", name))
		if err != nil {
			t.Fatal(err)
		}
		msg, err := parseMessageBytes(data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return msg
	}
	started := read("user_prompt_submit.json")
	if started.Event != "turn_started" || started.SessionID == "" || started.Workspace != "/home/user/demo-project" {
		t.Fatalf("turn start = %+v", started)
	}
	// 提问内容只用于让 Claude 开始回合，不进通知也不外传。
	if strings.Contains(started.Detail+started.Body+started.Title, "Reply with exactly") {
		t.Fatalf("the prompt must not travel with the turn start: %+v", started)
	}
	failed := read("stop_failure_not_logged_in.json")
	if failed.Event != "run_failed" || failed.Detail != "Not logged in · Please run /login" {
		t.Fatalf("stop failure = %+v", failed)
	}
}
