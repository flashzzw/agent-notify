package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWechatWorkSenderName(t *testing.T) {
	s := NewWechatWorkSender("https://example.com/webhook")
	if got := s.Name(); got != "wechat-work" {
		t.Fatalf("Name() = %q, want wechat-work", got)
	}
}

func TestWechatWorkSenderSendEmptyURL(t *testing.T) {
	s := NewWechatWorkSender("")
	err := s.Send(context.Background(), Message{Title: "t", Body: "b"})
	if err == nil {
		t.Fatal("Send() error = nil, want error for empty URL")
	}
	if !strings.Contains(err.Error(), "webhook_url is empty") {
		t.Fatalf("Send() error = %v, want webhook_url is empty", err)
	}
}

func TestWechatWorkSenderSendSuccess(t *testing.T) {
	var gotPayload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Errorf("Content-Type = %s, want application/json", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer srv.Close()

	s := NewWechatWorkSender(srv.URL)
	msg := Message{
		Agent:     "claude",
		Event:     "permission_required",
		SessionID: "sess-1",
		Workspace: "/path/to/project",
		Title:     "Claude Code 等待授权",
		Body:      "需要授权 xxx 操作",
	}
	if err := s.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if gotPayload["msgtype"] != "markdown" {
		t.Errorf("msgtype = %v, want markdown", gotPayload["msgtype"])
	}
	md, ok := gotPayload["markdown"].(map[string]any)
	if !ok {
		t.Fatal("markdown field missing or wrong type")
	}
	content, ok := md["content"].(string)
	if !ok || content == "" {
		t.Fatal("markdown.content is missing or empty")
	}
	if !strings.Contains(content, "Claude Code 等待授权") {
		t.Errorf("content should contain message title, got: %s", content)
	}
	if !strings.Contains(content, "需要授权 xxx 操作") {
		t.Errorf("content should contain message body, got: %s", content)
	}
	if !strings.Contains(content, "/path/to/project") {
		t.Errorf("content should contain workspace, got: %s", content)
	}
}

func TestWechatWorkSenderSendHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	s := NewWechatWorkSender(srv.URL)
	err := s.Send(context.Background(), Message{Title: "t", Body: "b"})
	if err == nil {
		t.Fatal("Send() error = nil, want error for HTTP 500")
	}
	if !strings.Contains(err.Error(), "unexpected status 500") {
		t.Errorf("Send() error = %v, want unexpected status 500", err)
	}
}

func TestWechatWorkSenderSendAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errcode":93000,"errmsg":"invalid webhook url"}`))
	}))
	defer srv.Close()

	s := NewWechatWorkSender(srv.URL)
	err := s.Send(context.Background(), Message{Title: "t", Body: "b"})
	if err == nil {
		t.Fatal("Send() error = nil, want api error")
	}
	if !strings.Contains(err.Error(), "api error") {
		t.Errorf("Send() error = %v, want api error", err)
	}
}

func TestWechatWorkRejectsMalformedSuccessAndRedactsTransportURL(t *testing.T) {
	for _, body := range []string{"not-json", `{}`, `{"errcode":null}`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
		err := NewWechatWorkSender(srv.URL+"?key=secret-value").Send(context.Background(), Message{Title: "test"})
		srv.Close()
		if err == nil {
			t.Fatalf("accepted malformed response: %s", body)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close()
	err := NewWechatWorkSender(srv.URL+"?key=secret-value").Send(context.Background(), Message{})
	if err == nil || strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("transport error leaked secret: %v", err)
	}
}

func TestBuildWechatMarkdownContainsBody(t *testing.T) {
	s := NewWechatWorkSender("https://example.com")
	msg := Message{
		Agent:     "claude",
		Event:     "run_completed",
		Title:     "运行完成",
		Body:      "任务执行成功",
		Workspace: "/workspace",
	}
	content := s.buildMarkdown(msg)
	if !strings.Contains(content, "任务执行成功") {
		t.Errorf("markdown should contain body, got: %s", content)
	}
	if !strings.Contains(content, "运行完成") {
		t.Errorf("markdown should contain eventName, got: %s", content)
	}
	if !strings.Contains(content, "/workspace") {
		t.Errorf("markdown should contain workspace, got: %s", content)
	}
}

func TestBuildWechatMarkdownOmitsWorkspaceForCodex(t *testing.T) {
	s := NewWechatWorkSender("https://example.com")
	msg := Message{
		Agent:     "codex",
		Event:     "run_completed",
		Title:     "运行完成",
		Body:      "done",
		Workspace: "/tmp/project",
	}
	content := s.buildMarkdown(msg)
	if strings.Contains(content, "/tmp/project") {
		t.Errorf("markdown should omit workspace for codex, got: %s", content)
	}
}

func TestBuildWechatMarkdownEventEmojis(t *testing.T) {
	s := NewWechatWorkSender("https://example.com")
	cases := []struct {
		event string
		emoji string
	}{
		{"permission_required", "🔐"},
		{"input_required", "⌨️"},
		{"run_completed", "✅"},
		{"run_failed", "❌"},
		{"unknown_event", "🔔"},
	}
	for _, tc := range cases {
		msg := Message{Event: tc.event, Title: "标题", Body: "内容"}
		content := s.buildMarkdown(msg)
		if !strings.Contains(content, tc.emoji) {
			t.Errorf("event %q: expected emoji %q in content: %s", tc.event, tc.emoji, content)
		}
	}
}
