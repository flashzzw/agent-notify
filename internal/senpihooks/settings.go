package senpihooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/flashzzw/agent-notify/internal/common"
)

const marker = "agent-notify senpi extension"

const extensionTemplate = `// agent-notify senpi extension
// __MARKER__
import { spawn } from "node:child_process";
const BINARY = __BINARY__;
const CONFIG = __CONFIG__;
// 仅在「转发给 magpie 插件」时为 true：每次提问上报一次回合开始，供插件计算用时。
const TURN_EVENTS = __TURN_EVENTS__;
function send(pi, event, body, sessionID = "", extra = {}) {
  try {
    const cwd = pi.cwd ?? process.cwd();
    const args = ["handle-omo-hook"];
    if (CONFIG) args.push("--config", CONFIG);
    const child = spawn(BINARY, args, { stdio: ["pipe", "ignore", "ignore"], detached: true, windowsHide: true });
    child.on("error", () => {}); child.stdin.on("error", () => {});
    child.stdin.end(JSON.stringify({ event, session_id: sessionID, cwd, body, ...extra })); child.unref();
  } catch {}
}
// 模型最后一条回复的纯文本与模型名；形态不符时给空值，不影响事件本身。
function replyOf(message) {
  try {
    const content = message?.content;
    const text = typeof content === "string" ? content
      : Array.isArray(content) ? content.filter(part => part?.type === "text" && typeof part.text === "string").map(part => part.text).join("\n") : "";
    return { text: text.slice(0, 8000), model: typeof message?.model === "string" ? message.model : "" };
  } catch { return { text: "", model: "" }; }
}
export default function (pi) {
  let sessionID = "";
  let lastAssistant;
  const remember = (ctx) => { sessionID = ctx?.sessionManager?.getSessionId?.() ?? sessionID; };
  pi.on("session_start", (_event, ctx) => { remember(ctx); lastAssistant = undefined; });
  pi.on("agent_start", (_event, ctx) => {
    remember(ctx); lastAssistant = undefined;
    if (TURN_EVENTS && pi.sessionKind !== "worker") send(pi, "turn_started", "", sessionID);
  });
  pi.on("agent_end", (event, ctx) => {
    remember(ctx);
    lastAssistant = event.messages?.findLast(message => message.role === "assistant");
  });
  pi.on("agent_settled", (event, ctx) => {
    remember(ctx);
    const reply = replyOf(lastAssistant);
    if (pi.sessionKind === "worker") { send(pi, "subagent_completed", "", sessionID, reply); return; }
    if (lastAssistant?.stopReason === "aborted") { send(pi, "run_interrupted", "", sessionID, { model: reply.model }); return; }
    const failed = Boolean(event?.fallbackExhausted || lastAssistant?.stopReason === "error");
    send(pi, failed ? "run_failed" : "run_completed", failed ? "Agent 运行失败" : "任务已完成，请查看结果", sessionID, reply);
  });
  pi.events?.on?.("herdr:blocked", (event) => {
    if (!event?.active) return;
    send(pi, event.kind === "permission" ? "permission_required" : "input_required", event.label || "等待您的输入", sessionID, { text: event.label || "" });
  });
}
`

func render(binary string, opts common.InstallOptions) []byte {
	b, _ := json.Marshal(binary)
	c, _ := json.Marshal(opts.ConfigPath)
	t, _ := json.Marshal(opts.TurnEvents)
	s := strings.ReplaceAll(extensionTemplate, "__MARKER__", marker)
	s = strings.ReplaceAll(s, "__BINARY__", string(b))
	s = strings.ReplaceAll(s, "__TURN_EVENTS__", string(t))
	return []byte(strings.ReplaceAll(s, "__CONFIG__", string(c)))
}

// Install 写入不上报回合开始的扩展；需要时用 InstallWithOptions。
func Install(path, binary, config string) error {
	return InstallWithOptions(path, binary, common.InstallOptions{ConfigPath: config})
}

// InstallWithOptions 整体重渲染扩展文件；opts.TurnEvents 决定是否上报回合开始。
func InstallWithOptions(path, binary string, opts common.InstallOptions) error {
	if data, err := os.ReadFile(path); err == nil && !strings.HasPrefix(string(data), "// "+marker+"\n") {
		return fmt.Errorf("拒绝覆盖非 Agent Notify 扩展: %s", path)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return common.WriteFileAtomic(path, render(binary, opts), 0644)
}
func Uninstall(path string) error {
	if data, err := os.ReadFile(path); err == nil && !strings.HasPrefix(string(data), "// "+marker+"\n") {
		return fmt.Errorf("拒绝删除非 Agent Notify 扩展: %s", path)
	} else if errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
func IsInstalled(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return strings.Contains(string(data), marker), nil
}
