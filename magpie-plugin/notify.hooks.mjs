// magpie 的异步网关钩子（package.json 的 magpie.hooks）：在网关上判断一个回合
// 什么时候结束——模型给出回答且不再要工具，或请求失败——然后交给分发中枢。
//
// 回合跟踪逻辑移植自 @magpie-community/gateway-notify 0.1.5（MIT，见
// LICENSE.magpie-community），判定函数在 turn.mjs，原样保留。与上游的差别只在
// 出口：这里不直接发送，而是把「完成 / 失败」事件交给 hub.mjs，由它和 Agent hook
// 上报的事件一起过滤、去重、发送。
//
// onRequest 标记回合开始（用户自己的话到达模型，而不是工具结果），不改请求。
// 一个会话 = session 加它的第一条用户消息，所以与父会话共用 session 的子代理
// 是独立会话，永远不发通知。这里不抛错，也不在请求路径上等待任何东西。
import { hasTools, opensTurn, ending, firstWords, compacting, continued, background, subagent, progressSummary } from "./turn.mjs"
import { now, later, cancel, setClock } from "./clock.mjs"
import { options, activeChannels } from "./options.mjs"
import { agentId } from "./message.mjs"
import { emit, emitSignIn, moveCooldown, noteIgnored, noteModel, settled, reset as resetHub } from "./hub.mjs"
import { ensureService } from "./service.mjs"

// 进行中的会话：key -> { key, base, start, seen, agent, model, cwd, session, busy, waiting, nested, failed, background, compactedAt }
//   base     会话所在的 session（没有就用 agent 加目录）
//   seen     magpie 最近一次看到它的请求或回复的时间
//   busy     已发出、onResponse 还没来的请求数
//   waiting  上一个回复要了工具，Agent 正在跑工具
//   nested   它第一次出现时，同一 session 里另一个顶层会话正忙或在等工具：
//            是子代理，永远不发通知
//   failed   失败通知被暂缓期间的 { at, timer }
//   background  它启动（或用 SendMessage 恢复）且还没回报的 Claude Code 后台代理，
//            tool_use_id -> { agent, interim }：它们回报之前回合不算结束
//   compactedAt  它最近一次请求压缩摘要的时间，压缩后继续的会话靠它找回原会话
const turns = new Map()
let startService = ensureService
// 每个 base 最新的顶层会话：给不带用户消息的请求（按 id 续接的 Responses 请求）用
const latest = new Map()
const MAX_TURNS = 500
const STALE = 24 * 3600 * 1000
// 这么久没动静的会话不再让新会话算作嵌套：Agent 在跑工具时被退出，或回复丢了
const ACTIVE = 15 * 60 * 1000
// 后台代理可能跑很久而父会话一直没有消息
const BACKGROUND = 2 * 3600 * 1000

// 请求在哪里跑：有 magpie 的 session id 就用它，否则用 agent 加目录。
// Claude Code 的子代理与父会话共用 session。
const baseOf = (ctx) => ctx?.session ? `s:${ctx.session}` : ctx?.agent && ctx?.cwd ? `a:${ctx.agent}|${ctx.cwd}` : ""

// 会话第一句话的短哈希（FNV-1a）
function hash(s) {
  let h = 0x811c9dc5
  for (let i = 0; i < s.length; i++) h = Math.imul(h ^ s.charCodeAt(i), 0x01000193)
  return (h >>> 0).toString(36)
}

// magpie 的请求来源：local 是本机上不带网关密钥的 Agent；magpie-key 是 magpie 自己
// 生成的共享密钥，且请求来自本机——开了共享以后，magpie 给本机 Agent 配的就是这把；
// gateway-key 是其他具名密钥（或共享密钥来自别处），可能是别人的；remote 是另一台
// 设备。除非 allOrigins，只有前两种算自己的（magpie 的 gateway-request-hooks 文档）。
// 上游 gateway-notify 0.1.5 只认 local，这里与它不同。
const OWN = new Set(["local", "magpie-key"])
const ownOrigin = (ctx, o) => o.allOrigins || ctx?.origin == null || OWN.has(ctx.origin)

function forget(key) {
  const t = turns.get(key)
  if (t?.failed) cancel(t.failed.timer)
  turns.delete(key)
}

function prune(t) {
  for (const [k, v] of turns) if (t - v.start > STALE) forget(k)
  while (turns.size > MAX_TURNS) forget(turns.keys().next().value)
  for (const [k, v] of latest) if (!turns.has(v)) latest.delete(k)
}

// 同一 session 里另一个顶层会话正在请求或跑工具时出现的新会话，是那个 Agent
// 启动的子代理。只有真实的 session 才能这么判断；agent 加目录拼成的 base
// 之后常被不相干的会话复用。
const parentActive = (base, t) => base.startsWith("s:") &&
  [...turns.values()].some((v) => v.base === base && !v.nested &&
    (((v.busy > 0 || v.waiting) && t - v.seen < ACTIVE) || (v.background.size > 0 && t - v.seen < BACKGROUND)))

// 压缩后继续的会话接的是哪一个：同一 base 里最近请求过压缩摘要的那个。
// 子代理的压缩接回子代理，所以它仍然是嵌套的。
function compacted(base, t) {
  let best = null
  for (const v of turns.values()) if (v.base === base && v.compactedAt && t - v.compactedAt < ACTIVE && (!best || v.compactedAt > best.compactedAt)) best = v
  return best
}

export function onRequest(body, ctx) {
  try {
    // 网关一有流量，本地服务（接收 hook 上报）就该在了
    startService(ctx?.options)
    const o = options(ctx?.options)
    if (!activeChannels(o).length) return undefined
    if (!ownOrigin(ctx, o)) { noteIgnored(ctx?.agent, ctx?.origin); return undefined }
    noteModel(ctx?.agent, ctx?.cwd, ctx?.model ?? body?.model, ctx?.session)
    if (o.agents.length && !o.agents.includes(agentId(ctx?.agent))) return undefined
    const base = baseOf(ctx)
    if (!base) return undefined
    const first = firstWords(body, ctx?.protocol)
    const key = first ? `${base}#${hash(first)}` : latest.get(base) ?? `${base}#`
    const t = now()
    if (compacting(body, ctx?.protocol)) {
      // 摘要不是回合的答案：既不开始也不结束，但要记住哪个会话即将换一句开头继续
      const c = turns.get(key)
      if (c) { c.compactedAt = t; c.seen = t }
      return undefined
    }
    // 子代理的进度标签（Claude Code 2.1.318+）：它的 end_turn 回复会结束子代理的
    // 回合，子代理的下一个请求在父会话安静一阵后就会被当成独立会话，在工作中途发通知
    if (progressSummary(body, ctx?.protocol)) return undefined
    if (!hasTools(body, ctx.protocol)) return undefined
    let turn = turns.get(key)
    const resumed = !turn && continued(body, ctx.protocol)
    if (resumed) {
      const old = compacted(base, t)
      if (old) {
        turns.delete(old.key)
        moveCooldown(old.key, key)
        if (latest.get(base) === old.key) latest.set(base, key)
        old.key = key
        old.compactedAt = 0
        turns.set(key, old)
        turn = old
      }
    }
    if (turn?.failed) {
      // Agent 自己重试了，或用户按 Esc 后继续：那次失败不是回合的结束
      cancel(turn.failed.timer)
      turn.failed = null
    } else if (turn && !resumed && opensTurn(body, ctx.protocol) && !turn.nested) {
      turn.start = t // 用户的新消息让回合重新计时
      // 只给过中间报告的代理可能再也不回报（比如开发服务器）：用户已经看过
      // 那份报告并继续了，它不再拖住后面的回合
      for (const [k, v] of turn.background) if (v.interim) turn.background.delete(k)
    } else if (!turn) {
      const nested = subagent(body, ctx.protocol) || parentActive(base, t)
      turn = { key, base, start: t, seen: t, agent: ctx.agent ?? "", model: ctx.model ?? body?.model ?? "", cwd: ctx.cwd ?? "", session: ctx.session ?? "", busy: 0, waiting: false, nested, failed: null, background: new Map(), compactedAt: 0 }
      turns.set(key, turn)
      if (!nested) latest.set(base, key)
      prune(t)
    }
    const bg = background(body, ctx.protocol)
    for (const id of bg.launched) {
      const agent = bg.agents[id] ?? ""
      // 一个代理同一时间只跑一次：恢复它的 SendMessage 取代它之前挂着的调用，
      // 那些调用迟到的通知对不上任何东西，结束不了恢复后的这一次
      if (agent) for (const [k, v] of turn.background) if (k !== id && v.agent === agent) turn.background.delete(k)
      turn.background.set(id, { agent, interim: false })
    }
    // 通知点名的是启动或最近恢复该代理的那次调用（tool-use id），只结束那一次；
    // 只点名代理的通知（background() 给出它的 task-id）结束它挂着的所有调用
    const match = (id, k, v) => k === id || v.agent === id
    for (const id of bg.interim) for (const [k, v] of turn.background) if (match(id, k, v)) v.interim = true
    for (const id of bg.stopped) for (const [k, v] of turn.background) if (match(id, k, v)) turn.background.delete(k)
    turn.waiting = false
    turn.busy++
    turn.seen = t
    if (ctx.state) ctx.state.notify = { turn }
  } catch {}
  return undefined
}

function finish(turn, at, ok, text, model, rawOptions) {
  emit({
    source: "gateway", key: turn.key, agent: turn.agent, kind: ok ? "run_completed" : "run_failed",
    session: turn.session, cwd: turn.cwd, model, ms: at - turn.start, text,
  }, rawOptions)
}

export function onResponse(reply, ctx) {
  try {
    const turn = ctx?.state?.notify?.turn
    if (!turn) return
    turn.busy = Math.max(0, turn.busy - 1)
    turn.seen = now()
    const key = turn.key
    if (turns.get(key) !== turn) return
    const failed = ctx.success === false
    const end = failed ? { done: true, text: "" } : ending(reply, ctx.protocol)
    if (!end.done) { turn.waiting = true; return }
    // Claude Code 的后台代理还在干活：它们停下时主会话会被恢复，那才是结束
    if (!failed && turn.background.size > 0) return
    const o = options(ctx.options)
    if (turn.nested || (failed && !o.events.includes("run_failed"))) { forget(key); return }
    const model = ctx.model ?? turn.model
    if (!failed) { forget(key); finish(turn, now(), true, end.text, model, ctx.options); return }
    // Agent 会自己重试 429 和 5xx，Esc 到网关这里也是一次失败：
    // 先把通知压住，会话如果继续就丢掉它
    const at = now()
    const timer = later(() => {
      if (turns.get(turn.key) !== turn || turn.failed?.timer !== timer) return
      turns.delete(turn.key)
      finish(turn, at, false, "", model, ctx.options)
    }, o.failureHoldSeconds * 1000)
    turn.failed = { at, timer }
  } catch {}
  return
}

// onSignIn：订阅账号需要重新登录（登录被撤销或刷新被拒），或 Claude 登录
// 三天内到期时，magpie（0.1.3443 起）告诉钩子一次。
export function onSignIn(ev, ctx) {
  try { emitSignIn(ev, ctx?.options) } catch {}
}

// 测试用：忘掉所有回合，钉住时钟和定时器
export function reset(clock, timers) {
  for (const k of [...turns.keys()]) forget(k)
  latest.clear()
  startService = () => {} // 测试里不占端口
  setClock(clock, timers)
  resetHub()
}
export { settled }
