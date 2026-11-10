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
function send(pi, event, body, sessionID = "") {
  try {
    const cwd = pi.cwd ?? process.cwd();
    const args = ["handle-omo-hook"];
    if (CONFIG) args.push("--config", CONFIG);
    const child = spawn(BINARY, args, { stdio: ["pipe", "ignore", "ignore"], detached: true, windowsHide: true });
    child.on("error", () => {}); child.stdin.on("error", () => {});
    child.stdin.end(JSON.stringify({ event, session_id: sessionID, cwd, body })); child.unref();
  } catch {}
}
export default function (pi) {
  let sessionID = "";
  let lastAssistant;
  const remember = (ctx) => { sessionID = ctx?.sessionManager?.getSessionId?.() ?? sessionID; };
  pi.on("session_start", (_event, ctx) => { remember(ctx); lastAssistant = undefined; });
  pi.on("agent_start", (_event, ctx) => { remember(ctx); lastAssistant = undefined; });
  pi.on("agent_end", (event, ctx) => {
    remember(ctx);
    lastAssistant = event.messages?.findLast(message => message.role === "assistant");
  });
  pi.on("agent_settled", (event, ctx) => {
    remember(ctx);
    if (pi.sessionKind === "worker") return;
    if (lastAssistant?.stopReason === "aborted") return;
    const failed = Boolean(event?.fallbackExhausted || lastAssistant?.stopReason === "error");
    send(pi, failed ? "run_failed" : "run_completed", failed ? "Agent 运行失败" : "任务已完成，请查看结果", sessionID);
  });
  pi.events?.on?.("herdr:blocked", (event) => {
    if (!event?.active) return;
    send(pi, event.kind === "permission" ? "permission_required" : "input_required", event.label || "等待您的输入", sessionID);
  });
}
`

func render(binary, config string) []byte {
	b, _ := json.Marshal(binary)
	c, _ := json.Marshal(config)
	s := strings.ReplaceAll(extensionTemplate, "__MARKER__", marker)
	s = strings.ReplaceAll(s, "__BINARY__", string(b))
	return []byte(strings.ReplaceAll(s, "__CONFIG__", string(c)))
}
func Install(path, binary, config string) error {
	if data, err := os.ReadFile(path); err == nil && !strings.HasPrefix(string(data), "// "+marker+"\n") {
		return fmt.Errorf("拒绝覆盖非 Agent Notify 扩展: %s", path)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return common.WriteFileAtomic(path, render(binary, config), 0644)
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
