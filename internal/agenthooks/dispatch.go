package agenthooks

import (
	"context"
	"fmt"
	"github.com/flashzzw/agent-notify/internal/config"
	"github.com/flashzzw/agent-notify/internal/notify"
	"github.com/flashzzw/agent-notify/internal/state"
	"slices"
	"time"
)

func Dispatch(ctx context.Context, cfg config.Config, statePath, logPath string, msg notify.Message) error {
	// Old SessionStart hooks remain harmless until the user reinstalls the integration.
	if msg.Event == "session_start" {
		return nil
	}
	a, n, err := cfg.Target(msg.Agent)
	if err != nil || !a.Enabled || !slices.Contains(n.Events, msg.Event) {
		return nil
	}
	msg.Workspace = notify.ResolveWorkspace(msg.Workspace)
	var senders []notify.Sender
	if n.Channels.System.Enabled {
		senders = append(senders, notify.NewSystemSender(notify.DefaultRunner))
	}
	if n.Channels.WechatWork.Enabled {
		senders = append(senders, notify.NewWechatWorkSender(n.Channels.WechatWork.WebhookURL))
	}
	freeze := state.NewFreezeStore(state.FreezePath(statePath)).Load()
	active := senders[:0]
	for _, s := range senders {
		if !freeze.Blocks(s.Name(), time.Now()) {
			active = append(active, s)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(cfg.Behavior.SendTimeoutSeconds)*time.Second)
	defer cancel()
	dispatcher := notify.NewDispatcher(state.NewStore(statePath), time.Duration(cfg.Behavior.DedupeSeconds)*time.Second, active...)
	if err := dispatcher.SendAll(ctx, msg); err != nil {
		return state.AppendLog(logPath, fmt.Sprintf("dispatch error agent=%s event=%s: %v", msg.Agent, msg.Event, err))
	}
	return nil
}
