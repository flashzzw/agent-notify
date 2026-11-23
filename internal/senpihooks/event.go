package senpihooks

import (
	"fmt"
	"io"
	"strings"

	"github.com/flashzzw/agent-notify/internal/common"
	"github.com/flashzzw/agent-notify/internal/notify"
)

type payload struct {
	Event     string `json:"event"`
	SessionID string `json:"session_id"`
	CWD       string `json:"cwd"`
	Body      string `json:"body"`
	// 未经排版的原始文本与模型名，供「转发给 magpie 插件」使用。
	Text  string `json:"text"`
	Model string `json:"model"`
}

func ParseMessage(r io.Reader) (notify.Message, error) {
	var p payload
	if err := common.DecodeHookPayload(r, &p); err != nil {
		return notify.Message{}, err
	}
	switch strings.TrimSpace(p.Event) {
	case "turn_started":
		// 主会话开始处理一次提问；只在开启 magpie 转发的扩展里才会发出。
		return notify.Message{Agent: "omo", Event: p.Event, SessionID: p.SessionID, Workspace: p.CWD, Model: p.Model}, nil
	case "permission_required", "input_required", "run_completed", "run_failed", "run_interrupted", "subagent_completed":
		if p.Body == "" {
			p.Body = notify.DefaultBody(p.Event)
		}
		return notify.Message{Agent: "omo", Event: p.Event, SessionID: p.SessionID, Workspace: p.CWD, Title: notify.FormatTitle("omo", p.Event), Body: p.Body, Detail: strings.TrimSpace(p.Text), Model: p.Model}, nil
	default:
		return notify.Message{}, fmt.Errorf("unsupported senpi event: %s", p.Event)
	}
}
