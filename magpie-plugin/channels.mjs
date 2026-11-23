// 通知发到哪里。两个渠道：Windows Toast 和企业微信群机器人。
// send() 从不抛错，只回答 { ok, error? }；错误里不带 Webhook 地址。
import { send as toast } from "./toast.mjs"

const TIMEOUT = 10_000
// 企业微信 text 消息的上限是 2048 个 UTF-8 字节
const WECOM_BYTES = 2048

// 按字节截断，不拆开一个多字节字符
export function cutBytes(text, max) {
  const bytes = Buffer.from(text, "utf8")
  if (bytes.length <= max) return text
  let end = max - Buffer.byteLength("…")
  while (end > 0 && (bytes[end] & 0xc0) === 0x80) end--
  return `${bytes.subarray(0, end).toString("utf8")}…`
}

// 纯文本消息（msgtype: text）：微信里也能直接显示，不用 markdown。
async function wecom(c, m) {
  const content = cutBytes(`${m.emoji} ${m.title}${m.body ? `\n${m.body}` : ""}`, WECOM_BYTES)
  let res
  try {
    res = await fetch(c.webhook, {
      method: "POST",
      headers: { "content-type": "application/json; charset=utf-8" },
      body: JSON.stringify({ msgtype: "text", text: { content } }),
      signal: AbortSignal.timeout(TIMEOUT),
    })
  } catch (e) {
    // 传输层错误可能带着完整地址，只报类别
    return { ok: false, error: e?.name === "TimeoutError" ? "timeout" : "request failed" }
  }
  if (!res.ok) return { ok: false, error: `http ${res.status}` }
  let data
  try { data = await res.json() } catch {}
  if (typeof data?.errcode !== "number") return { ok: false, error: "invalid response" }
  return data.errcode === 0 ? { ok: true } : { ok: false, error: `errcode ${data.errcode}` }
}

const kinds = { toast: (_c, m) => toast(m), wecom }

// 测试用：整体替换某个渠道的发送函数；传 undefined 恢复
const real = { ...kinds }
export const setChannel = (name, fn) => { kinds[name] = fn ?? real[name] }

export async function send(name, config, message) {
  try {
    const kind = kinds[name]
    if (!kind) return { ok: false, error: "unknown channel" }
    return await kind(config, message)
  } catch {
    return { ok: false, error: "channel failed" }
  }
}
