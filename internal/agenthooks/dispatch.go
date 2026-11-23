package agenthooks

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/flashzzw/agent-notify/internal/config"
	"github.com/flashzzw/agent-notify/internal/magpieforward"
	"github.com/flashzzw/agent-notify/internal/notify"
	"github.com/flashzzw/agent-notify/internal/state"
)

// turnStarted 只用来让 magpie 插件计算回合用时，本身永远不是一条通知。
const turnStarted = "turn_started"

// newSystemSender 返回本机 Windows Toast 发送器；系统通知不可用时返回错误。
// 做成包级变量是为了让测试替换掉真实的 PowerShell 调用。
var newSystemSender = func() (notify.Sender, error) {
	if err := notify.SystemStatus(); err != nil {
		return nil, err
	}
	return notify.NewSystemSender(notify.DefaultRunner), nil
}

func Dispatch(ctx context.Context, cfg config.Config, statePath, logPath string, msg notify.Message) error {
	// Old SessionStart hooks remain harmless until the user reinstalls the integration.
	if msg.Event == "session_start" {
		return nil
	}
	a, n, err := cfg.Target(msg.Agent)
	if err != nil || !a.Enabled {
		return nil
	}
	msg.Workspace = notify.ResolveWorkspace(msg.Workspace)
	subscribed := slices.Contains(n.Events, msg.Event)
	if cfg.Magpie.Enabled {
		// 转发模式：事件过滤、文案、去重都由插件决定，这里不看本机的 events。
		forwardErr := magpieforward.Send(ctx, cfg.Magpie, magpieforward.EventFromMessage(msg))
		if forwardErr == nil {
			return nil
		}
		// 回合开始只用于计时，每次提问都有一条：插件不在时不值得各记一行日志。
		if msg.Event == turnStarted {
			return nil
		}
		logErr := state.AppendLog(logPath, fmt.Sprintf("magpie forward error agent=%s event=%s: %v", msg.Agent, msg.Event, forwardErr))
		// 降级契约：插件不可达时，本机订阅了的事件仍弹一条 Windows Toast——
		// 漏掉「等待授权」的代价远大于多一条本地通知。企业微信需要凭据且由
		// 插件统一管理，不参与降级。
		if !subscribed {
			return logErr
		}
		sender, err := newSystemSender()
		if err != nil {
			return state.AppendLog(logPath, fmt.Sprintf("magpie fallback unavailable agent=%s event=%s: %v", msg.Agent, msg.Event, err))
		}
		return send(ctx, cfg, statePath, logPath, msg, []notify.Sender{sender})
	}
	if msg.Event == turnStarted || !subscribed {
		return nil
	}
	var senders []notify.Sender
	if n.Channels.System.Enabled {
		senders = append(senders, notify.NewSystemSender(notify.DefaultRunner))
	}
	if n.Channels.WechatWork.Enabled {
		senders = append(senders, notify.NewWechatWorkSender(n.Channels.WechatWork.WebhookURL))
	}
	return send(ctx, cfg, statePath, logPath, msg, senders)
}

// send 是本机发送路径：渲染模板、应用冻结、去重并发送。
func send(ctx context.Context, cfg config.Config, statePath, logPath string, msg notify.Message, senders []notify.Sender) error {
	// 模板在 Dispatch 层渲染：三个 hooks 解析器只产默认文案，两个渠道共享
	// 模板后的 Title/Body（企微排版逻辑不变，自然生效）。
	msg = notify.ApplyTemplates(msg, cfg.Behavior.TitleTemplate, cfg.Behavior.BodyTemplate)
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
