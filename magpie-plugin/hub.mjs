// 分发中枢：两个事件来源（网关钩子、Agent hook 上报）在这里汇合，
// 统一做过滤、用时、去重和发送。调用方拿到结果立即返回，发送在后台进行。
import { appendFile, mkdir, rename, stat } from "node:fs/promises"
import { homedir } from "node:os"
import { join } from "node:path"
import { now } from "./clock.mjs"
import { options, activeChannels, EVENTS } from "./options.mjs"
import { agentId, build, buildSignIn, originKind, originOf } from "./message.mjs"
import { send } from "./channels.mjs"

// 有 hook 上报的这段时间内，网关判定的回合结束不再发：hook 的事件更全（授权、
// 输入），两边都发就是重复。「有 hook 上报」按两种方式认：
// - 同一个会话报过。网关的 session 就是 Agent 自己的会话 id，这样认最准，也不怕
//   两边给 Agent 起的名字不同：桌面版里跑的 Claude Code，hook 报的是 claude，网关
//   可能叫它 claude-desktop。
// - 同名 Agent 在同一侧（WSL / Windows）报过。请求不带会话 id 的 Agent 只能这样认。
const HOOK_ACTIVE = 12 * 3600 * 1000
// hook 自己会报「回合失败」的 Agent（Go 端 config.Events 里有 run_failed 的那些）。
// 其余的（Codex）hook 没有失败事件：网关判定的失败对它照发，否则两边都不出声。
// Go 端给某个 Agent 加了失败事件时，这里同步加上。
const HOOK_REPORTS_FAILURE = new Set(["claude", "omo"])
// 回合结束的两种事件：两个来源都可能报同一次
const ENDS = new Set(["run_completed", "run_failed"])
// 中断的 hook 和网关看到那次请求断掉，前后差不了这么久
const INTERRUPT_SLACK = 30 * 1000
const STALE = 24 * 3600 * 1000
const MAX_KEYS = 2000
const LOG_BYTES = 1024 * 1024
// 不值得各记一行日志的跳过原因：每次提问都有的回合开始，和整个插件被关掉
const QUIET = new Set(["turn started", "no channel"])

const hookSeen = new Map()     // `${agent}|${wsl|windows|linux}` -> 最近一次 hook 上报的时间
const hookSessions = new Map() // 会话 id -> { at, agent }：最近一次 hook 上报的时间和 hook 报的 Agent
const interrupts = new Map()   // 会话 id -> hook 最近一次报「已中断」的时间
const starts = new Map()       // `${agent}|${session}` -> 回合开始时间（hook 的 turn_started）
const models = new Map()       // `s|${session}` 或 `${agent}|${cwd}` -> 网关最近看到的模型名
const sentAt = new Map()       // 去重与冷却键 -> 上次发送时间
const accountAt = new Map()    // 账号通知的冷却
const ignored = new Set()      // 已经记过日志的「被忽略的来源」
let pending = new Set()
let logFile = join(homedir(), ".agent-notify", "magpie-plugin.log")
let logTail = Promise.resolve()

// 日志只记来源、Agent、事件、每个渠道的结果和没发的原因，不记正文、目录和地址。
// 写入排成一队：行的先后就是事件的先后，轮转也不会两头同时发生。
function log(line) {
  const file = logFile
  if (!file) return
  const entry = `${new Date(now()).toISOString()} ${line}\n`
  logTail = logTail.then(async () => {
    try {
      await mkdir(join(file, ".."), { recursive: true })
      const size = await stat(file).then((s) => s.size, () => 0)
      if (size > LOG_BYTES) await rename(file, `${file}.1`).catch(() => {})
      await appendFile(file, entry)
    } catch {}
  })
}

// Agent 名来自请求的 User-Agent 或上报方，进日志前只留安全字符
const safe = (s) => String(s || "-").replace(/[^\w.\-]/g, "_").slice(0, 40)

function prune(map, t, age = STALE, at = (v) => v) {
  for (const [k, v] of map) if (t - at(v) > age) map.delete(k)
  while (map.size > MAX_KEYS) map.delete(map.keys().next().value)
}

function hash(s) {
  let h = 0x811c9dc5
  for (let i = 0; i < s.length; i++) h = Math.imul(h ^ s.charCodeAt(i), 0x01000193)
  return (h >>> 0).toString(36)
}

function dispatch(o, m, tag) {
  const names = activeChannels(o)
  const job = Promise.all(names.map(async (name) => {
    const r = await send(name, o.channels[name], m)
    return `${name}=${r.ok ? "ok" : `error(${r.error ?? "unknown"})`}`
  }))
    .then((results) => log(`${tag} ${results.join(" ")}`))
    .catch(() => {})
    .finally(() => pending.delete(job))
  pending.add(job)
}

const skipped = (reason) => ({ sent: false, reason })

// 网关每次看到请求时记下模型名：Claude 的 hook 事件里没有模型，从这里补。
// 有会话 id 就按会话记（两边给 Agent 起的名字可能不同），另按 Agent 加目录记一份。
export function noteModel(agent, cwd, model, session) {
  if (!model) return
  if (session) models.set(`s|${session}`, String(model))
  if (agent && cwd) models.set(`${agentId(agent)}|${cwd}`, String(model))
  while (models.size > MAX_KEYS) models.delete(models.keys().next().value)
}

// 来源不是本机自己的 Agent 而被网关钩子忽略的请求。每种（Agent、来源）只记一次：
// 够回答「这个会话为什么没有通知」，又不至于每个请求一行。
export function noteIgnored(agent, origin) {
  const key = `${safe(agentId(agent))}|${safe(origin)}`
  if (ignored.has(key) || ignored.size >= 100) return
  ignored.add(key)
  log(`gateway agent=${safe(agentId(agent))} origin=${safe(origin)} ignored (not this computer's own agent)`)
}

// ev: { source: "gateway" | "hook", agent, kind, session, cwd, origin, model, tool, text, ms, key }
//   kind  EVENTS 里的事件，或 hook 专有的 turn_started（只计时）、test（跳过所有过滤）
//   key   网关来源的会话键，用于冷却
// 返回 { sent, reason? }。没发的事件带原因记一行日志。
export function emit(ev, rawOptions) {
  const result = route(ev, rawOptions)
  if (!result.sent && !QUIET.has(result.reason)) {
    log(`${ev.source === "hook" ? "hook" : "gateway"} agent=${safe(agentId(ev.agent))} event=${safe(ev.kind)} skipped (${result.reason})`)
  }
  return result
}

function route(ev, rawOptions) {
  const o = options(rawOptions)
  const t = now()
  const agent = agentId(ev.agent)
  const hook = ev.source === "hook"
  const origin = ev.origin || originOf(ev.cwd)
  const side = `${agent}|${originKind(origin)}`
  const session = ev.session || ""

  if (hook) {
    hookSeen.set(side, t)
    prune(hookSeen, t)
    if (session) {
      hookSessions.set(session, { at: t, agent })
      prune(hookSessions, t, STALE, (v) => v.at)
      // 就算没订阅「已中断」也记下：网关压着的那次失败要靠它认出来
      if (ev.kind === "run_interrupted") { interrupts.set(session, t); prune(interrupts, t) }
    }
  }
  if (hook && ev.kind === "turn_started") {
    starts.set(`${agent}|${ev.session ?? ""}`, t)
    prune(starts, t)
    return skipped("turn started")
  }
  if (!activeChannels(o).length) return skipped("no channel")
  if (ev.kind === "test") {
    dispatch(o, build({ ...ev, agent, origin }, o), `test agent=${safe(agent)}`)
    return { sent: true }
  }
  if (!EVENTS.includes(ev.kind)) return skipped("unknown event")
  if (!hook) {
    // 这个回合由谁的 hook 在报：先按会话认，再按同名 Agent 的同一侧认
    const bySession = session ? hookSessions.get(session) : undefined
    const reporter = bySession && t - bySession.at < HOOK_ACTIVE ? bySession.agent
      : t - (hookSeen.get(side) ?? -Infinity) < HOOK_ACTIVE ? agent : ""
    if (reporter && (ev.kind !== "run_failed" || HOOK_REPORTS_FAILURE.has(reporter))) return skipped("covered by hooks")
    // 用户按了 Esc：回复没传完，在网关看来和上游失败没有区别，只有 hook 知道那是中断
    if (ev.kind === "run_failed" && session &&
        t - (interrupts.get(session) ?? -Infinity) < o.failureHoldSeconds * 1000 + INTERRUPT_SLACK) return skipped("interrupted")
  }
  if (o.agents.length && !o.agents.includes(agent)) return skipped("agent filtered")
  if (!o.events.includes(ev.kind)) return skipped("event off")

  let ms = ev.ms
  if (hook) {
    const start = starts.get(`${agent}|${ev.session ?? ""}`)
    ms = start === undefined ? undefined : t - start
  }
  // 太短的回合不值得打扰；等待授权和等待输入没有用时，永远即时
  if (Number.isFinite(ms) && ms < o.minSeconds * 1000 && ["run_completed", "run_failed", "run_interrupted", "subagent_completed"].includes(ev.kind)) {
    return skipped("too short")
  }

  const model = ev.model || (session && models.get(`s|${session}`)) || models.get(`${agent}|${ev.cwd ?? ""}`) || ""
  const m = build({ ...ev, agent, origin, model, ms }, o)
  // 同一个会话的这次回合结束，网关刚发过：宿主重启后中枢不记得谁有 hook，两个来源
  // 会各报一次。反过来不用查——hook 先到的话，上面按会话就认出来了。
  const across = session && ENDS.has(ev.kind) ? `x|${session}|${ev.kind}` : ""
  if (hook && across && t - (sentAt.get(across) ?? -Infinity) < o.dedupeSeconds * 1000) return skipped("duplicate")
  // 网关来源：每个会话在冷却期内只发一条。hook 来源：同一条内容短时间内不重发
  // （hook 可能被 Agent 重试），会话 id 为空时退到工作目录，避免多个实例互相吞掉。
  const key = hook
    ? `h|${agent}|${ev.session || ev.cwd || ""}|${ev.kind}|${hash(`${m.title}\n${m.body}`)}`
    : `g|${ev.key ?? `${agent}|${ev.session ?? ""}|${ev.cwd ?? ""}`}`
  const window = (hook ? o.dedupeSeconds : o.cooldownSeconds) * 1000
  if (t - (sentAt.get(key) ?? -Infinity) < window) return skipped("duplicate")
  sentAt.set(key, t)
  if (!hook && across) sentAt.set(across, t)
  prune(sentAt, t)

  dispatch(o, m, `${hook ? "hook" : "gateway"} agent=${safe(agent)} event=${ev.kind}`)
  return { sent: true }
}

// 网关里一个会话换了键（压缩后继续）时，把冷却记录带过去
export function moveCooldown(from, to) {
  const at = sentAt.get(`g|${from}`)
  if (at !== undefined) sentAt.set(`g|${to}`, at)
}

// magpie 的 onSignIn：账号要重新登录，或 Claude 登录即将失效。
export function emitSignIn(ev, rawOptions) {
  const o = options(rawOptions)
  if (!o.accounts || !activeChannels(o).length || !ev || typeof ev !== "object") return skipped("off")
  if (ev.state !== "signed-out" && ev.state !== "ending") return skipped("not a sign-in event")
  const t = now()
  const key = `${ev.provider ?? ""}|${String(ev.account ?? "").toLowerCase()}|${ev.state}`
  if (t - (accountAt.get(key) ?? -Infinity) < o.accountCooldownMinutes * 60_000) return skipped("duplicate")
  accountAt.set(key, t)
  prune(accountAt, t)
  dispatch(o, buildSignIn(ev, o, t), `account provider=${safe(ev.provider)} state=${ev.state}`)
  return { sent: true }
}

// 测试用：等发送和日志都落地；清空所有状态并指定日志文件（"" 表示不写日志）
export async function settled() {
  await Promise.all([...pending])
  await logTail
}
export function reset(log = "") {
  for (const state of [hookSeen, hookSessions, interrupts, starts, models, sentAt, accountAt, ignored]) state.clear()
  pending = new Set()
  logFile = log
}
