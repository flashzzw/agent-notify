package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
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

	if gotPayload["msgtype"] != "text" {
		t.Errorf("msgtype = %v, want text", gotPayload["msgtype"])
	}
	md, ok := gotPayload["text"].(map[string]any)
	if !ok {
		t.Fatal("text field missing or wrong type")
	}
	content, ok := md["content"].(string)
	if !ok || content == "" {
		t.Fatal("text.content is missing or empty")
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

func TestWechatWorkErrorsDoNotExposeCredentials(t *testing.T) {
	const secret = "reflected-test-credential"
	for _, status := range []int{http.StatusBadRequest, http.StatusOK} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]any{"errcode": 93000, "errmsg": "invalid URL " + r.URL.String()})
		}))
		err := NewWechatWorkSender(srv.URL+"?key="+secret).Send(context.Background(), Message{})
		srv.Close()
		if err == nil || strings.Contains(err.Error(), secret) {
			t.Errorf("status %d: missing safe error", status)
		}
	}
	err := NewWechatWorkSender("https://example.invalid/"+secret+"%zz").Send(context.Background(), Message{})
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Error("invalid URL: missing safe error")
	}
}

func captureWechatText(t *testing.T, msg Message) string {
	t.Helper()
	var payload struct {
		Type string `json:"msgtype"`
		Text struct {
			Content string `json:"content"`
		} `json:"text"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		_, _ = w.Write([]byte(`{"errcode":0}`))
	}))
	defer srv.Close()
	if err := NewWechatWorkSender(srv.URL).Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	if payload.Type != "text" {
		t.Fatalf("msgtype = %q, want text for WeChat compatibility", payload.Type)
	}
	return payload.Text.Content
}

func TestWechatWorkTextRestoresLegacyLayout(t *testing.T) {
	for _, tc := range []struct {
		agent, event, title, wantTitle, workspace string
	}{
		{"codex", "run_completed", "Codex 运行完成", "✅ Codex · 运行完成", "/tmp/项目"},
		{"claude_code", "permission_required", "Claude Code 等待授权", "🔐 Claude Code · 等待授权", `C:\工作\项目`},
		{"omo", "input_required", "Oh My OpenCode 等待输入", "⌨️ Oh My OpenCode · 等待输入", "/tmp/项目"},
		{"claude", "run_failed", "部署失败", "❌ 部署失败", "/tmp/项目"},
	} {
		t.Run(tc.agent, func(t *testing.T) {
			content := captureWechatText(t, Message{Agent: tc.agent, Event: tc.event, Title: tc.title,
				Body: "第一行\n第二行 <原文>", Workspace: tc.workspace, SessionID: "session-123"})
			content = regexp.MustCompile(`时间：\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}`).ReplaceAllString(content, "时间：<time>")
			want := tc.wantTitle + "\n消息内容：\n第一行\n第二行 <原文>\n时间：<time>\n项目：项目\n工作目录：" + tc.workspace + "\n会话：session-123"
			if content != want {
				t.Fatalf("content = %q, want %q", content, want)
			}
		})
	}
}

func TestWechatWorkTextLimitsUTF8BytesAndKeepsMetadata(t *testing.T) {
	for _, oversizedMetadata := range []bool{false, true} {
		msg := Message{Title: "标题", Body: strings.Repeat("中文🙂", 1000), Workspace: "/tmp/项目", SessionID: "session-123"}
		if oversizedMetadata {
			msg.Title = strings.Repeat("标题", 1000)
			msg.Workspace = "/tmp/" + strings.Repeat("项目", 1000)
			msg.SessionID = strings.Repeat("会话", 1000)
		}
		content := captureWechatText(t, msg)
		if len(content) > 2048 || !utf8.ValidString(content) {
			t.Fatalf("content must fit 2048 UTF-8 bytes, got %d bytes, valid=%t", len(content), utf8.ValidString(content))
		}
		if !strings.Contains(content, "…\n时间：") || !strings.Contains(content, "\n项目：") || !strings.Contains(content, "\n工作目录：") || !strings.Contains(content, "\n会话：") {
			t.Fatalf("truncated message lost metadata: %q", content)
		}
		if !oversizedMetadata && !strings.HasSuffix(content, "\n项目：项目\n工作目录：/tmp/项目\n会话：session-123") {
			t.Fatal("ordinary metadata was truncated")
		}
	}
}

func TestWechatWorkTextPreservesCustomTitleAndOmitsMissingMetadata(t *testing.T) {
	content := captureWechatText(t, Message{Title: "自定义标题", Body: "内容"})
	if !strings.HasPrefix(content, "🔔 自定义标题\n消息内容：\n内容\n时间：") {
		t.Fatalf("unexpected content: %q", content)
	}
	for _, field := range []string{"项目：", "工作目录：", "会话："} {
		if strings.Contains(content, field) {
			t.Errorf("empty metadata %q should be omitted", field)
		}
	}
}
