// Agent Notify 的配置页：直接编辑插件选项，不用碰任何文件。页面在沙箱框架里，
// 没有网络，只通过 _magpie/bridge.js 读写本插件自己的选项；通知不从页面发出。
// 保存时整份写回：页面上没有对应控件的字段（比如 port、cooldownSeconds）原样保留。
// 样式沿用 @magpie-community/gateway-notify 的 page/style.css（MIT）。
(function () {
  "use strict"
  var root = document.getElementById("root")

  // 与 options.mjs 的默认值保持一致
  var DEFAULTS = {
    enabled: true, lang: "zh",
    channels: { toast: { enabled: true }, wecom: { enabled: false, webhook: "" } },
    events: ["permission_required", "input_required", "run_completed", "run_failed"],
    agents: [], minSeconds: 0, includeText: true, maxTextLength: 200, accounts: true
  }
  // [事件, 中文, English, 只有 hook 上报才有]
  var EVENTS = [
    ["run_completed", "回合完成", "Turn done", false],
    ["run_failed", "回合失败", "Turn failed", false],
    ["permission_required", "等待授权", "Needs approval", true],
    ["input_required", "等待输入", "Needs input", true],
    ["run_interrupted", "运行中断", "Interrupted", true],
    ["subagent_completed", "子代理完成", "Subagent done", true],
    ["tool_failed", "工具失败", "Tool failed", true]
  ]
  var TEXT = {
    "标题": "Agent Notify",
    "开启通知": "Notifications on",
    "渠道": "Channels",
    "Windows 系统通知": "Windows notification",
    "企业微信群机器人": "WeCom group robot",
    "Webhook 地址": "Webhook URL",
    "纯文本消息，不使用 markdown": "Plain text messages, never markdown",
    "地址不是企业微信群机器人的 Webhook，不会发送": "Not a WeCom group robot webhook; nothing is sent",
    "通知哪些事件": "What to notify",
    "需要在 Agent 里安装 hook 上报": "needs the agent's hook installed",
    "事件说明": "The gateway sees only turns that finish or fail. The others are reported by the agent's own hooks: run agent-notify install --agent <claude|codex|omo> --magpie on each side (WSL and Windows).",
    "内容与时机": "Content and timing",
    "只通知超过（秒）的回合": "Only turns longer than (seconds)",
    "附上模型回复的开头": "Include the start of the reply",
    "回复最多保留（字）": "Reply cut to (characters)",
    "账号需要重新登录时通知": "Account needs signing in again",
    "只限这些 Agent（id，逗号分隔，留空为全部）": "Only these agents (ids, comma separated; empty for all)",
    "通知语言": "Language of the notification",
    "保存": "Save", "已保存": "Saved", "有未保存的更改": "Unsaved changes",
    "保存失败": "Could not save", "读取设置失败": "Could not read the settings"
  }
  var HINT_ZH = "网关只能看到回合完成和失败。其余事件由 Agent 自己的 hook 上报：在 WSL 和 Windows 两侧分别运行 agent-notify install --agent <claude|codex|omo> --magpie。"
  var ui = "zh"
  function t(s) { return ui === "zh" ? (s === "事件说明" ? HINT_ZH : s === "标题" ? "Agent 通知" : s) : (TEXT[s] || s) }

  var saved = null // 插件当前保存的选项
  var draft = null // 页面上正在编辑的
  var note = ["", false]
  var busy = false

  function el(tag, cls, text) {
    var n = document.createElement(tag)
    if (cls) n.className = cls
    if (text != null) n.textContent = text
    return n
  }
  function obj(v) { return v && typeof v === "object" && !Array.isArray(v) ? v : {} }
  function same(a, b) { return JSON.stringify(a) === JSON.stringify(b) }
  function fill(o) {
    var r = Object.assign({}, DEFAULTS, obj(o))
    var ch = obj(r.channels)
    r.channels = Object.assign({}, ch, {
      toast: Object.assign({}, DEFAULTS.channels.toast, obj(ch.toast)),
      wecom: Object.assign({}, DEFAULTS.channels.wecom, obj(ch.wecom))
    })
    r.events = Array.isArray(r.events) ? r.events.map(String) : DEFAULTS.events.slice()
    r.agents = Array.isArray(r.agents) ? r.agents.map(String) : typeof r.agents === "string" && r.agents ? r.agents.split(",").map(function (s) { return s.trim() }).filter(Boolean) : []
    return r
  }
  // 与 options.mjs 的 wecomWebhookOK 相同的判断
  function webhookOK(value) {
    try {
      var u = new URL(String(value || ""))
      if (u.username || u.password || u.hash) return false
      if (u.protocol === "https:" && u.hostname === "qyapi.weixin.qq.com" && u.pathname === "/cgi-bin/webhook/send" && u.searchParams.get("key")) return true
      return u.protocol === "http:" && ["127.0.0.1", "localhost", "[::1]"].indexOf(u.hostname) >= 0
    } catch (e) { return false }
  }
  function change() { note = ["", false]; renderBar() }

  function toggle(on, label, fn) {
    var b = el("button", "switch")
    b.type = "button"
    b.setAttribute("role", "switch")
    b.setAttribute("aria-checked", on ? "true" : "false")
    b.setAttribute("aria-label", label)
    b.addEventListener("click", function () {
      var next = b.getAttribute("aria-checked") !== "true"
      b.setAttribute("aria-checked", next ? "true" : "false")
      fn(next)
    })
    return b
  }
  function switchRow(label, on, fn, sub) {
    var row = el("div", "row")
    var name = el("span", "name", label)
    if (sub) name.appendChild(el("span", "sub", sub))
    row.append(name, toggle(on, label, fn))
    return row
  }
  function numberRow(label, key, max) {
    var row = el("div", "row")
    var l = el("label", "", label)
    var i = el("input")
    i.type = "number"; i.min = "0"; i.max = String(max); i.value = String(draft[key])
    i.id = "n-" + key; l.htmlFor = i.id
    i.addEventListener("input", function () { draft[key] = Math.max(0, Math.min(max, Number(i.value) || 0)); change() })
    row.append(l, i)
    return row
  }

  var bar = el("div", "bar")
  function renderBar() {
    bar.textContent = ""
    var d = !same(draft, saved)
    var s = el("span", "status" + (note[1] ? " error" : ""), note[0] || (d ? t("有未保存的更改") : ""))
    var b = el("button", "primary", t("保存"))
    b.type = "button"
    b.disabled = !d || busy
    b.addEventListener("click", function () { save(draft, true) })
    bar.append(s, b)
  }
  function save(next, full) {
    busy = true; renderBar()
    return window.magpie.options.set(next).then(function (got) {
      saved = fill(got)
      if (full) draft = fill(got)
      note = [t("已保存"), false]
    }).catch(function () {
      note = [t("保存失败"), true]
    }).then(function () { busy = false; if (full) render(); else renderBar() })
  }

  function render() {
    root.textContent = ""
    document.documentElement.lang = ui
    var head = el("header")
    head.appendChild(el("h1", "", t("标题")))
    head.appendChild(toggle(draft.enabled !== false, t("开启通知"), function (v) {
      // 总开关立即保存，而且只保存它自己
      draft.enabled = v
      save(Object.assign({}, saved, { enabled: v }), false)
    }))
    root.appendChild(head)

    var ch = el("section")
    ch.appendChild(el("div", "head")).appendChild(el("h2", "", t("渠道")))
    var toastBox = el("div", "box chan")
    toastBox.appendChild(switchRow(t("Windows 系统通知"), draft.channels.toast.enabled !== false, function (v) { draft.channels.toast.enabled = v; change() }))
    ch.appendChild(toastBox)
    var wecom = draft.channels.wecom
    var wecomBox = el("div", "box chan")
    var warn = el("span", "warn")
    wecomBox.appendChild(switchRow(t("企业微信群机器人"), wecom.enabled === true, function (v) { wecom.enabled = v; check(); change() }, t("纯文本消息，不使用 markdown")))
    var fields = el("div", "fields")
    var lab = el("label", "", t("Webhook 地址"))
    var inp = el("input")
    inp.id = "wecom-webhook"; lab.htmlFor = inp.id
    inp.type = "password"; inp.autocomplete = "off"; inp.spellcheck = false
    inp.placeholder = "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=…"
    inp.value = wecom.webhook || ""
    inp.addEventListener("input", function () { wecom.webhook = inp.value.trim(); check(); change() })
    fields.append(lab, inp)
    wecomBox.appendChild(fields)
    var warnRow = el("div", "hint"); warnRow.appendChild(warn); wecomBox.appendChild(warnRow)
    function check() {
      warn.textContent = wecom.enabled && wecom.webhook && !webhookOK(wecom.webhook) ? t("地址不是企业微信群机器人的 Webhook，不会发送") : ""
      warnRow.hidden = !warn.textContent
    }
    check()
    ch.appendChild(wecomBox)
    root.appendChild(ch)

    var ev = el("section")
    ev.appendChild(el("div", "head")).appendChild(el("h2", "", t("通知哪些事件")))
    var evBox = el("div", "box")
    EVENTS.forEach(function (e) {
      evBox.appendChild(switchRow(ui === "zh" ? e[1] : e[2], draft.events.indexOf(e[0]) >= 0, function (v) {
        draft.events = EVENTS.map(function (x) { return x[0] }).filter(function (k) { return k === e[0] ? v : draft.events.indexOf(k) >= 0 })
        change()
      }, e[3] ? t("需要在 Agent 里安装 hook 上报") : ""))
    })
    ev.appendChild(evBox)
    ev.appendChild(el("p", "hint", t("事件说明")))
    root.appendChild(ev)

    var w = el("section")
    w.appendChild(el("div", "head")).appendChild(el("h2", "", t("内容与时机")))
    var box = el("div", "box")
    box.appendChild(numberRow(t("只通知超过（秒）的回合"), "minSeconds", 86400))
    box.appendChild(switchRow(t("附上模型回复的开头"), draft.includeText !== false, function (v) { draft.includeText = v; change() }))
    box.appendChild(numberRow(t("回复最多保留（字）"), "maxTextLength", 2000))
    box.appendChild(switchRow(t("账号需要重新登录时通知"), draft.accounts !== false, function (v) { draft.accounts = v; change() }))
    var ag = el("div", "row")
    var al = el("label", "", t("只限这些 Agent（id，逗号分隔，留空为全部）"))
    var ai = el("input"); ai.type = "text"; ai.placeholder = "claude, codex, omo"; ai.style.maxWidth = "200px"; ai.spellcheck = false
    ai.id = "agents"; al.htmlFor = ai.id
    ai.value = draft.agents.join(", ")
    ai.addEventListener("input", function () { draft.agents = ai.value.split(",").map(function (s) { return s.trim() }).filter(Boolean); change() })
    ag.append(al, ai)
    box.appendChild(ag)
    var lr = el("div", "row")
    lr.appendChild(el("span", "name", t("通知语言")))
    var pick = el("div", "pick")
    pick.setAttribute("role", "group")
    pick.setAttribute("aria-label", t("通知语言"))
    ;[["zh", "中文"], ["en", "English"]].forEach(function (l) {
      var b = el("button", "", l[1])
      b.type = "button"
      b.setAttribute("aria-pressed", draft.lang === l[0] ? "true" : "false")
      b.addEventListener("click", function () { draft.lang = l[0]; change(); render() })
      pick.appendChild(b)
    })
    lr.appendChild(pick)
    box.appendChild(lr)
    w.appendChild(box)
    root.appendChild(w)

    root.appendChild(bar)
    renderBar()
  }

  function theme(th) { ui = th && typeof th.lang === "string" && th.lang.toLowerCase().indexOf("zh") !== 0 ? "en" : "zh" }
  window.magpie.ready.then(function (th) {
    theme(th)
    if (window.magpie.onTheme) window.magpie.onTheme(function (next) { theme(next); if (draft) render() })
    return window.magpie.options.get()
  }).then(function (o) {
    saved = fill(o); draft = fill(o); render()
  }).catch(function () {
    root.textContent = t("读取设置失败")
  })
})()
