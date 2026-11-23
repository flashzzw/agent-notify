package codexhooks

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flashzzw/agent-notify/internal/notify"
)

func TestParseSessionStart(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "codex-hooks", "session_start.json"))
	if err != nil {
		t.Fatal(err)
	}

	msg, err := parseMessageBytes(data)
	if err != nil {
		t.Fatalf("ParseMessage() error = %v", err)
	}
	if msg.Agent != "codex" {
		t.Fatalf("Agent = %q, want codex", msg.Agent)
	}
	// session_start 不发通知，但必须带上 session_id / cwd:
	// Dispatch 用它们把窗口快照按会话缓存，点击通知时再查回来聚焦。
	if msg.Event != "session_start" {
		t.Fatalf("Event = %q, want session_start", msg.Event)
	}
	if msg.SessionID != "019fa95b-1157-7a40-8c92-aeddffad385a" {
		t.Fatalf("SessionID = %q, want the payload session_id", msg.SessionID)
	}
	if msg.Workspace != "/tmp/demo" {
		t.Fatalf("Workspace = %q, want /tmp/demo", msg.Workspace)
	}
}

func TestParsePermissionRequest(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "codex-hooks", "permission_request.json"))
	if err != nil {
		t.Fatal(err)
	}

	msg, err := parseMessageBytes(data)
	if err != nil {
		t.Fatalf("ParseMessage() error = %v", err)
	}
	if msg.Agent != "codex" {
		t.Fatalf("Agent = %q, want codex", msg.Agent)
	}
	if msg.Event != "permission_required" {
		t.Fatalf("Event = %q, want permission_required", msg.Event)
	}
	if !strings.Contains(msg.Body, "Bash") {
		t.Fatalf("Body = %q, want tool name Bash", msg.Body)
	}
	if msg.Workspace != "/tmp/demo" {
		t.Fatalf("Workspace = %q, want /tmp/demo", msg.Workspace)
	}
}

func TestParseStop(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "codex-hooks", "stop.json"))
	if err != nil {
		t.Fatal(err)
	}

	msg, err := parseMessageBytes(data)
	if err != nil {
		t.Fatalf("ParseMessage() error = %v", err)
	}
	if msg.Agent != "codex" {
		t.Fatalf("Agent = %q, want codex", msg.Agent)
	}
	if msg.Event != "run_completed" {
		t.Fatalf("Event = %q, want run_completed", msg.Event)
	}
	// last_assistant_message 非空时应作为 Body
	if !strings.Contains(msg.Body, "cargo build") {
		t.Fatalf("Body = %q, want last_assistant_message content", msg.Body)
	}
}

func TestParseStopFallsBackToDefaultBody(t *testing.T) {
	raw := []byte(`{"hook_event_name":"Stop","session_id":"s","cwd":"/tmp","last_assistant_message":""}`)

	msg, err := parseMessageBytes(raw)
	if err != nil {
		t.Fatalf("ParseMessage() error = %v", err)
	}
	if msg.Body == "" {
		t.Fatal("Body should fall back to default when last_assistant_message empty")
	}
}

func TestParseInterrupt(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "codex-hooks", "interrupt.json"))
	if err != nil {
		t.Fatal(err)
	}
	msg, err := parseMessageBytes(data)
	if err != nil {
		t.Fatalf("ParseMessage() error = %v", err)
	}
	if msg.Event != "run_interrupted" || msg.Agent != "codex" {
		t.Fatalf("Event = %q Agent = %q, want run_interrupted / codex", msg.Event, msg.Agent)
	}
	if msg.Body != notify.DefaultBody("run_interrupted") {
		t.Fatalf("Body = %q, want default interrupted body", msg.Body)
	}
}

func TestParseSubagentStop(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "codex-hooks", "subagent_stop.json"))
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
	if !strings.Contains(msg.Body, "3 处引用") {
		t.Fatalf("Body = %q, want last_assistant_message carried", msg.Body)
	}
	// 空 last_assistant_message 时回退默认文案。
	msg, err = parseMessageBytes([]byte(`{"hook_event_name":"SubagentStop","session_id":"s","cwd":"/tmp"}`))
	if err != nil {
		t.Fatalf("ParseMessage() error = %v", err)
	}
	if msg.Event != "subagent_completed" || msg.Body != notify.DefaultBody("subagent_completed") {
		t.Fatalf("empty message should fall back to default body, got %+v", msg)
	}
}

func TestParseUnsupportedEvent(t *testing.T) {
	raw := []byte(`{"hook_event_name":"PreToolUse","session_id":"s","cwd":"/tmp"}`)

	_, err := parseMessageBytes(raw)
	if err == nil {
		t.Fatal("parseMessageBytes() expected error for unsupported event")
	}
}

func TestTruncateMessage(t *testing.T) {
	tests := []struct {
		in    string
		limit int
		want  string
	}{
		{"", 10, ""},
		{"short", 10, "short"},
		{"1234567890ab", 10, "1234567..."},
	}
	for _, tt := range tests {
		got := truncateMessage(tt.in, tt.limit)
		if got != tt.want {
			t.Fatalf("truncateMessage(%q, %d) = %q, want %q", tt.in, tt.limit, got, tt.want)
		}
	}
}

// parseMessageBytes 让既有用例继续以字节数组驱动 ParseMessage,
// 后者现在从 io.Reader 流式解码(见 common.DecodeHookPayload)。
func parseMessageBytes(data []byte) (notify.Message, error) {
	return ParseMessage(bytes.NewReader(data))
}

func TestParseUserPromptSubmitStartsTurnWithoutPromptText(t *testing.T) {
	msg, err := parseMessageBytes([]byte(`{"hook_event_name":"UserPromptSubmit","session_id":"s9","cwd":"/w","model":"gpt-5","turn_id":"t1","prompt":"机密提问"}`))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Event != "turn_started" || msg.SessionID != "s9" || msg.Model != "gpt-5" {
		t.Fatalf("unexpected message %+v", msg)
	}
	if strings.Contains(msg.Title+msg.Body+msg.Detail, "机密提问") {
		t.Fatal("prompt text must not leave the hook")
	}
}

func TestParseStopCarriesModelAndFullReply(t *testing.T) {
	long := strings.Repeat("回复", 300)
	msg, err := parseMessageBytes([]byte(`{"hook_event_name":"Stop","session_id":"s","cwd":"/w","model":"gpt-5","last_assistant_message":"` + long + `"}`))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Model != "gpt-5" || msg.Detail != long {
		t.Fatalf("Model=%q Detail runes=%d, want model and untruncated reply", msg.Model, len([]rune(msg.Detail)))
	}
	if len([]rune(msg.Body)) > 200 {
		t.Fatalf("Body should stay truncated for local channels, got %d runes", len([]rune(msg.Body)))
	}
}
