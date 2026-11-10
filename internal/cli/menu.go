package cli

import (
	"context"
	"fmt"
	"github.com/flashzzw/agent-notify/internal/app"
	"github.com/flashzzw/agent-notify/internal/config"
	"os"
	"strconv"
	"strings"
)

func agentOptions() []PromptOption {
	return []PromptOption{{Label: "Claude Code", Value: "claude"}, {Label: "Codex", Value: "codex"}, {Label: "Oh My OpenCode (OMO)", Value: "omo"}}
}
func runMenu(ctx context.Context, s Streams, path string) error {
	p, err := newPrompter(s)
	if err != nil {
		return err
	}
	fmt.Fprintf(s.Stdout, "\nAgent Notify %s\nClaude Code · Codex · OMO\n企业微信 · Windows / WSL 通知\n\n", Version)
	for {
		choice, err := p.Select("请选择操作", []PromptOption{
			{Label: "配置 Agent 和通知", Value: "setup"},
			{Label: "测试通知", Value: "test"},
			{Label: "查看配置", Value: "view"},
			{Label: "诊断", Value: "doctor"},
			{Label: "通知超时与去重", Value: "behavior"},
			{Label: "启用 / 关闭 Agent 通知", Value: "toggle"},
			{Label: "卸载 Agent 接入", Value: "uninstall"},
			{Label: "退出", Value: "quit"},
		}, "setup")
		if err != nil {
			return err
		}
		switch choice {
		case "quit":
			return nil
		case "setup":
			err = runSetup(s, path, p)
		case "view":
			var cfg config.Config
			cfg, err = config.Load(path)
			if err == nil {
				var data []byte
				data, err = cfg.Redacted()
				if err == nil {
					fmt.Fprintf(s.Stdout, "配置: %s\n%s\n", path, data)
				}
			}
		case "doctor":
			err = doctor(s, path)
		case "test":
			var id, ch string
			id, err = p.Select("使用哪个 Agent 的配置", agentOptions(), "claude")
			if err == nil {
				ch, err = p.Select("测试哪个渠道", []PromptOption{{Label: "Windows 通知", Value: "system"}, {Label: "企业微信", Value: "wechat-work"}}, "system")
			}
			if err == nil {
				err = deliver(ctx, s, path, id, ch, "Agent Notify 测试", "这是一条测试通知。")
			}
		case "behavior":
			err = runBehavior(path, p)
		case "toggle":
			var id string
			id, err = p.Select("选择 Agent", agentOptions(), "claude")
			if err == nil {
				var cfg config.Config
				cfg, err = config.Load(path)
				if err == nil {
					a, _, _ := cfg.Target(id)
					var enabled bool
					enabled, err = p.Confirm("启用通知", a.Enabled)
					if err == nil {
						if enabled && len(a.InstalledPaths) == 0 {
							err = fmt.Errorf("请先配置该 Agent 的接入")
						} else {
							a.Enabled = enabled
							err = config.Save(path, cfg)
						}
					}
				}
			}
		case "uninstall":
			var id string
			options := append(agentOptions(), PromptOption{Label: "全部首期 Agent", Value: "all"})
			id, err = p.Select("卸载哪个 Agent 的接入", options, "claude")
			if err == nil {
				var yes bool
				yes, err = p.Confirm("移除 Agent Notify 接入，保留其他 hooks 和通知配置？", false)
				if err == nil && yes {
					err = app.Uninstall(path, id)
				}
			}
		}
		if err != nil {
			if err == ErrCancelled {
				return nil
			}
			fmt.Fprintf(s.Stderr, "操作失败: %v\n\n", err)
		}
	}
}
func runSetup(s Streams, path string, p Prompter) error {
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	id, err := p.Select("选择 Agent", agentOptions(), "claude")
	if err != nil {
		return err
	}
	a, n, _ := cfg.Target(id)
	scope, err := p.Select("安装范围", []PromptOption{{Label: "当前用户（推荐）", Value: "user"}, {Label: "当前项目", Value: "project"}}, a.InstallScope)
	if err != nil {
		return err
	}
	channels, err := p.MultiSelect("通知渠道（空格选择，回车确认）", []PromptOption{{Label: "Windows 通知（支持 WSL）", Value: "system"}, {Label: "企业微信机器人", Value: "wechat-work"}}, selectedChannels(n.Channels))
	if err != nil {
		return err
	}
	if len(channels) == 0 {
		return fmt.Errorf("请至少选择一个渠道；临时关闭请使用启用 / 关闭菜单")
	}
	selected := n.Channels
	selected.System.Enabled = has(channels, "system")
	selected.WechatWork.Enabled = has(channels, "wechat-work")
	if selected.WechatWork.Enabled {
		webhook := strings.TrimSpace(os.Getenv("AGENT_NOTIFY_WECOM_WEBHOOK"))
		if webhook == "" && selected.WechatWork.WebhookURL != "" {
			keep, err := p.Confirm("已有企业微信 Webhook，继续使用？", true)
			if err != nil {
				return err
			}
			if keep {
				webhook = selected.WechatWork.WebhookURL
			}
		}
		if webhook == "" {
			webhook, err = secretInput(p, "企业微信机器人 Webhook（输入隐藏）")
			if err != nil {
				return err
			}
		}
		selected.WechatWork.WebhookURL = strings.TrimSpace(webhook)
		if err := config.ValidateWebhook(selected.WechatWork.WebhookURL); err != nil {
			return err
		}
	}
	labels := map[string]string{"permission_required": "等待授权", "input_required": "等待输入", "run_completed": "运行完成", "run_failed": "运行失败"}
	opts := []PromptOption{}
	for _, event := range config.Events(id) {
		opts = append(opts, PromptOption{Label: labels[event], Value: event})
	}
	events, err := p.MultiSelect("需要接收的事件", opts, n.Events)
	if err != nil {
		return err
	}
	if len(events) == 0 {
		return fmt.Errorf("请至少选择一个通知事件")
	}
	target, err := app.Configure(path, app.Setup{Agent: id, Scope: scope, Events: events, Channels: selected})
	if err != nil {
		return err
	}
	printSetupDone(s, id, target)
	return nil
}
func secretInput(p Prompter, message string) (string, error) {
	if secret, ok := p.(interface{ Password(string) (string, error) }); ok {
		return secret.Password(message)
	}
	return p.Input(message, "")
}
func runBehavior(path string, p Prompter) error {
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	for _, field := range []struct {
		label string
		value *int
	}{{"去重窗口（秒，0–3600）", &cfg.Behavior.DedupeSeconds}, {"发送超时（秒，1–60）", &cfg.Behavior.SendTimeoutSeconds}} {
		answer, err := p.Input(field.label, strconv.Itoa(*field.value))
		if err != nil {
			return err
		}
		value, err := strconv.Atoi(strings.TrimSpace(answer))
		if err != nil {
			return fmt.Errorf("请输入整数")
		}
		*field.value = value
	}
	return config.Save(path, cfg)
}
