// 本地服务：只监听 127.0.0.1，接收 Agent hook 上报的事件（agent-notify 的转发
// 模式），交给分发中枢。网关看不到「等待授权 / 等待输入」，这些只能由 hook 报上来。
//
// 防护：
// - 每个请求都要带口令（x-agent-notify-token）。口令随机生成，和端口一起写在
//   用户主目录下的端点文件里，只有本机用户读得到；网页拿不到它，也就发不了通知。
// - Host 只认本服务自己的地址，挡住 DNS 重绑定。
// - 请求体限长，字段逐个校验、截断。
import { createServer } from "node:http"
import { randomBytes, timingSafeEqual } from "node:crypto"
import { mkdir, readFile, stat, writeFile } from "node:fs/promises"
import { homedir } from "node:os"
import { dirname, join } from "node:path"
import { fileURLToPath } from "node:url"
import { options, EVENTS } from "./options.mjs"
import { emit } from "./hub.mjs"

export const TOKEN_HEADER = "x-agent-notify-token"
const BODY_LIMIT = 64 * 1024
const KINDS = new Set([...EVENTS, "turn_started", "test"])
const ORIGIN = /^(?:windows|linux|wsl(?::[\w.\- ]{1,64})?)$/

export const endpointFile = (home = homedir()) => join(home, ".agent-notify", "magpie-endpoint.json")

async function loadToken(file) {
  try {
    const token = JSON.parse(await readFile(file, "utf8"))?.token
    if (typeof token === "string" && /^[0-9a-f]{64}$/.test(token)) return token
  } catch {}
  return randomBytes(32).toString("hex")
}

const str = (v, max) => typeof v === "string" ? v.slice(0, max) : ""

// 把上报的 JSON 变成中枢认识的事件；不合规返回 null
export function parseEvent(body) {
  if (body === null || typeof body !== "object" || Array.isArray(body)) return null
  if (body.v !== 1 || !KINDS.has(body.event)) return null
  const agent = str(body.agent, 40).trim().toLowerCase()
  if (!agent && body.event !== "test") return null
  const origin = str(body.origin, 80)
  return {
    source: "hook", agent, kind: body.event,
    session: str(body.session, 200), cwd: str(body.cwd, 1000),
    origin: ORIGIN.test(origin) ? origin : "",
    model: str(body.model, 200), tool: str(body.tool, 200), text: str(body.text, 8000),
  }
}

function reply(res, status, body) {
  const data = JSON.stringify(body)
  res.writeHead(status, { "content-type": "application/json; charset=utf-8", "cache-control": "no-store", "x-agent-notify": "1" })
  res.end(data)
}

function readBody(req) {
  return new Promise((resolve, reject) => {
    const chunks = []
    let size = 0
    req.on("data", (c) => {
      size += c.length
      if (size > BODY_LIMIT) { reject(new Error("too large")); req.destroy(); return }
      chunks.push(c)
    })
    req.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")))
    req.on("error", reject)
  })
}

// 启动服务。port 传 0 由系统挑一个（测试用）；getOptions 每次上报时取当前选项。
// 监听成功之后才写端点文件：端口被占（另一个 magpie 宿主已经在服务）时不动它。
export async function startService({ port, home = homedir(), getOptions = () => undefined }) {
  const file = endpointFile(home)
  const token = await loadToken(file)
  const expected = Buffer.from(token)
  let bound = 0

  const server = createServer(async (req, res) => {
    try {
      const host = String(req.headers.host ?? "")
      if (host !== `127.0.0.1:${bound}` && host !== `localhost:${bound}`) return reply(res, 403, { ok: false, error: "bad host" })
      const path = new URL(req.url ?? "/", "http://127.0.0.1").pathname
      if (req.method === "GET" && path === "/health") return reply(res, 200, { ok: true, name: "agent-notify" })
      if (path !== "/event") return reply(res, 404, { ok: false, error: "not found" })
      if (req.method !== "POST") return reply(res, 405, { ok: false, error: "method not allowed" })
      const got = Buffer.from(String(req.headers[TOKEN_HEADER] ?? ""))
      if (got.length !== expected.length || !timingSafeEqual(got, expected)) return reply(res, 401, { ok: false, error: "bad token" })
      if (!String(req.headers["content-type"] ?? "").toLowerCase().startsWith("application/json")) return reply(res, 415, { ok: false, error: "json only" })
      let event
      try { event = parseEvent(JSON.parse(await readBody(req))) } catch { event = null }
      if (!event) return reply(res, 400, { ok: false, error: "bad event" })
      // 发送在后台进行，这里立即回答：hook 不能拖住 Agent
      reply(res, 202, { ok: true, ...emit(event, await getOptions()) })
    } catch {
      try { reply(res, 500, { ok: false, error: "internal" }) } catch {}
    }
  })

  await new Promise((resolve, reject) => {
    server.once("error", reject)
    server.listen(port, "127.0.0.1", () => { server.off("error", reject); resolve() })
  })
  bound = server.address().port
  server.unref?.()
  try {
    await mkdir(dirname(file), { recursive: true })
    await writeFile(file, JSON.stringify({ port: bound, token, pid: process.pid, startedAt: new Date().toISOString() }), { mode: 0o600 })
  } catch {
    // 写不了端点文件时服务仍在，只是 hook 找不到它，会按约定降级为本地通知
  }
  return { port: bound, token, file, close: () => new Promise((resolve) => server.close(() => resolve())) }
}

// magpie 把每个插件的选项存在它配置目录的 plugins.json 里。选项改了以后，宿主要到
// 下一个网关请求才重启；只靠 hook 上报的 Agent（不走网关）等不到那一刻，所以服务
// 每次处理上报时自己读一遍（按修改时间缓存）。读不到就返回 undefined，由调用方
// 退回宿主最近给的那份。
const PACKAGE = "agent-notify-magpie"
const SELF = fileURLToPath(new URL(".", import.meta.url))
const norm = (p) => String(p ?? "").replace(/\\/g, "/").replace(/\/+$/, "").replace(/^\/+/, "").toLowerCase()
export const magpieConfigDir = (env = process.env, home = homedir()) => join(env.XDG_CONFIG_HOME?.trim() || join(home, ".config"), "magpie")

let stored = { file: "", mtime: -1, value: undefined }
export async function storedOptions(dir = magpieConfigDir(), self = SELF) {
  const file = join(dir, "plugins.json")
  try {
    const mtime = (await stat(file)).mtimeMs
    if (stored.file === file && stored.mtime === mtime) return stored.value
    const list = JSON.parse(await readFile(file, "utf8"))?.plugins
    const mine = Array.isArray(list) ? list.find((p) => {
      const spec = typeof p === "string" ? p : p?.spec
      return norm(spec) === norm(self) || String(spec ?? "").startsWith(PACKAGE)
    }) : undefined
    const value = mine && typeof mine === "object" && mine.options && typeof mine.options === "object" ? mine.options : undefined
    stored = { file, mtime, value }
    return value
  } catch {
    return undefined
  }
}

// 宿主里的单例：magpie 启动插件宿主时调 server()，网关每次有请求时钩子也会走到这里。
// 选项每次都更新；端口变了就换一个服务。端口被占时（另一个 magpie 命令自带的宿主
// 正好短暂占着）隔几秒重试几次，都不行就等下次调用；任何失败都不抛错。
const RETRIES = [2000, 5000, 15000]
let current = null // { port, ready }
let latestOptions

async function startWithRetry(port) {
  for (let i = 0; ; i++) {
    try {
      return await startService({ port, getOptions: async () => (await storedOptions()) ?? latestOptions })
    } catch (e) {
      if (e?.code !== "EADDRINUSE" || i >= RETRIES.length) throw e
      await new Promise((resolve) => { setTimeout(resolve, RETRIES[i]).unref?.() })
    }
  }
}

export function ensureService(rawOptions) {
  latestOptions = rawOptions
  const port = options(rawOptions).port
  if (current?.port === port) return current.ready
  const previous = current
  const ready = (async () => {
    if (previous) await previous.ready.then((s) => s?.close(), () => {})
    return startWithRetry(port)
  })().catch(() => { if (current?.ready === ready) current = null; return null })
  current = { port, ready }
  return ready
}
