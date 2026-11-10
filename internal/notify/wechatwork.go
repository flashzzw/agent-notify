package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"
	"unicode/utf8"
)

// WechatWorkSender sends notifications via WeChat Work (企业微信) group bot webhook.
type WechatWorkSender struct {
	webhookURL string
	httpClient *http.Client
}

// NewWechatWorkSender creates a new WechatWorkSender with the given webhook URL.
func NewWechatWorkSender(webhookURL string) *WechatWorkSender {
	return &WechatWorkSender{
		webhookURL: webhookURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *WechatWorkSender) Name() string { return "wechat-work" }

// Send uses plain text so notifications also display in WeChat.
func (s *WechatWorkSender) Send(ctx context.Context, msg Message) error {
	if s.webhookURL == "" {
		return fmt.Errorf("wechat-work: webhook_url is empty")
	}

	content := s.buildText(msg)
	payload := map[string]any{
		"msgtype": "text",
		"text": map[string]any{
			"content": content,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("wechat-work: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("wechat-work: invalid webhook request")
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		// The webhook contains the bot secret in its query string. Never include
		// the URL or the wrapped transport error in a user-facing error.
		return fmt.Errorf("wechat-work: send request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Response text can reflect the credential-bearing request URL.
		return fmt.Errorf("wechat-work: unexpected status %d", resp.StatusCode)
	}

	// Parse response to check errcode
	var result struct {
		ErrCode *int `json:"errcode"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&result); err != nil {
		return fmt.Errorf("wechat-work: invalid API response")
	}
	if result.ErrCode == nil {
		return fmt.Errorf("wechat-work: invalid API response")
	}
	if *result.ErrCode != 0 {
		return fmt.Errorf("wechat-work: api error code=%d", *result.ErrCode)
	}

	return nil
}

// buildText preserves the original notifier's layout and reserves space for
// metadata within WeCom's 2048-byte UTF-8 text limit.
func (s *WechatWorkSender) buildText(msg Message) string {
	title := msg.Title
	if title == FormatTitle(msg.Agent, msg.Event) {
		title = appDisplayName(msg.Agent) + " · " + eventDisplayName(msg.Event)
	}
	prefix := eventEmoji(msg.Event) + " " + truncateWechatText(title, 240) + "\n消息内容：\n"
	metadata := []string{"时间：" + time.Now().Format("2006-01-02 15:04:05")}
	if workspace := strings.TrimSpace(msg.Workspace); workspace != "" {
		project := path.Base(strings.ReplaceAll(workspace, `\`, "/"))
		metadata = append(metadata, "项目："+truncateWechatText(project, 160), "工作目录："+truncateWechatText(workspace, 400))
	}
	if msg.SessionID != "" {
		metadata = append(metadata, "会话："+truncateWechatText(msg.SessionID, 80))
	}
	suffix := "\n" + strings.Join(metadata, "\n")
	return prefix + truncateWechatText(msg.Body, 2048-len(prefix)-len(suffix)) + suffix
}

// Byte limits must not split a Chinese character or emoji. All callers reserve
// at least len("…") bytes, including when metadata reaches its individual caps.
func truncateWechatText(text string, maxBytes int) string {
	text = strings.ToValidUTF8(text, "�")
	if len(text) <= maxBytes {
		return text
	}
	end := maxBytes - len("…")
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}
	return text[:end] + "…"
}

func eventEmoji(event string) string {
	switch event {
	case "permission_required":
		return "🔐"
	case "input_required":
		return "⌨️"
	case "run_completed":
		return "✅"
	case "run_failed":
		return "❌"
	default:
		return "🔔"
	}
}
