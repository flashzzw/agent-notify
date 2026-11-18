package notify

import (
	"strings"
	"testing"
	"time"
)

func TestApplyTemplatesExpandsAllPlaceholders(t *testing.T) {
	msg := Message{
		Agent:     "claude_code",
		Event:     "run_completed",
		SessionID: "0123456789abcdef",
		Workspace: "/home/u/学习ai编程项目/agent-notify",
		Title:     "Claude Code 运行完成",
		Body:      "任务已完成，请查看结果",
	}
	titleTemplate := "{agentName} · {event} · {project}"
	bodyTemplate := "[{session}] {message} @ {workspace} {time}"
	before := time.Now()
	got := ApplyTemplates(msg, titleTemplate, bodyTemplate)
	after := time.Now()
	if got.Title != "Claude Code · 运行完成 · agent-notify" {
		t.Fatalf("title = %q", got.Title)
	}
	if !strings.HasPrefix(got.Body, "[01234567] 任务已完成，请查看结果 @ /home/u/学习ai编程项目/agent-notify ") {
		t.Fatalf("body prefix = %q", got.Body)
	}
	// The {time} token renders with seconds; the two captures tolerate a boundary.
	if !strings.HasSuffix(got.Body, before.Format("15:04:05")) && !strings.HasSuffix(got.Body, after.Format("15:04:05")) {
		t.Fatalf("body should end with the current time, got %q", got.Body)
	}
}

func TestApplyTemplatesKeepsUnknownPlaceholders(t *testing.T) {
	got := ApplyTemplates(Message{Agent: "codex", Event: "run_completed", Body: "b"}, "{nope} {agentName}", "{agentName} {unknown}")
	if got.Title != "{nope} Codex" {
		t.Fatalf("title = %q, want unknown placeholder kept", got.Title)
	}
	if got.Body != "Codex {unknown}" {
		t.Fatalf("body = %q, want unknown placeholder kept", got.Body)
	}
}

func TestApplyTemplatesEmptyKeepsDefaults(t *testing.T) {
	msg := Message{Agent: "codex", Event: "run_completed", Title: "t", Body: "b"}
	got := ApplyTemplates(msg, "", "")
	if got != msg {
		t.Fatalf("empty templates should be a no-op, got %+v", got)
	}
	got = ApplyTemplates(msg, "", "{message}")
	if got.Title != "t" || got.Body != "b" {
		t.Fatalf("partial templates only rewrite the filled side, got %+v", got)
	}
}

func TestApplyTemplatesSinglePassNoChainedSubstitution(t *testing.T) {
	// The message body contains a literal "{event}"; it must not be re-expanded
	// after the body template is applied.
	msg := Message{Agent: "claude_code", Event: "run_completed", Body: `say "{event}"`}
	got := ApplyTemplates(msg, "{event}", "{message}")
	if got.Title != "运行完成" {
		t.Fatalf("title = %q", got.Title)
	}
	if got.Body != `say "{event}"` {
		t.Fatalf("body = %q, want literal placeholder preserved", got.Body)
	}
}

func TestApplyTemplatesEmptyWorkspaceAndShortSession(t *testing.T) {
	msg := Message{Agent: "omo", Event: "run_failed", SessionID: "abc", Body: "失败"}
	got := ApplyTemplates(msg, "{project}|{session}|{workspace}", "{project}|{session}|{workspace}")
	if got.Title != "|abc|" || got.Body != "|abc|" {
		t.Fatalf("empty workspace should render empty segments, got title=%q body=%q", got.Title, got.Body)
	}
}
