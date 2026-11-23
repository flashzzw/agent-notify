// 本插件自己的行为：选项、纯文本文案、两个渠道、hook 上报与网关事件的汇合。
// 回合判定的回归测试在 turns.upstream.test.mjs。
import { afterEach, beforeEach, expect, test } from "bun:test"
import { createHash } from "node:crypto"
import { mkdirSync, mkdtempSync, readFileSync, rmSync, utimesSync, writeFileSync } from "node:fs"
import { request as httpRequest } from "node:http"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { onRequest, onResponse, onSignIn, reset, settled } from "../notify.hooks.mjs"
import { emit, reset as resetHub } from "../hub.mjs"
import { options, activeChannels, wecomWebhookOK } from "../options.mjs"
import { build, plain, cut, originOf, originLabel, resetDistro } from "../message.mjs"
import { send, setChannel, cutBytes } from "../channels.mjs"
import { request as toastRequest, setRunner } from "../toast.mjs"
import { startService, parseEvent, storedOptions, TOKEN_HEADER } from "../service.mjs"

const HOOK = "http://127.0.0.1:9/cgi-bin/webhook/send?key=test"
const both = { channels: { toast: { enabled: true }, wecom: { enabled: true, webhook: HOOK } } }

let clock = 1_000_000
let sent = [] // { channel, m }
const minutes = (n) => { clock += n * 60_000 }
const seconds = (n) => { clock += n * 1000 }
let timers = [] // 被压住的失败通知：{ at, fn }
const fakeTimers = {
  later: (fn, ms) => { const t = { at: clock + ms, fn }; timers.push(t); return t },
  cancel: (t) => { timers = timers.filter((x) => x !== t) },
}
// 拨动时钟，触发到点的定时器，等发送落地
async function wait(ms) {
  clock += ms
  for (const t of timers.filter((x) => x.at <= clock)) { timers = timers.filter((x) => x !== t); t.fn() }
  await settled()
}

beforeEach(() => {
  clock = 1_000_000
  sent = []
  timers = []
  reset(() => clock, fakeTimers)
  resetDistro("")
  for (const name of ["toast", "wecom"]) setChannel(name, async (_c, m) => { sent.push({ channel: name, m }); return { ok: true } })
})
afterEach(() => { setChannel("toast"); setChannel("wecom"); setRunner(); reset(); resetDistro(undefined) })

const hook = (kind, extra = {}) => ({ source: "hook", agent: "claude", kind, session: "s1", cwd: "/home/me/code/demo", origin: "wsl:Ubuntu-24.04", ...extra })

// ---- 选项 ----
test("默认：开 Toast、关企业微信、四个首期事件、带回复、不限用时、中文", () => {
  const o = options(undefined)
  expect(activeChannels(o)).toEqual(["toast"])
  expect(o.events).toEqual(["permission_required", "input_required", "run_completed", "run_failed"])
  expect(o.includeText).toBe(true)
  expect(o.minSeconds).toBe(0)
  expect(o.lang).toBe("zh")
  expect(o.port).toBe(3497)
})

test("package.json 给出的示例选项与默认行为一致", () => {
  const pkg = JSON.parse(readFileSync(new URL("../package.json", import.meta.url), "utf8"))
  expect(pkg.magpie.hooks).toBe("./notify.hooks.mjs")
  expect(options(pkg.magpie.options)).toEqual(options(undefined))
})

test("企业微信只接受群机器人地址；地址不对或没填就不发", () => {
  expect(wecomWebhookOK("https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=abc")).toBe(true)
  expect(wecomWebhookOK("https://qyapi.weixin.qq.com/cgi-bin/webhook/send")).toBe(false)
  expect(wecomWebhookOK("https://example.com/cgi-bin/webhook/send?key=abc")).toBe(false)
  expect(wecomWebhookOK("http://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=abc")).toBe(false)
  expect(wecomWebhookOK("")).toBe(false)
  const o = options({ channels: { wecom: { enabled: true, webhook: "https://example.com/hook" } } })
  expect(activeChannels(o)).toEqual(["toast"])
})

test("总开关关掉后没有任何渠道；乱填的值回到默认", () => {
  expect(activeChannels(options({ ...both, enabled: false }))).toEqual([])
  const o = options({ port: "abc", minSeconds: -5, maxTextLength: 99999, events: ["run_completed", "nope"], lang: "fr" })
  expect(o.port).toBe(3497)
  expect(o.minSeconds).toBe(0)
  expect(o.maxTextLength).toBe(2000)
  expect(o.events).toEqual(["run_completed"])
  expect(o.lang).toBe("zh")
})

// ---- 文案 ----
test("模型回复里的 markdown 标记被去掉，压成一段纯文本", () => {
  const md = "## 结果\n\n已修复 **两个** 测试，见 `src/app.ts` 和 [文档](https://example.com/doc)。\n\n- 第一项\n- 第二项\n\n```ts\nconst a = 1\n```\n> 引用一句\n\n---\n*完成*"
  expect(plain(md)).toBe("结果 已修复 两个 测试，见 src/app.ts 和 文档。 第一项 第二项 const a = 1 引用一句 完成")
})

test("不是 markdown 的符号留着：下划线变量名、乘号、单独的星号", () => {
  expect(plain("snake_case_name 和 a * b = c")).toBe("snake_case_name 和 a * b = c")
  expect(plain("__init__.py 里的 2*3*4")).toBe("__init__.py 里的 2*3*4")
  expect(plain("调用 `f(**kwargs)` 和 `g(*args)`")).toBe("调用 f(**kwargs) 和 g(*args)")
})

test("截断按码点算，不拆开表情", () => {
  expect(cut("👍👍👍👍", 2)).toBe("👍👍…")
  expect(cut("短", 5)).toBe("短")
})

test("完成通知：标题带项目，正文是用时、模型、来源和纯文本回复", () => {
  const m = build({ agent: "claude", kind: "run_completed", cwd: "/home/me/code/demo", origin: "wsl:Ubuntu-24.04", model: "claude-opus", ms: 12 * 60_000, text: "**全部**通过" }, options({}))
  expect(m.emoji).toBe("✅")
  expect(m.title).toBe("Claude Code 完成 · demo")
  expect(m.body).toBe("用时 12 分钟\n模型：claude-opus\n来源：WSL · Ubuntu-24.04\n\n全部通过")
})

test("等待授权：没有用时，带工具名", () => {
  const m = build({ agent: "codex", kind: "permission_required", cwd: "C:\\work\\demo", origin: "windows", tool: "Bash", ms: 5000 }, options({}))
  expect(m.title).toBe("Codex 等待授权 · demo")
  expect(m.body).toBe("工具：Bash\n来源：Windows")
})

test("includeText 关掉后不带回复；英文文案", () => {
  const m = build({ agent: "omo", kind: "run_failed", cwd: "/w/p", origin: "linux", ms: 90_000, text: "secret" }, options({ includeText: false, lang: "en" }))
  expect(m.title).toBe("Oh My OpenCode failed · p")
  expect(m.body).toBe("took 2 min\nFrom: Linux")
})

test("来源按工作目录形态推断", () => {
  expect(originOf("C:\\Users\\me\\proj", "win32")).toBe("windows")
  expect(originOf("\\\\wsl.localhost\\Ubuntu-24.04\\home\\me", "win32")).toBe("wsl:Ubuntu-24.04")
  expect(originOf("/home/me/proj", "win32")).toBe("wsl")
  expect(originOf("/home/me/proj", "linux", { WSL_DISTRO_NAME: "Debian" })).toBe("wsl:Debian")
  expect(originOf("/home/me/proj", "linux", {})).toBe("linux")
  expect(originOf("", "win32")).toBe("")
  resetDistro("Ubuntu-24.04")
  expect(originOf("/home/me/proj", "win32")).toBe("wsl:Ubuntu-24.04")
  expect(originLabel("wsl:Ubuntu-24.04")).toBe("WSL · Ubuntu-24.04")
})

// ---- 渠道 ----
test("企业微信发的是 text 纯文本消息，不是 markdown", async () => {
  setChannel("wecom")
  const realFetch = globalThis.fetch
  const calls = []
  globalThis.fetch = async (url, init) => { calls.push({ url: String(url), init }); return Response.json({ errcode: 0, errmsg: "ok" }) }
  try {
    const m = build({ agent: "claude", kind: "run_completed", cwd: "/w/demo", origin: "windows", ms: 60_000, text: "# 标题\n**好**" }, options({}))
    expect(await send("wecom", { webhook: HOOK }, m)).toEqual({ ok: true })
    expect(calls).toHaveLength(1)
    expect(calls[0].url).toBe(HOOK)
    const body = JSON.parse(calls[0].init.body)
    expect(body).toEqual({ msgtype: "text", text: { content: "✅ Claude Code 完成 · demo\n用时 1 分钟\n来源：Windows\n\n标题 好" } })
  } finally { globalThis.fetch = realFetch }
})

test("企业微信报错时不回显 Webhook 地址", async () => {
  setChannel("wecom")
  const realFetch = globalThis.fetch
  const m = build({ agent: "claude", kind: "test" }, options({}))
  try {
    globalThis.fetch = async () => { throw new Error(`connect failed ${HOOK}`) }
    expect(await send("wecom", { webhook: HOOK }, m)).toEqual({ ok: false, error: "request failed" })
    globalThis.fetch = async () => Response.json({ errcode: 93000, errmsg: `invalid webhook url ${HOOK}` })
    expect(await send("wecom", { webhook: HOOK }, m)).toEqual({ ok: false, error: "errcode 93000" })
    globalThis.fetch = async () => new Response("nope", { status: 502 })
    expect(await send("wecom", { webhook: HOOK }, m)).toEqual({ ok: false, error: "http 502" })
  } finally { globalThis.fetch = realFetch }
})

test("企业微信正文按 2048 字节截断，不拆开汉字", () => {
  const out = cutBytes("汉".repeat(1000), 2048)
  expect(Buffer.byteLength(out)).toBeLessThanOrEqual(2048)
  expect(out.endsWith("…")).toBe(true)
  expect(out).not.toContain("\uFFFD")
})

test("Toast：文本进 XML 时转义，正文只有文字不放图片，底部一行是项目和来源，每个 Agent 有自己的通知身份", () => {
  const m = build({ agent: "codex", kind: "run_completed", cwd: "/w/a&b", origin: "wsl:Ubuntu-24.04", ms: 1000, text: "<script>1 & 2</script>\u0001" }, options({}))
  const r = toastRequest(m)
  expect(r.AppID).toBe("AgentNotify.Desktop.Codex")
  expect(r.DisplayName).toBe("Codex Notify")
  // Agent 的图标只随通知身份走（顶部来源名旁的小图标），不进正文
  expect(r.IconFile).toMatch(/^[0-9a-f]{64}\.png$/)
  expect(r.Icon.length).toBeGreaterThan(0)
  expect(r.XML).not.toContain("<image")
  expect(r.XML).toContain("<text>Codex 完成 · a&amp;b</text>")
  expect(r.XML).toContain("&lt;script&gt;1 &amp; 2&lt;/script&gt;")
  expect(r.XML).not.toContain("\u0001")
  expect(r.XML).toContain('<text placement="attribution">a&amp;b · WSL · Ubuntu-24.04</text>')
  expect(toastRequest(build({ agent: "gemini", kind: "test" }, options({}))).AppID).toBe("AgentNotify.Desktop")
})

test("Toast：请求以 Base64 走 stdin，命令行里只有固定脚本", async () => {
  setChannel("toast")
  const runs = []
  setRunner(async (file, args, input) => { runs.push({ file, args, input }); return { ok: true } })
  const m = build({ agent: "claude", kind: "permission_required", cwd: "/w/demo", tool: "Bash; rm -rf /" }, options({}))
  const r = await send("toast", {}, m)
  if (!runs.length) { expect(r.ok).toBe(false); return } // 这台机器上没有 PowerShell
  expect(r).toEqual({ ok: true })
  expect(runs[0].args.join(" ")).not.toContain("rm -rf")
  const decoded = JSON.parse(Buffer.from(runs[0].input, "base64").toString("utf8"))
  expect(decoded.XML).toContain("工具：Bash; rm -rf /")
})

// ---- hook 上报 ----
test("hook 上报的等待授权立即发到所有开启的渠道", async () => {
  expect(emit(hook("permission_required", { tool: "Bash" }), both)).toEqual({ sent: true })
  await settled()
  expect(sent.map((s) => s.channel).sort()).toEqual(["toast", "wecom"])
  expect(sent[0].m.title).toBe("Claude Code 等待授权 · demo")
  expect(sent[0].m.body).toBe("工具：Bash\n来源：WSL · Ubuntu-24.04")
})

test("turn_started 只计时；随后的完成通知带用时", async () => {
  expect(emit(hook("turn_started"), both).sent).toBe(false)
  minutes(7)
  emit(hook("run_completed", { text: "done" }), both)
  await settled()
  expect(sent).toHaveLength(2)
  expect(sent[0].m.body).toContain("用时 7 分钟")
})

test("minSeconds 只挡太短的回合，等待授权不受影响", async () => {
  const o = { ...both, minSeconds: 60 }
  emit(hook("turn_started"), o)
  seconds(20)
  expect(emit(hook("permission_required", { tool: "Edit" }), o).sent).toBe(true)
  expect(emit(hook("run_completed"), o)).toEqual({ sent: false, reason: "too short" })
  seconds(60)
  expect(emit(hook("run_completed"), o).sent).toBe(true)
})

test("没订阅的事件、被过滤的 Agent 不发；关着的渠道不发", async () => {
  expect(emit(hook("subagent_completed"), both).reason).toBe("event off")
  expect(emit(hook("tool_failed"), both).reason).toBe("event off")
  expect(emit(hook("run_completed", { agent: "codex" }), { ...both, agents: ["claude"] }).reason).toBe("agent filtered")
  expect(emit(hook("run_completed"), { enabled: false }).reason).toBe("no channel")
  emit(hook("run_completed"), {})
  await settled()
  expect(sent.map((s) => s.channel)).toEqual(["toast"])
})

test("同一条 hook 事件短时间内只发一次，内容不同或过了窗口再发", async () => {
  expect(emit(hook("permission_required", { tool: "Bash" }), both).sent).toBe(true)
  expect(emit(hook("permission_required", { tool: "Bash" }), both).reason).toBe("duplicate")
  expect(emit(hook("permission_required", { tool: "Edit" }), both).sent).toBe(true)
  seconds(11)
  expect(emit(hook("permission_required", { tool: "Bash" }), both).sent).toBe(true)
})

test("test 事件跳过事件过滤和去重", async () => {
  const o = { ...both, events: [] }
  expect(emit({ source: "hook", agent: "codex", kind: "test", origin: "windows" }, o).sent).toBe(true)
  expect(emit({ source: "hook", agent: "codex", kind: "test", origin: "windows" }, o).sent).toBe(true)
  await settled()
  expect(sent).toHaveLength(4)
  expect(sent[0].m.title).toBe("Codex 测试通知")
})

// ---- 网关与 hook 汇合 ----
const tools = [{ name: "Bash", input_schema: { type: "object" } }]
const ask = { model: "m", tools, messages: [{ role: "user", content: "fix the test" }] }
const answer = { type: "message", role: "assistant", stop_reason: "end_turn", content: [{ type: "text", text: "All **tests** pass now." }] }
const gctx = (extra = {}) => ({ protocol: "anthropic", agent: "claude", session: "sess-1", cwd: "/home/me/code/demo", origin: "local", model: "claude-opus", state: {}, options: both, ...extra })

test("网关判定的完成：默认不限用时，带模型、来源和纯文本回复", async () => {
  const c = gctx()
  onRequest(ask, c)
  minutes(2)
  onResponse(answer, c)
  await settled()
  expect(sent).toHaveLength(2)
  expect(sent[0].m.title).toBe("Claude Code 完成 · demo")
  expect(sent[0].m.body).toContain("用时 2 分钟")
  expect(sent[0].m.body).toContain("模型：claude-opus")
  expect(sent[0].m.body.endsWith("All tests pass now.")).toBe(true)
})

test("某一侧的 Agent 有 hook 上报时，网关对它不再重复发完成通知", async () => {
  emit(hook("turn_started"), both) // WSL 侧的 claude 装了 hook
  const c = gctx()
  onRequest(ask, c)
  minutes(2)
  onResponse(answer, c)
  await settled()
  expect(sent).toHaveLength(0)
  emit(hook("run_completed", { text: "ok" }), both)
  await settled()
  expect(sent).toHaveLength(2)
  // hook 事件里没有模型，从网关看到的同一目录的请求补上
  expect(sent[0].m.body).toContain("模型：claude-opus")
})

test("另一侧（Windows）的同名 Agent 没装 hook，网关照发", async () => {
  emit(hook("turn_started"), both)
  const c = gctx({ cwd: "C:\\work\\demo", session: "sess-win" })
  onRequest(ask, c)
  minutes(1)
  onResponse(answer, c)
  await settled()
  expect(sent).toHaveLength(2)
  expect(sent[0].m.body).toContain("来源：Windows")
})

// ---- 两个来源对同一个会话的认定 ----
// 下面这些情形都来自真实流量：网关给请求起的名字和来源，与 hook 上报的不总是一样。
const failed = (c) => onResponse(null, { ...c, success: false })

test("magpie 写进本机 Agent 的共享密钥（magpie-key）算自己的；其他具名密钥、别的设备和不明来源不算", async () => {
  for (const origin of ["gateway-key", "remote", ""]) {
    const c = gctx({ origin, session: `far-${origin}` })
    onRequest(ask, c)
    minutes(1)
    onResponse(answer, c)
  }
  await settled()
  expect(sent).toHaveLength(0)
  const c = gctx({ origin: "magpie-key", session: "own" })
  onRequest(ask, c)
  minutes(1)
  onResponse(answer, c)
  await settled()
  expect(sent).toHaveLength(2)
  expect(sent[0].m.title).toBe("Claude Code 完成 · demo")
})

test("同一个会话有 hook 上报时网关不重复发，哪怕网关给它起了别的名字（桌面版里跑的 Claude Code 是 claude-desktop）", async () => {
  emit(hook("turn_started", { session: "sess-1" }), both)
  const c = gctx({ agent: "claude-desktop", origin: "magpie-key" })
  onRequest(ask, c)
  minutes(2)
  onResponse(answer, c)
  await settled()
  expect(sent).toHaveLength(0)
  emit(hook("run_completed", { session: "sess-1", text: "ok" }), both)
  await settled()
  expect(sent).toHaveLength(2)
  expect(sent[0].m.title).toBe("Claude Code 完成 · demo")
  // 模型名按会话补，不受两边名字不同的影响
  expect(sent[0].m.body).toContain("模型：claude-opus")
  // 同一侧另一个没有 hook 上报的桌面版会话，网关照发
  const other = gctx({ agent: "claude-desktop", session: "sess-2" })
  onRequest(ask, other)
  minutes(1)
  onResponse(answer, other)
  await settled()
  expect(sent).toHaveLength(4)
  expect(sent[2].m.title).toBe("Claude Desktop 完成 · demo")
})

test("hook 不报失败的 Agent（Codex）：网关判定的失败照发，完成仍让给 hook；Claude 的失败由 hook 自己报", async () => {
  const cx = { agent: "codex", session: "cx-1" }
  emit(hook("turn_started", cx), both)
  const c = gctx({ agent: "codex", session: "cx-1" })
  onRequest(ask, c)
  minutes(1)
  failed(c)
  await wait(61_000)
  expect(sent).toHaveLength(2)
  expect(sent[0].m.title).toBe("Codex 失败 · demo")
  // 同一个会话后来正常完成：hook 会报，网关不发
  const d = gctx({ agent: "codex", session: "cx-1" })
  onRequest(ask, d)
  minutes(1)
  onResponse(answer, d)
  await settled()
  expect(sent).toHaveLength(2)
  // Claude Code 的 hook 有 StopFailure，网关的失败不再发
  emit(hook("turn_started"), both)
  const k = gctx({ session: "s1" })
  onRequest(ask, k)
  minutes(1)
  failed(k)
  await wait(61_000)
  expect(sent).toHaveLength(2)
})

test("用户中断（hook 报了 run_interrupted）之后，网关压着的那次失败不发", async () => {
  const cx = { agent: "codex", session: "cx-1" }
  emit(hook("turn_started", cx), both)
  const c = gctx({ agent: "codex", session: "cx-1" })
  onRequest(ask, c)
  minutes(1)
  failed(c) // 按了 Esc：回复没传完，在网关看来是一次失败
  seconds(1)
  // 默认没订阅「已中断」，不发通知，但中枢记下了这次中断
  expect(emit(hook("run_interrupted", cx), both).reason).toBe("event off")
  await wait(61_000)
  expect(sent).toHaveLength(0)
  // 之后一次真的失败照发
  minutes(5)
  const d = gctx({ agent: "codex", session: "cx-1" })
  onRequest(ask, d)
  minutes(1)
  failed(d)
  await wait(61_000)
  expect(sent).toHaveLength(2)
  expect(sent[0].m.title).toBe("Codex 失败 · demo")
})

test("宿主重启后中枢不记得谁有 hook：同一会话的完成，网关发过了，随后 hook 报的不再发", async () => {
  const c = gctx({ agent: "claude-desktop" })
  onRequest(ask, c)
  minutes(2)
  onResponse(answer, c)
  seconds(1)
  expect(emit(hook("run_completed", { session: "sess-1", text: "ok" }), both)).toEqual({ sent: false, reason: "duplicate" })
  await settled()
  expect(sent).toHaveLength(2)
  expect(sent[0].m.title).toBe("Claude Desktop 完成 · demo")
  // 下一个回合：这个会话已经有 hook 上报，网关让给它
  minutes(1)
  const d = gctx({ agent: "claude-desktop" })
  onRequest(ask, d)
  minutes(1)
  onResponse(answer, d)
  expect(emit(hook("run_completed", { session: "sess-1", text: "again" }), both).sent).toBe(true)
  await settled()
  expect(sent).toHaveLength(4)
  expect(sent[2].m.title).toBe("Claude Code 完成 · demo")
})

test("没发出去的事件带原因记进日志；回合开始不记，被忽略的来源每种只记一次，正文和目录不进日志", async () => {
  const dir = mkdtempSync(join(tmpdir(), "agent-notify-log-"))
  const file = join(dir, "magpie-plugin.log")
  try {
    resetHub(file)
    emit(hook("turn_started"), both)
    emit(hook("tool_failed", { text: "secret output" }), both)
    const c = gctx()
    onRequest(ask, c)
    minutes(1)
    onResponse(answer, c)
    onRequest(ask, gctx({ origin: "gateway-key", session: "far-1" }))
    onRequest(ask, gctx({ origin: "gateway-key", session: "far-2" }))
    await settled()
    const lines = readFileSync(file, "utf8").trim().split("\n")
    expect(lines).toHaveLength(3)
    expect(lines[0]).toMatch(/ hook agent=claude event=tool_failed skipped \(event off\)$/)
    expect(lines[1]).toMatch(/ gateway agent=claude event=run_completed skipped \(covered by hooks\)$/)
    expect(lines[2]).toMatch(/ gateway agent=claude origin=gateway-key ignored \(not this computer's own agent\)$/)
    expect(lines.join("\n")).not.toMatch(/secret|demo/)
  } finally {
    resetHub()
    rmSync(dir, { recursive: true, force: true })
  }
})

test("账号掉登录走同样的渠道，冷却期内只发一次", async () => {
  const ev = { provider: "codex", providerName: "Codex", account: "me@example.com", state: "signed-out" }
  onSignIn(ev, { options: both })
  onSignIn(ev, { options: both })
  await settled()
  expect(sent).toHaveLength(2)
  expect(sent[0].m.title).toBe("Codex 账号 me@example.com 已登出")
  expect(sent[0].m.body).toBe("请在 magpie 中重新登录。")
  onSignIn(ev, { options: { ...both, accounts: false } })
  await settled()
  expect(sent).toHaveLength(2)
})

// ---- 本地服务 ----
test("上报的事件逐字段校验", () => {
  expect(parseEvent({ v: 1, agent: "Claude", event: "run_completed", origin: "wsl:Ubuntu-24.04", text: "x".repeat(9000) })).toMatchObject({ source: "hook", agent: "claude", kind: "run_completed", origin: "wsl:Ubuntu-24.04" })
  expect(parseEvent({ v: 1, agent: "claude", event: "run_completed", text: "x".repeat(9000) }).text).toHaveLength(8000)
  expect(parseEvent({ v: 1, agent: "claude", event: "run_completed", origin: "<b>evil</b>" }).origin).toBe("")
  expect(parseEvent({ v: 2, agent: "claude", event: "run_completed" })).toBeNull()
  expect(parseEvent({ v: 1, agent: "claude", event: "rm -rf" })).toBeNull()
  expect(parseEvent({ v: 1, event: "run_completed" })).toBeNull()
  expect(parseEvent({ v: 1, event: "test" })).not.toBeNull()
  expect(parseEvent([])).toBeNull()
})

test("服务只认带口令的本机 JSON 请求，并把端口和口令写进端点文件", async () => {
  const home = mkdtempSync(join(tmpdir(), "agent-notify-"))
  const svc = await startService({ port: 0, home, getOptions: () => both })
  try {
    const endpoint = JSON.parse(readFileSync(join(home, ".agent-notify", "magpie-endpoint.json"), "utf8"))
    expect(endpoint.port).toBe(svc.port)
    expect(endpoint.token).toMatch(/^[0-9a-f]{64}$/)
    const url = `http://127.0.0.1:${svc.port}/event`
    const post = (headers, body) => fetch(url, { method: "POST", headers, body: typeof body === "string" ? body : JSON.stringify(body) })
    const ok = { "content-type": "application/json", [TOKEN_HEADER]: endpoint.token }
    const event = { v: 1, agent: "codex", event: "permission_required", cwd: "/w/demo", origin: "wsl:Ubuntu-24.04", tool: "shell" }

    expect((await post({ "content-type": "application/json" }, event)).status).toBe(401)
    expect((await post({ ...ok, [TOKEN_HEADER]: "0".repeat(64) }, event)).status).toBe(401)
    expect((await post({ ...ok, "content-type": "text/plain" }, event)).status).toBe(415)
    expect((await post(ok, "{not json")).status).toBe(400)
    expect((await post(ok, { v: 1, agent: "codex", event: "nope" })).status).toBe(400)
    // DNS 重绑定：请求打到本服务，Host 却是别的名字
    const rebound = await new Promise((resolve, reject) => {
      const req = httpRequest({ host: "127.0.0.1", port: svc.port, path: "/event", method: "POST", headers: { ...ok, host: "evil.example" } }, (res) => { res.resume(); resolve(res.statusCode) })
      req.on("error", reject)
      req.end(JSON.stringify(event))
    })
    expect(rebound).toBe(403)
    expect((await fetch(`http://127.0.0.1:${svc.port}/health`)).status).toBe(200)
    expect(sent).toHaveLength(0)

    const res = await post(ok, event)
    expect(res.status).toBe(202)
    expect(await res.json()).toEqual({ ok: true, sent: true })
    await settled()
    expect(sent).toHaveLength(2)
    expect(sent[0].m.title).toBe("Codex 等待授权 · demo")

    // 重启后沿用同一个口令，已安装的 hook 不用重配
    await svc.close()
    const again = await startService({ port: 0, home, getOptions: () => both })
    expect(again.token).toBe(endpoint.token)
    await again.close()
  } finally {
    await svc.close().catch(() => {})
    rmSync(home, { recursive: true, force: true })
  }
})

test("服务从 magpie 的 plugins.json 读自己的选项，改了就能读到新值", async () => {
  const dir = mkdtempSync(join(tmpdir(), "agent-notify-magpie-"))
  const file = join(dir, "plugins.json")
  const write = (plugins, at) => { writeFileSync(file, JSON.stringify({ plugins })); utimesSync(file, at, at) }
  try {
    expect(await storedOptions(dir, "/opt/plugins/agent-notify/magpie-plugin/")).toBeUndefined() // 文件还不存在
    write([{ spec: "@guotree/magpie-websearch@latest", options: { a: 1 } }, { spec: "/opt/plugins/agent-notify/magpie-plugin", options: { lang: "en" } }], 1000)
    expect(await storedOptions(dir, "/opt/plugins/agent-notify/magpie-plugin/")).toEqual({ lang: "en" })
    write([{ spec: "/opt/plugins/agent-notify/magpie-plugin", options: { lang: "zh" } }], 2000)
    expect(await storedOptions(dir, "/opt/plugins/agent-notify/magpie-plugin/")).toEqual({ lang: "zh" })
    // Windows 上原位加载 WSL 里的目录：UNC 路径，大小写和斜杠方向都可能不同
    write([{ spec: "\\\\wsl.localhost\\Ubuntu-24.04\\home\\me\\agent-notify\\magpie-plugin", options: { minSeconds: 5 } }], 3000)
    expect(await storedOptions(dir, "//WSL.localhost/Ubuntu-24.04/home/me/agent-notify/magpie-plugin/")).toEqual({ minSeconds: 5 })
    // 从 npm 装的：按包名认
    write([{ spec: "agent-notify-magpie@latest", options: { port: 4000 } }], 4000)
    expect(await storedOptions(dir, "/somewhere/else")).toEqual({ port: 4000 })
    // 没设过选项，或列表里没有本插件
    write([{ spec: "agent-notify-magpie@latest" }, { spec: "other", options: { x: 1 } }], 5000)
    expect(await storedOptions(dir, "/somewhere/else")).toBeUndefined()
  } finally { rmSync(dir, { recursive: true, force: true }) }
})

test("配置页随包发布，不联网、没有内联脚本，默认值与 options.mjs 一致", () => {
  const pkg = JSON.parse(readFileSync(new URL("../package.json", import.meta.url), "utf8"))
  expect(pkg.magpie.page.file).toBe("page/index.html")
  expect(pkg.magpie.permissions).toEqual([])
  for (const pattern of ["page/*.html", "page/*.js", "page/*.css", "icons/*.png"]) expect(pkg.files).toContain(pattern)
  const html = readFileSync(new URL("../page/index.html", import.meta.url), "utf8")
  expect(html).toContain('<script src="_magpie/bridge.js"></script>')
  expect(html).not.toMatch(/<script(?![^>]*\ssrc=)[^>]*>/)
  expect(html).not.toMatch(/\son[a-z]+=/i)
  const js = readFileSync(new URL("../page/app.js", import.meta.url), "utf8")
  expect(js).not.toMatch(/\bfetch\(|XMLHttpRequest|WebSocket|EventSource|sendBeacon|innerHTML|\beval\(/)
  const page = Function(`${js.match(/var DEFAULTS = (\{[\s\S]*?\n  \})/)[1].replace(/^/, "return ")}`)()
  const mine = options(undefined)
  for (const key of Object.keys(page)) expect(page[key]).toEqual(key === "channels" ? { toast: { enabled: true }, wecom: { enabled: false, webhook: "" } } : mine[key])
})

test("插件图标是随包发布的 Material Icons 铃铛：页签和插件列表用同一份，不联网", () => {
  const pkg = JSON.parse(readFileSync(new URL("../package.json", import.meta.url), "utf8"))
  // 页签图标：magpie 要求它和页面文件在同一个目录里
  expect(pkg.magpie.page.icon).toBe("page/icon.svg")
  expect(pkg.files).toContain("page/*.svg")
  const svg = readFileSync(new URL("../page/icon.svg", import.meta.url))
  // 与 google/material-design-icons 的 src/social/notifications/materialicons/24px.svg 逐字节相同
  expect(createHash("sha256").update(svg).digest("hex")).toBe("c43e401f607f9c98627a7c3d6fa5c5a959498adfd419c6d6272c15c559b6965d")
  expect(svg.toString("utf8")).not.toMatch(/<script|\son[a-z]+=|href=|url\(|<image|<foreignObject/i)
  // 插件列表里的图标：内嵌同一份图，不给 https 地址，magpie 不用去下载
  expect(pkg.magpie.icon).toBe(`data:image/svg+xml;base64,${svg.toString("base64")}`)
})
