// 把一个归一化事件变成通知文案。所有渠道都用纯文本：模型回复里的
// markdown 标记先去掉再截断，企业微信和 Toast 都不渲染 markdown。
import { readFileSync } from "node:fs"
import { homedir } from "node:os"
import { join } from "node:path"

const words = {
  zh: {
    kinds: {
      run_completed: "完成", run_failed: "失败", permission_required: "等待授权", input_required: "等待输入",
      run_interrupted: "已中断", subagent_completed: "子代理完成", tool_failed: "工具失败", test: "测试通知",
    },
    took: "用时", model: "模型：", tool: "工具：", from: "来源：", h: "小时", min: "分钟", s: "秒",
    testBody: "配置成功，这是一条测试通知。",
    signedOut: (p, a) => `${p} 账号 ${a} 已登出`,
    ending: (p, a, d) => `${p} 账号 ${a} 的登录将在 ${d} 天后失效`,
    signInAgain: "请在 magpie 中重新登录。",
  },
  en: {
    kinds: {
      run_completed: "done", run_failed: "failed", permission_required: "needs approval", input_required: "needs input",
      run_interrupted: "interrupted", subagent_completed: "subagent done", tool_failed: "tool failed", test: "test",
    },
    took: "took", model: "Model: ", tool: "Tool: ", from: "From: ", h: "h", min: "min", s: "s",
    testBody: "It works. This is a test notification.",
    signedOut: (p, a) => `${p} account ${a} signed out`,
    ending: (p, a, d) => `${p} account ${a}: sign-in ends in ${d} day${d === 1 ? "" : "s"}`,
    signInAgain: "Sign in again in magpie.",
  },
}

const emoji = {
  run_completed: "✅", run_failed: "❌", permission_required: "🔐", input_required: "⌨️",
  run_interrupted: "🛑", subagent_completed: "🤖", tool_failed: "⚠️", test: "🔔",
}

const names = {
  claude: "Claude Code", "claude-desktop": "Claude Desktop", codex: "Codex", omo: "Oh My OpenCode", gemini: "Gemini CLI",
  opencode: "OpenCode", qwen: "Qwen Code", crush: "Crush", goose: "Goose", cursor: "Cursor", kimi: "Kimi Code", pi: "Pi", droid: "Droid",
}

// 回合有起止的事件才显示用时
const TIMED = new Set(["run_completed", "run_failed", "run_interrupted", "subagent_completed"])

export const agentId = (agent) => {
  const a = String(agent ?? "").trim().toLowerCase()
  return a === "claude_code" ? "claude" : a
}
export const agentName = (agent) => {
  const a = agentId(agent)
  return names[a] ?? (a ? a[0].toUpperCase() + a.slice(1) : "Agent")
}

export function duration(ms, w) {
  const s = Math.round(ms / 1000)
  if (s < 60) return `${s} ${w.s}`
  const min = Math.round(s / 60)
  if (min < 60) return `${min} ${w.min}`
  const h = Math.floor(min / 60), rest = min % 60
  return rest ? `${h} ${w.h} ${rest} ${w.min}` : `${h} ${w.h}`
}

export const projectOf = (cwd) => cwd ? String(cwd).replace(/[\\/]+$/, "").split(/[\\/]/).pop() ?? "" : ""

// magpie 自己记着它认识的 WSL 发行版；只有一个时，网关来的 WSL 请求就能写出发行版名。
let distro
function onlyDistro() {
  if (distro !== undefined) return distro
  distro = ""
  try {
    const dir = process.env.XDG_CONFIG_HOME?.trim() || join(homedir(), ".config")
    const names = Object.keys(JSON.parse(readFileSync(join(dir, "magpie", "wsl.json"), "utf8")))
    if (names.length === 1) distro = names[0]
  } catch {}
  return distro
}

// 来源：windows、wsl:<发行版>、wsl、linux 或 ""。hook 上报时由上报方说明；
// 网关来的请求只有工作目录可看，按路径形态推断。
export function originOf(cwd, platform = process.platform, env = process.env) {
  const p = String(cwd ?? "")
  const unc = p.match(/^[\\/]{2}wsl(?:\.localhost|\$)[\\/]([^\\/]+)/i)
  if (unc) return `wsl:${unc[1]}`
  if (/^[A-Za-z]:[\\/]/.test(p)) return "windows"
  if (!p.startsWith("/")) return ""
  if (platform === "win32") { const d = onlyDistro(); return d ? `wsl:${d}` : "wsl" }
  if (env.WSL_DISTRO_NAME) return `wsl:${env.WSL_DISTRO_NAME}`
  return "linux"
}
export const originKind = (origin) => String(origin ?? "").split(":")[0]
export function originLabel(origin) {
  const [kind, name] = String(origin ?? "").split(":")
  if (kind === "windows") return "Windows"
  if (kind === "wsl") return name ? `WSL · ${name}` : "WSL"
  if (kind === "linux") return "Linux"
  return ""
}

// 去掉 markdown 标记，压成一段纯文本。只处理常见写法；认不出的原样留下，
// 宁可留一个符号，也不吞掉正文。代码（围栏和行内）只去掉外面的标记，里面的
// 星号、下划线不当作强调处理。
const CODE = /(```[\s\S]*?```|`[^`\n]+`)/
export function plain(text) {
  return String(text ?? "").split(CODE).map((part, i) => {
    if (i % 2) return part.startsWith("```") ? part.slice(3, -3).replace(/^[^\n]*\n/, " ") : part.slice(1, -1)
    return part
      .replace(/```[^\n]*/g, " ")                    // 没闭合的围栏
      .replace(/!\[([^\]]*)\]\([^)]*\)/g, "$1")       // 图片
      .replace(/\[([^\]]+)\]\([^)]*\)/g, "$1")        // 链接
      .replace(/^\s{0,3}#{1,6}\s+/gm, "")             // 标题
      .replace(/^\s{0,3}>\s?/gm, "")                  // 引用
      .replace(/^\s*[-*_]{3,}\s*$/gm, " ")            // 分隔线
      .replace(/^\s*[-*+]\s+/gm, "")                  // 列表符号
      .replace(/\*\*(?=\S)([\s\S]*?\S)\*\*/g, "$1")   // 粗体
      .replace(/(^|[^\w*])\*(?=\S)([^*\n]*?\S)\*(?!\w)/g, "$1$2") // 斜体
      .replace(/~~(?=\S)([\s\S]*?\S)~~/g, "$1")       // 删除线
  }).join("").replace(/\s+/g, " ").trim()
}

// 按码点截断，不拆开一个表情或生僻字
export function cut(text, max) {
  const points = Array.from(text)
  return points.length > max ? `${points.slice(0, max).join("")}…` : text
}

// ev: { agent, kind, cwd, origin, model, tool, text, ms }
// 返回 { emoji, title, body, agent, kind, project, origin }
export function build(ev, o) {
  const w = words[o.lang] ?? words.zh
  const agent = agentId(ev.agent)
  const project = projectOf(ev.cwd)
  const title = `${agentName(agent)} ${w.kinds[ev.kind] ?? ev.kind}${project ? ` · ${project}` : ""}`
  const lines = []
  if (TIMED.has(ev.kind) && Number.isFinite(ev.ms) && ev.ms >= 0) lines.push(`${w.took} ${duration(ev.ms, w)}`)
  if (ev.tool) lines.push(`${w.tool}${cut(plain(ev.tool), 80)}`)
  if (ev.model) lines.push(`${w.model}${ev.model}`)
  const from = originLabel(ev.origin)
  if (from) lines.push(`${w.from}${from}`)
  const text = ev.kind === "test" ? w.testBody : o.includeText && o.maxTextLength ? cut(plain(ev.text), o.maxTextLength) : ""
  if (text) lines.push("", text)
  return { emoji: emoji[ev.kind] ?? "🔔", title, body: lines.join("\n"), agent, kind: ev.kind, project, origin: ev.origin ?? "" }
}

// 账号需要重新登录（magpie 的 onSignIn）。只带供应商和账号名，没有令牌和原因文本。
export function buildSignIn(ev, o, nowMs) {
  const w = words[o.lang] ?? words.zh
  const provider = String(ev.providerName || ev.provider || "magpie")
  const account = String(ev.account ?? "")
  let days = 0
  if (ev.state === "ending") {
    const at = Date.parse(ev.endsAt ?? "")
    days = Number.isFinite(at) ? Math.max(1, Math.ceil((at - nowMs) / 86_400_000)) : 1
  }
  return {
    emoji: "⚠️", title: days ? w.ending(provider, account, days) : w.signedOut(provider, account),
    body: w.signInAgain, agent: "", kind: "account", project: "", origin: "",
  }
}

// 测试用：忘掉缓存的发行版名
export const resetDistro = (value) => { distro = value }
