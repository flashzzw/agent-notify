// 插件选项：唯一的一份配置，存在 magpie 的 plugins.json 里，由配置页或
// `magpie plugin options` 修改。这里只做归一化，任何输入都不会抛错。

// 可订阅的事件。网关只能看到 run_completed / run_failed；其余来自 Agent 的 hook。
export const EVENTS = [
  "permission_required",
  "input_required",
  "run_completed",
  "run_failed",
  "run_interrupted",
  "subagent_completed",
  "tool_failed",
]

export const DEFAULT_PORT = 3497

export const defaults = {
  enabled: true,
  port: DEFAULT_PORT,
  lang: "zh",
  channels: {
    toast: { enabled: true },
    wecom: { enabled: false, webhook: "" },
  },
  events: ["permission_required", "input_required", "run_completed", "run_failed"],
  agents: [],
  minSeconds: 0,
  cooldownSeconds: 30,
  dedupeSeconds: 10,
  includeText: true,
  maxTextLength: 200,
  failureHoldSeconds: 60,
  accounts: true,
  accountCooldownMinutes: 60,
  allOrigins: false,
}

const isObj = (v) => v !== null && typeof v === "object" && !Array.isArray(v)
const num = (v, fallback, min, max) => {
  const n = Number(v)
  return Number.isFinite(n) ? Math.max(min, Math.min(max, n)) : fallback
}
const list = (v) => Array.isArray(v) ? v.map(String).map((s) => s.trim()).filter(Boolean)
  : typeof v === "string" && v ? v.split(",").map((s) => s.trim()).filter(Boolean) : null

// 只接受企业微信群机器人的 HTTPS 地址；回环 HTTP 仅用于本地测试。
// 地址里带着机器人密钥，所以任何报错都不回显它。
export function wecomWebhookOK(value) {
  let u
  try { u = new URL(String(value ?? "")) } catch { return false }
  if (u.username || u.password || u.hash) return false
  if (u.protocol === "https:" && u.hostname === "qyapi.weixin.qq.com" && u.pathname === "/cgi-bin/webhook/send" && u.searchParams.get("key")) return true
  return u.protocol === "http:" && ["127.0.0.1", "localhost", "[::1]"].includes(u.hostname)
}

export function options(raw) {
  const r = isObj(raw) ? raw : {}
  const ch = isObj(r.channels) ? r.channels : {}
  const toast = isObj(ch.toast) ? ch.toast : {}
  const wecom = isObj(ch.wecom) ? ch.wecom : {}
  const webhook = typeof wecom.webhook === "string" ? wecom.webhook.trim() : ""
  const o = {
    enabled: r.enabled !== false,
    port: Math.trunc(num(r.port, DEFAULT_PORT, 1, 65535)) || DEFAULT_PORT,
    lang: r.lang === "en" ? "en" : "zh",
    channels: {
      toast: { enabled: toast.enabled !== false },
      // 没填地址或地址不合规就当作没开，免得把通知内容发到别处
      wecom: { enabled: wecom.enabled === true && wecomWebhookOK(webhook), webhook },
    },
    events: (list(r.events) ?? defaults.events).filter((e) => EVENTS.includes(e)),
    agents: list(r.agents) ?? [],
    minSeconds: num(r.minSeconds, defaults.minSeconds, 0, 24 * 3600),
    cooldownSeconds: num(r.cooldownSeconds, defaults.cooldownSeconds, 0, 3600),
    dedupeSeconds: num(r.dedupeSeconds, defaults.dedupeSeconds, 0, 3600),
    includeText: r.includeText !== false,
    maxTextLength: Math.trunc(num(r.maxTextLength, defaults.maxTextLength, 0, 2000)),
    failureHoldSeconds: num(r.failureHoldSeconds, defaults.failureHoldSeconds, 0, 600),
    accounts: r.accounts !== false,
    accountCooldownMinutes: num(r.accountCooldownMinutes, defaults.accountCooldownMinutes, 0, 24 * 60),
    allOrigins: r.allOrigins === true,
  }
  return o
}

// 实际会发送的渠道名；总开关关掉时为空。
export function activeChannels(o) {
  if (!o.enabled) return []
  return ["toast", "wecom"].filter((name) => o.channels[name].enabled)
}
