// Windows Toast：通过 Windows PowerShell 调 WinRT 弹出。magpie 跑在 Windows 上时
// 直接调 powershell.exe；跑在 WSL 里时走互操作。请求整体以 Base64 经 stdin 传入，
// 脚本本身是固定常量，用户文本只会进 XML 文本节点，不会变成可执行代码。
// 脚本与 Go 端 internal/notify/windows.go 保持一致，两边共用同一组通知身份和图标缓存。
import { spawn } from "node:child_process"
import { createHash } from "node:crypto"
import { existsSync, readFileSync } from "node:fs"
import { cut, originLabel } from "./message.mjs"

const WSL_POWERSHELL = "/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe"
const TIMEOUT = 10_000

const SCRIPT = `$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$request = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String([Console]::In.ReadToEnd())) | ConvertFrom-Json
$appID = $request.AppID
$iconPath = $null
if ($request.Icon) {
    $iconDir = Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'AgentNotify\\icons'
    [IO.Directory]::CreateDirectory($iconDir) | Out-Null
    $iconPath = Join-Path $iconDir $request.IconFile
    if (-not [IO.File]::Exists($iconPath)) {
        $temporaryIcon = $iconPath + '.' + [Guid]::NewGuid().ToString('N') + '.tmp'
        try {
            [IO.File]::WriteAllBytes($temporaryIcon, [Convert]::FromBase64String($request.Icon))
            try { [IO.File]::Move($temporaryIcon, $iconPath) } catch {
                if (-not [IO.File]::Exists($iconPath)) { throw }
            }
        } finally {
            if ([IO.File]::Exists($temporaryIcon)) { [IO.File]::Delete($temporaryIcon) }
        }
    }
}
$key = 'HKCU:\\Software\\Classes\\AppUserModelId\\' + $appID
if (-not (Test-Path $key)) { New-Item -Path $key -Force | Out-Null }
New-ItemProperty -Path $key -Name 'DisplayName' -Value $request.DisplayName -PropertyType String -Force | Out-Null
if ($iconPath) {
    New-ItemProperty -Path $key -Name 'IconUri' -Value $iconPath -PropertyType String -Force | Out-Null
}
[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
[Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime] | Out-Null
$xml = New-Object Windows.Data.Xml.Dom.XmlDocument
$xml.LoadXml($request.XML)
$toast = [Windows.UI.Notifications.ToastNotification]::new($xml)
$toast.ExpirationTime = [DateTimeOffset]::Now.AddMinutes(5)
$notifier = [Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier($appID)
$setting = $null
try { $setting = $notifier.get_Setting() } catch {
    if ($_.Exception.InnerException.HResult -ne -2147023728) { throw }
}
if ($null -ne $setting -and $setting -ne [Windows.UI.Notifications.NotificationSetting]::Enabled) { throw 'Windows notifications are disabled for Agent Notify or by policy' }
$notifier.Show($toast)
`

const esc = (s) => String(s).replace(/[<>&"']/g, (c) => ({ "<": "&lt;", ">": "&gt;", "&": "&amp;", '"': "&quot;", "'": "&apos;" })[c])
// XML 1.0 不允许的控制字符会让整条通知 LoadXml 失败
const clean = (s) => String(s ?? "").replace(/[\u0000-\u0008\u000B\u000C\u000E-\u001F\uFFFE\uFFFF]/g, "")

const identities = {
  claude: { suffix: ".Claude", name: "Claude Notify", icon: "claude.png" },
  codex: { suffix: ".Codex", name: "Codex Notify", icon: "codex.png" },
  omo: { suffix: ".OMO", name: "OMO Notify", icon: "omo.png" },
}
const icons = new Map()
function identity(agent) {
  const id = identities[agent]
  const out = { AppID: `AgentNotify.Desktop${id?.suffix ?? ""}`, DisplayName: id?.name ?? "Agent Notify" }
  if (!id) return out
  if (!icons.has(id.icon)) {
    try {
      const bytes = readFileSync(new URL(`./icons/${id.icon}`, import.meta.url))
      icons.set(id.icon, { file: `${createHash("sha256").update(bytes).digest("hex")}.png`, data: bytes.toString("base64") })
    } catch { icons.set(id.icon, null) }
  }
  const icon = icons.get(id.icon)
  return icon ? { ...out, IconFile: icon.file, Icon: icon.data } : out
}

// m: build() 的结果。Toast 自带时间，底部一行放项目和来源。正文只有文字：
// Agent 的图标只作为通知身份的小图标显示在顶部来源名旁，不在正文前再放一张大图。
export function request(m) {
  const id = identity(m.agent)
  const origin = originLabel(m.origin)
  const attribution = cut([m.project, origin].filter(Boolean).join(" · "), 60)
  let xml = `<toast duration="long"><visual><binding template="ToastGeneric">`
  xml += `<text>${esc(cut(clean(m.title), 120))}</text><text>${esc(cut(clean(m.body), 1200))}</text>`
  if (attribution) xml += `<text placement="attribution">${esc(clean(attribution))}</text>`
  xml += `</binding></visual><audio src="ms-winsoundevent:Notification.Default"/></toast>`
  return { ...id, XML: xml }
}

export function powershell(platform = process.platform) {
  if (platform === "win32") return "powershell.exe"
  return existsSync(WSL_POWERSHELL) ? WSL_POWERSHELL : ""
}

const spawnRun = (file, args, input) => new Promise((resolve) => {
  let done = false
  const finish = (r) => { if (!done) { done = true; clearTimeout(timer); resolve(r) } }
  let child
  try {
    child = spawn(file, args, { stdio: ["pipe", "ignore", "ignore"], windowsHide: true })
  } catch (e) { resolve({ ok: false, error: `spawn: ${e?.code ?? "failed"}` }); return }
  const timer = setTimeout(() => { try { child.kill() } catch {} finish({ ok: false, error: "timeout" }) }, TIMEOUT)
  child.on("error", (e) => finish({ ok: false, error: `spawn: ${e?.code ?? "failed"}` }))
  // PowerShell 失败时可能把请求内容打出来，所以只报退出码
  child.on("close", (code) => finish(code === 0 ? { ok: true } : { ok: false, error: `powershell exit ${code}` }))
  child.stdin.on("error", () => {})
  child.stdin.end(input)
})
let run = spawnRun
// 测试用：换掉起进程的那一步；不传参数恢复
export const setRunner = (fn) => { run = fn ?? spawnRun }

export async function send(m) {
  const file = powershell()
  if (!file) return { ok: false, error: "Windows PowerShell not found (Windows or WSL interop required)" }
  const input = Buffer.from(JSON.stringify(request(m)), "utf8").toString("base64")
  const encoded = Buffer.from(SCRIPT, "utf16le").toString("base64")
  return run(file, ["-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-EncodedCommand", encoded], input)
}
