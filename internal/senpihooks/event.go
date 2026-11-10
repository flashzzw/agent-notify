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
}

func ParseMessage(r io.Reader) (notify.Message, error) {
	var p payload
	if err := common.DecodeHookPayload(r, &p); err != nil {
		return notify.Message{}, err
	}
	switch strings.TrimSpace(p.Event) {
	case "permission_required", "input_required", "run_completed", "run_failed":
		if p.Body == "" {
			p.Body = notify.DefaultBody(p.Event)
		}
		return notify.Message{Agent: "omo", Event: p.Event, SessionID: p.SessionID, Workspace: p.CWD, Title: notify.FormatTitle("omo", p.Event), Body: p.Body}, nil
	default:
		return notify.Message{}, fmt.Errorf("unsupported senpi event: %s", p.Event)
	}
}
