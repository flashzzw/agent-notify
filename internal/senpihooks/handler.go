package senpihooks

import (
	"context"
	"fmt"
	"github.com/flashzzw/agent-notify/internal/agenthooks"
	"github.com/flashzzw/agent-notify/internal/config"
	"github.com/flashzzw/agent-notify/internal/state"
	"io"
)

func Handle(ctx context.Context, cfg config.Config, statePath, logPath string, r io.Reader) error {
	msg, err := ParseMessage(r)
	if err != nil {
		return state.AppendLog(logPath, fmt.Sprintf("skip event: %v", err))
	}
	return agenthooks.Dispatch(ctx, cfg, statePath, logPath, msg)
}
