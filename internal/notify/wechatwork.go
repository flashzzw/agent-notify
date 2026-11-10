package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
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

// Send sends a notification message via WeChat Work webhook using the markdown message type.
func (s *WechatWorkSender) Send(ctx context.Context, msg Message) error {
	if s.webhookURL == "" {
		return fmt.Errorf("wechat-work: webhook_url is empty")
	}

	content := s.buildMarkdown(msg)
	payload := map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]any{
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

// buildMarkdown constructs the markdown content for a WeChat Work message.
func (s *WechatWorkSender) buildMarkdown(msg Message) string {
	emoji := eventEmoji(msg.Event)
	eventName := eventDisplayName(msg.Event)
	timestamp := time.Now().Format("2006-01-02 15:04:05")

	footerText := "🤖 Agent Notify"
	if msg.Agent == "codex" {
		footerText = "🤖 Codex Agent Notify"
	}

	content := fmt.Sprintf("## %s %s\n\n", emoji, msg.Title)
	content += fmt.Sprintf(">**事件类型**：%s\n", eventName)
	content += fmt.Sprintf(">**时间**：%s\n", timestamp)
	content += fmt.Sprintf(">**消息内容**：%s\n", msg.Body)
	if msg.Workspace != "" && msg.Agent != "codex" {
		content += fmt.Sprintf(">**工作目录**：`%s`\n", msg.Workspace)
	}
	content += fmt.Sprintf("\n<font color=\"comment\">%s</font>", footerText)

	return content
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
