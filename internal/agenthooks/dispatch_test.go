package agenthooks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flashzzw/agent-notify/internal/config"
	"github.com/flashzzw/agent-notify/internal/notify"
	"github.com/flashzzw/agent-notify/internal/testutil"
)

// wecomRecorder 捕获企微 webhook 收到的正文，用于从公开接口验证
// Dispatch 的模板渲染行为。
type wecomRecorder struct {
	bodies []string
	server *httptest.Server
}

func newWecomRecorder(t *testing.T) *wecomRecorder {
	t.Helper()
	rec := &wecomRecorder{}
	rec.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Text struct {
				Content string `json:"content"`
			} `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		rec.bodies = append(rec.bodies, payload.Text.Content)
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	t.Cleanup(rec.server.Close)
	return rec
}

func TestDispatchRendersTemplatesForRemoteChannel(t *testing.T) {
	testutil.IsolateHome(t)
	rec := newWecomRecorder(t)
	dir := t.TempDir()

	cfg := config.Default()
	a, n, _ := cfg.Target("claude")
	a.Enabled = true
	a.InstalledPaths = []string{filepath.Join(dir, "settings.json")}
	n.Events = []string{"run_completed"}
	n.Channels.System.Enabled = false
	n.Channels.WechatWork.Enabled = true
	n.Channels.WechatWork.WebhookURL = rec.server.URL
	cfg.Behavior.TitleTemplate = "{agentName} · {event} · {project}"
	cfg.Behavior.BodyTemplate = "[{session}] {message}"

	statePath := filepath.Join(dir, "state.json")
	logPath := filepath.Join(dir, "agent-notify.log")
	msg := notify.Message{
		Agent:     "claude_code",
		Event:     "run_completed",
		SessionID: "0123456789abcdef",
		Workspace: "/home/u/学习ai编程项目/agent-notify",
		Title:     notify.FormatTitle("claude_code", "run_completed"),
		Body:      notify.DefaultBody("run_completed"),
	}
	if err := Dispatch(context.Background(), cfg, statePath, logPath, msg); err != nil {
		t.Fatal(err)
	}
	if len(rec.bodies) != 1 {
		t.Fatalf("received %d messages; want one", len(rec.bodies))
	}
	content := rec.bodies[0]
	if !strings.Contains(content, "Claude Code · 运行完成 · agent-notify") {
		t.Fatalf("templated title missing in wecom content:\n%s", content)
	}
	if !strings.Contains(content, "[01234567] 任务已完成，请查看结果") {
		t.Fatalf("templated body missing in wecom content:\n%s", content)
	}
}

func TestDispatchKeepsDefaultsWithoutTemplatesAndDropsSessionStart(t *testing.T) {
	testutil.IsolateHome(t)
	rec := newWecomRecorder(t)
	dir := t.TempDir()

	cfg := config.Default()
	a, n, _ := cfg.Target("codex")
	a.Enabled = true
	a.InstalledPaths = []string{filepath.Join(dir, "hooks.json")}
	n.Events = []string{"run_completed", "session_start"}
	n.Channels.System.Enabled = false
	n.Channels.WechatWork.Enabled = true
	n.Channels.WechatWork.WebhookURL = rec.server.URL

	statePath := filepath.Join(dir, "state.json")
	logPath := filepath.Join(dir, "agent-notify.log")
	stopMsg := notify.Message{
		Agent:     "codex",
		Event:     "run_completed",
		SessionID: "sess-1",
		Workspace: "/project",
		Title:     notify.FormatTitle("codex", "run_completed"),
		Body:      notify.DefaultBody("run_completed"),
	}
	if err := Dispatch(context.Background(), cfg, statePath, logPath, stopMsg); err != nil {
		t.Fatal(err)
	}
	sessionMsg := stopMsg
	sessionMsg.Event = "session_start"
	if err := Dispatch(context.Background(), cfg, statePath, logPath, sessionMsg); err != nil {
		t.Fatal(err)
	}
	if len(rec.bodies) != 1 {
		t.Fatalf("received %d messages; want only the Stop notification", len(rec.bodies))
	}
	// 无模板时保持默认文案（企微把默认标题改写为「Codex · 运行完成」分隔符变体）。
	if !strings.Contains(rec.bodies[0], "Codex") || !strings.Contains(rec.bodies[0], "运行完成") {
		t.Fatalf("default title missing in wecom content:\n%s", rec.bodies[0])
	}
	if !strings.Contains(rec.bodies[0], notify.DefaultBody("run_completed")) {
		t.Fatalf("default body missing in wecom content:\n%s", rec.bodies[0])
	}
}
