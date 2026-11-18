package notify

import "testing"

func TestFormatTitle(t *testing.T) {
	tests := []struct {
		name  string
		agent string
		event string
		want  string
	}{
		{name: "claude permission", agent: "claude_code", event: "permission_required", want: "Claude Code 等待授权"},
		{name: "claude failed", agent: "claude_code", event: "run_failed", want: "Claude Code 运行失败"},
		{name: "codex completed", agent: "codex", event: "run_completed", want: "Codex 运行完成"},
		{name: "grok completed", agent: "grok", event: "run_completed", want: "Grok 运行完成"},
		{name: "grok session", agent: "grok", event: "session_start", want: "Grok 会话开始"},
	}

	for _, tt := range tests {
		if got := FormatTitle(tt.agent, tt.event); got != tt.want {
			t.Fatalf("%s: FormatTitle() = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestDefaultBody(t *testing.T) {
	tests := []struct {
		event string
		want  string
	}{
		{"run_completed", "任务已完成，请查看结果"},
		{"run_failed", "任务运行失败，请查看对话"},
		{"run_interrupted", "运行已被中断"},
		{"subagent_completed", "子代理任务已完成"},
	}
	for _, tt := range tests {
		if got := DefaultBody(tt.event); got != tt.want {
			t.Fatalf("DefaultBody(%q) = %q, want %q", tt.event, got, tt.want)
		}
	}
}

func TestFormatTitleNewEvents(t *testing.T) {
	tests := []struct {
		agent string
		event string
		want  string
	}{
		{"codex", "run_interrupted", "Codex 运行中断"},
		{"claude_code", "subagent_completed", "Claude Code 子代理完成"},
		{"omo", "subagent_completed", "Oh My OpenCode 子代理完成"},
	}
	for _, tt := range tests {
		if got := FormatTitle(tt.agent, tt.event); got != tt.want {
			t.Fatalf("FormatTitle(%q, %q) = %q, want %q", tt.agent, tt.event, got, tt.want)
		}
	}
}
