package cli

import (
	"context"
	"errors"
	"fmt"
	"github.com/flashzzw/agent-notify/internal/app"
	"github.com/flashzzw/agent-notify/internal/claudehooks"
	"github.com/flashzzw/agent-notify/internal/codexhooks"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/flashzzw/agent-notify/internal/config"
	"github.com/flashzzw/agent-notify/internal/magpieforward"
	"github.com/flashzzw/agent-notify/internal/notify"
	"github.com/flashzzw/agent-notify/internal/senpihooks"
	"github.com/flashzzw/agent-notify/internal/state"
	"github.com/spf13/cobra"
)

type Streams struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	root := NewRootCmd(ctx, Streams{stdin, stdout, stderr})
	root.SetArgs(args)
	err := root.Execute()
	if errors.Is(err, ErrCancelled) {
		return nil
	}
	return err
}
func NewRootCmd(ctx context.Context, s Streams) *cobra.Command {
	path := os.Getenv("AGENT_NOTIFY_CONFIG")
	if path == "" {
		path, _ = config.DefaultPath()
	}
	root := &cobra.Command{Use: "agent-notify", Short: "Claude / Codex / OMO → 企业微信与 Windows 通知", Version: Version, SilenceUsage: true, SilenceErrors: true}
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetContext(ctx)
	root.SetIn(s.Stdin)
	root.SetOut(s.Stdout)
	root.SetErr(s.Stderr)
	root.PersistentFlags().StringVar(&path, "config", path, "配置文件路径")
	absolutePath := func() (string, error) { return filepath.Abs(path) }
	root.RunE = func(cmd *cobra.Command, args []string) error {
		p, err := absolutePath()
		if err != nil {
			return err
		}
		return runMenu(cmd.Context(), s, p)
	}
	initCmd := &cobra.Command{Use: "init", Short: "交互配置 Agent、渠道与事件", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		p, err := absolutePath()
		if err != nil {
			return err
		}
		prompter, err := newPrompter(s)
		if err != nil {
			return err
		}
		return runSetup(s, p, prompter)
	}}
	root.AddCommand(initCmd, newInstallCmd(s, absolutePath), newDoctorCmd(s, absolutePath))
	configCmd := &cobra.Command{Use: "config", Short: "查看配置（Webhook 自动隐藏）"}
	configCmd.AddCommand(&cobra.Command{Use: "show", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		p, err := absolutePath()
		if err != nil {
			return err
		}
		cfg, err := config.Load(p)
		if err != nil {
			return err
		}
		data, err := cfg.Redacted()
		if err != nil {
			return err
		}
		fmt.Fprintf(s.Stdout, "配置: %s\n%s", p, data)
		return nil
	}}, &cobra.Command{Use: "path", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		p, err := absolutePath()
		if err == nil {
			fmt.Fprintln(s.Stdout, p)
		}
		return err
	}})
	root.AddCommand(configCmd)
	var uninstallAgent string
	uninstall := &cobra.Command{Use: "uninstall", Short: "卸载本工具的 Agent 接入，保留其他 hooks 与配置", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		p, err := absolutePath()
		if err != nil {
			return err
		}
		if err := app.Uninstall(p, uninstallAgent); err != nil {
			return err
		}
		fmt.Fprintln(s.Stdout, "接入已卸载，通知配置已保留。")
		return nil
	}}
	uninstall.Flags().StringVar(&uninstallAgent, "agent", "all", "claude、codex、omo 或 all")
	root.AddCommand(uninstall)
	for _, enable := range []bool{true, false} {
		verb := "disable"
		description := "关闭 Agent 通知（保留接入）"
		if enable {
			verb = "enable"
			description = "启用已安装 Agent 的通知"
		}
		var agent string
		cmd := &cobra.Command{Use: verb, Short: description, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
			p, err := absolutePath()
			if err != nil {
				return err
			}
			cfg, err := config.Load(p)
			if err != nil {
				return err
			}
			a, _, err := cfg.Target(agent)
			if err != nil {
				return err
			}
			if enable && len(a.InstalledPaths) == 0 {
				return fmt.Errorf("请先运行 init 或 install 安装接入")
			}
			a.Enabled = enable
			return config.Save(p, cfg)
		}}
		cmd.Flags().StringVar(&agent, "agent", "claude", "claude、codex、omo")
		root.AddCommand(cmd)
	}
	root.AddCommand(newSendCmd(s, absolutePath), newTestCmd(s, absolutePath))
	freeze := &cobra.Command{Use: "freeze", Short: "临时暂停企业微信通知", Args: cobra.NoArgs}
	duration := 30 * time.Minute
	freeze.Flags().DurationVar(&duration, "for", duration, "暂停时长，例如 30m、2h")
	freeze.RunE = func(cmd *cobra.Command, args []string) error {
		if duration <= 0 || duration > 7*24*time.Hour {
			return fmt.Errorf("暂停时长必须大于 0 且不超过 7 天")
		}
		p, err := absolutePath()
		if err != nil {
			return err
		}
		return state.NewFreezeStore(filepath.Join(filepath.Dir(p), "freeze.json")).Set(time.Now().Add(duration), []string{"wechat-work"}, time.Now())
	}
	root.AddCommand(freeze, &cobra.Command{Use: "unfreeze", Short: "恢复企业微信通知", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		p, err := absolutePath()
		if err != nil {
			return err
		}
		return state.NewFreezeStore(filepath.Join(filepath.Dir(p), "freeze.json")).Clear()
	}})
	for _, id := range config.Agents {
		hook := &cobra.Command{Use: "handle-" + id + "-hook", Hidden: true, Args: cobra.NoArgs}
		if id == "omo" {
			hook.Aliases = []string{"handle-opencode-hook"}
		}
		hook.RunE = func(cmd *cobra.Command, args []string) error {
			p, err := absolutePath()
			if err != nil {
				return nil
			}
			logPath := filepath.Join(filepath.Dir(p), "agent-notify.log")
			cfg, err := config.Load(p)
			if err != nil {
				_ = state.AppendLog(logPath, "hook config error: "+err.Error())
				return nil
			}
			statePath := filepath.Join(filepath.Dir(p), "state.json")
			switch id {
			case "claude":
				err = claudehooks.Handle(cmd.Context(), cfg, statePath, logPath, s.Stdin)
			case "codex":
				err = codexhooks.Handle(cmd.Context(), cfg, statePath, logPath, s.Stdin)
			case "omo":
				err = senpihooks.Handle(cmd.Context(), cfg, statePath, logPath, s.Stdin)
			}
			if err != nil {
				_ = state.AppendLog(logPath, "hook error: "+err.Error())
			}
			// Notification failures must not fail the Agent's tool or approval.
			return nil
		}
		root.AddCommand(hook)
	}
	return root
}

type pathFunc func() (string, error)

func newInstallCmd(s Streams, path pathFunc) *cobra.Command {
	var agent, scope, settings, binaryPath string
	var channels, events []string
	var magpie bool
	cmd := &cobra.Command{Use: "install", Short: "使用命令参数配置并安装接入", Args: cobra.NoArgs}
	cmd.Flags().StringVar(&agent, "agent", "claude", "claude、codex、omo")
	cmd.Flags().StringVar(&scope, "scope", "user", "user 或 project")
	cmd.Flags().StringVar(&settings, "settings", "", "自定义 Agent 配置文件路径")
	cmd.Flags().StringVar(&binaryPath, "binary", "", "持久保存的 agent-notify 程序路径")
	cmd.Flags().StringSliceVar(&channels, "channels", nil, "system,wechat-work（留空沿用原配置）")
	cmd.Flags().StringSliceVar(&events, "events", nil, "逗号分隔的通知事件（留空沿用原配置）")
	cmd.Flags().BoolVar(&magpie, "magpie", false, "把事件转发给 magpie 插件发送（--magpie=false 关闭；省略时沿用原配置）")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		p, err := path()
		if err != nil {
			return err
		}
		cfg, err := config.Load(p)
		if err != nil {
			return err
		}
		_, n, err := cfg.Target(agent)
		if err != nil {
			return err
		}
		selected := n.Channels
		if cmd.Flags().Changed("channels") {
			selected.System.Enabled = false
			selected.WechatWork.Enabled = false
			for _, ch := range channels {
				switch ch {
				case "system":
					selected.System.Enabled = true
				case "wechat-work":
					selected.WechatWork.Enabled = true
				default:
					return fmt.Errorf("unsupported channel %q", ch)
				}
			}
		}
		if webhook := os.Getenv("AGENT_NOTIFY_WECOM_WEBHOOK"); webhook != "" {
			selected.WechatWork.WebhookURL = strings.TrimSpace(webhook)
		}
		if !cmd.Flags().Changed("events") {
			events = n.Events
		}
		// 省略 --magpie 时沿用原配置：重装接入不应悄悄关掉转发。
		forwarding := cfg.Magpie.Enabled
		setup := app.Setup{Agent: agent, Scope: scope, SettingsPath: settings, BinaryPath: binaryPath, Events: events, Channels: selected}
		if cmd.Flags().Changed("magpie") {
			forwarding = magpie
			setup.Magpie = &magpie
		}
		// 转发模式下渠道由插件管理，本机不必选渠道。
		if !forwarding && !selected.System.Enabled && !selected.WechatWork.Enabled && !cmd.Flags().Changed("channels") {
			return fmt.Errorf("请选择 --channels system、wechat-work，使用 --magpie，或运行 init")
		}
		if forwarding && cfg.Magpie.EndpointFile == "" {
			setup.MagpieEndpointFile = windowsEndpointFile(cmd.Context(), s)
		}
		target, err := app.Configure(p, setup)
		if err != nil {
			return err
		}
		printSetupDone(s, agent, target)
		if forwarding {
			fmt.Fprintln(s.Stdout, "事件将转发给 magpie 插件；渠道与事件在插件页面配置。可运行 test magpie 验证。")
		}
		return nil
	}
	return cmd
}

// locateWindowsEndpointFile 可在测试里替换，避免真的调用 cmd.exe。
var locateWindowsEndpointFile = magpieforward.WindowsEndpointFile

// windowsEndpointFile 在 WSL 里定位 Windows 侧的端点文件：magpie 通常运行在
// Windows 上。找不到只提示、不中止安装——插件也可能就运行在 WSL 里，那时
// 默认位置 ~/.agent-notify 已经够用。
func windowsEndpointFile(ctx context.Context, s Streams) string {
	if !notify.IsWSL() {
		return ""
	}
	path, err := locateWindowsEndpointFile(ctx)
	if err != nil {
		fmt.Fprintf(s.Stdout, "未能定位 Windows 用户目录（%v）；如 magpie 运行在 Windows 上，请在配置里设置 magpie.endpoint_file。\n", err)
		return ""
	}
	return path
}
func printSetupDone(s Streams, agent, path string) {
	fmt.Fprintf(s.Stdout, "%s 接入已配置: %s\n", config.AgentName(agent), path)
	if config.NormalizeAgent(agent) == "codex" {
		fmt.Fprintln(s.Stdout, "请重启 Codex，并运行 /hooks 审核并信任新增/变更的 hook。")
	}
	if config.NormalizeAgent(agent) == "omo" {
		fmt.Fprintln(s.Stdout, "请在 OMO 中运行 /reload 或重启 OMO，加载 Senpi 通知扩展。")
	}
	fmt.Fprintln(s.Stdout, "请将独立包保存在固定位置；移动后重新运行 init 更新接入路径。")
}
func channelSender(cfg config.Config, agent, channel string) (notify.Sender, error) {
	_, n, err := cfg.Target(agent)
	if err != nil {
		return nil, err
	}
	switch channel {
	case "system":
		return notify.NewSystemSender(notify.DefaultRunner), nil
	case "wechat-work":
		if err := config.ValidateWebhook(n.Channels.WechatWork.WebhookURL); err != nil {
			return nil, err
		}
		return notify.NewWechatWorkSender(n.Channels.WechatWork.WebhookURL), nil
	default:
		return nil, fmt.Errorf("unsupported channel %q (system, wechat-work)", channel)
	}
}
func deliver(ctx context.Context, s Streams, path, agent, channel, title, body string) error {
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	sender, err := channelSender(cfg, agent, channel)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(cfg.Behavior.SendTimeoutSeconds)*time.Second)
	defer cancel()
	if err := sender.Send(ctx, notify.Message{Agent: config.NormalizeAgent(agent), Event: "run_completed", Title: title, Body: body}); err != nil {
		return err
	}
	fmt.Fprintln(s.Stdout, "通知已提交，请在目标渠道确认收到。")
	return nil
}
func newSendCmd(s Streams, path pathFunc) *cobra.Command {
	var agent, channel, title, message string
	cmd := &cobra.Command{Use: "send [消息]", Short: "显式发送消息（不受开关、事件或去重影响）", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			if message != "" {
				return fmt.Errorf("消息参数与 --message 只能选一个")
			}
			message = args[0]
		}
		if strings.TrimSpace(message) == "" {
			return fmt.Errorf("消息不能为空")
		}
		p, err := path()
		if err != nil {
			return err
		}
		return deliver(cmd.Context(), s, p, agent, channel, title, message)
	}}
	cmd.Flags().StringVar(&agent, "agent", "claude", "claude、codex、omo")
	cmd.Flags().StringVar(&channel, "channel", "system", "system 或 wechat-work")
	cmd.Flags().StringVar(&title, "title", "Agent Notify", "通知标题")
	cmd.Flags().StringVar(&message, "message", "", "消息正文")
	return cmd
}
func newTestCmd(s Streams, path pathFunc) *cobra.Command {
	var agent string
	cmd := &cobra.Command{Use: "test <system|wechat-work|magpie>", Short: "发送一条测试通知", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		p, err := path()
		if err != nil {
			return err
		}
		if args[0] == "magpie" {
			return forwardTest(cmd.Context(), s, p, agent)
		}
		return deliver(cmd.Context(), s, p, agent, args[0], "Agent Notify 测试", "配置成功！这是一条测试通知。")
	}}
	cmd.Flags().StringVar(&agent, "agent", "claude", "使用该 Agent 的渠道配置")
	return cmd
}

// forwardTest 走与 hook 相同的转发路径上报一个 test 事件；是否真的弹出通知
// 取决于插件里的渠道配置。
func forwardTest(ctx context.Context, s Streams, path, agent string) error {
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	if config.NormalizeAgent(agent) == "" {
		return fmt.Errorf("unsupported agent %q (claude, codex, omo)", agent)
	}
	wd, _ := os.Getwd()
	event := magpieforward.EventFromMessage(notify.Message{Agent: agent, Event: "test", Workspace: wd, Detail: "这是一条经 magpie 插件转发的测试通知。"})
	if err := magpieforward.Send(ctx, cfg.Magpie, event); err != nil {
		return err
	}
	fmt.Fprintln(s.Stdout, "测试事件已交给 magpie 插件，请在插件配置的渠道确认收到。")
	return nil
}
func newDoctorCmd(s Streams, path pathFunc) *cobra.Command {
	return &cobra.Command{Use: "doctor", Short: "检查配置、接入和通知环境（不发送消息）", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		p, err := path()
		if err != nil {
			return err
		}
		return doctor(s, p)
	}}
}
func doctor(s Streams, path string) error {
	fmt.Fprintf(s.Stdout, "Agent Notify %s\n配置: %s\n", Version, path)
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("未找到配置，请运行 init")
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	var problems []string
	for _, id := range config.Agents {
		a, n, _ := cfg.Target(id)
		status := "未启用"
		if a.Enabled {
			status = "接入已写入（需在 Agent 内确认加载）"
			integration, _ := app.Integration(id)
			paths := a.InstalledPaths
			if len(paths) == 0 {
				p, err := integration.SettingsPath(a.InstallScope)
				if err == nil {
					paths = []string{p}
				}
			}
			if len(paths) == 0 {
				problems = append(problems, config.AgentName(id)+": 缺少接入路径")
			}
			for _, p := range paths {
				ok, err := integration.IsHookInstalled(p)
				if err != nil || !ok {
					problems = append(problems, fmt.Sprintf("%s: 接入缺失或不可读: %s", config.AgentName(id), p))
					status = "接入异常"
				}
			}
			if a.BinaryPath != "" {
				if st, err := os.Stat(a.BinaryPath); err != nil || st.IsDir() {
					problems = append(problems, config.AgentName(id)+": 程序路径失效，请重新安装接入")
					status = "程序路径失效"
				}
			}
			if n.Channels.System.Enabled {
				if err := notify.SystemStatus(); err != nil {
					problems = append(problems, config.AgentName(id)+": "+err.Error())
				}
			}
		}
		fmt.Fprintf(s.Stdout, "%s: %s | Windows=%t 企业微信=%t | 事件=%s\n", config.AgentName(id), status, n.Channels.System.Enabled, n.Channels.WechatWork.Enabled, strings.Join(n.Events, ","))
	}
	if err := notify.SystemStatus(); err != nil {
		fmt.Fprintln(s.Stdout, "Windows 通知:", err)
	} else {
		fmt.Fprintln(s.Stdout, "Windows 通知: PowerShell 可用；请用 test system 验证桌面显示。")
	}
	if problem := magpieStatus(s, cfg.Magpie); problem != "" {
		problems = append(problems, problem)
	}
	fmt.Fprintln(s.Stdout, "Codex 接入变更后需 /hooks 信任；OMO 使用 Senpi Extension 接口。")
	fmt.Fprintln(s.Stdout, "日志:", filepath.Join(filepath.Dir(path), "agent-notify.log"))
	if len(problems) > 0 {
		return fmt.Errorf("诊断发现问题:\n%s", strings.Join(problems, "\n"))
	}
	return nil
}

// magpieStatus 打印转发状态并返回需要用户处理的问题（没有则为空串）。
// 只读端点文件并探测端口，不上报任何事件。
func magpieStatus(s Streams, cfg config.MagpieConfig) string {
	if !cfg.Enabled {
		fmt.Fprintln(s.Stdout, "magpie 转发: 未开启")
		return ""
	}
	endpoint, err := magpieforward.Resolve(cfg)
	if err != nil {
		fmt.Fprintln(s.Stdout, "magpie 转发: 已开启 | 端点文件未找到")
		return "magpie 转发: " + err.Error()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := magpieforward.Reachable(ctx, endpoint); err != nil {
		fmt.Fprintf(s.Stdout, "magpie 转发: 已开启 | 端点文件=%s | 端口 %d 无法连接\n", endpoint.File, endpoint.Port)
		return fmt.Sprintf("magpie 转发: 插件服务端口 %d 无法连接（magpie 未运行或插件未启用）；事件会降级为本机 Windows 通知", endpoint.Port)
	}
	fmt.Fprintf(s.Stdout, "magpie 转发: 已开启 | 端点文件=%s | 端口 %d 可连接；请用 test magpie 验证发送。\n", endpoint.File, endpoint.Port)
	return ""
}
func selectedChannels(c config.ChannelsConfig) []string {
	var result []string
	if c.System.Enabled {
		result = append(result, "system")
	}
	if c.WechatWork.Enabled {
		result = append(result, "wechat-work")
	}
	return result
}
func has(items []string, item string) bool { return slices.Contains(items, item) }
