# Agent Notify · magpie 插件

Agent 的回合完成、失败、等待授权或等待输入时，发一条 Windows 系统通知和一条企业微信纯文本消息。WSL 与 Windows 两侧的 CLI 共用这一份配置。

```text
✅ Claude Code 完成 · agent-notify
用时 12 分钟
模型：claude-fable-5-2
来源：WSL · Ubuntu-24.04

已修复两个测试，见 app.ts。
```

## 安装

本地原位加载，不发布到 npm 或社区市场。需要 magpie 0.1.3443 或更新版本。

```bash
magpie plugin add ./magpie-plugin
```

magpie 运行在 Windows、仓库在 WSL 里时，用 `\\wsl.localhost\<发行版>\...\agent-notify\magpie-plugin` 这样的路径。改了插件源码后不用重装：按路径添加的插件，magpie 在下一次用到它时会发现文件变了，自动用新文件重新加载（0.1.4623 上实测，有网关流量时保存后几秒内生效）；没生效时，在 app 里把插件关掉再打开。这也意味着这个目录里每一次保存都会直接上线，包括改到一半的。

然后在 magpie 侧边栏的「Agent 通知」页面打开渠道、填企业微信群机器人的 Webhook 地址。也可以用命令：

```bash
magpie plugin options agent-notify-magpie '{"channels":{"toast":{"enabled":true},"wecom":{"enabled":true,"webhook":"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=…"}}}'
```

## 两个事件来源

**网关**：走 magpie 网关的 Agent，插件在网关上判断回合什么时候结束，不需要在 Agent 里装任何东西。它只能看到「回合完成」「回合失败」和「账号需要重新登录」。

**Agent 的 hook**：「等待授权」「等待输入」只发生在 CLI 本地，网关看不到；不走网关的 Agent（比如直连上游的 Codex、OMO）网关也看不到。这些由仓库里的 Go 程序 `agent-notify` 通过各 Agent 自己的 hook 上报给插件。在需要的每一侧（WSL、Windows）各运行一次：

```bash
agent-notify install --agent claude --magpie
agent-notify install --agent codex --magpie
agent-notify install --agent omo --magpie
agent-notify test magpie
```

这一侧只装 hook，不填渠道和 Webhook。插件不在时，`agent-notify` 把本机订阅的事件降级为本机 Windows 通知。

hook 连的是 `127.0.0.1`。magpie 在 Windows、Agent 在 WSL 时，WSL 要用 mirrored 网络（`%USERPROFILE%\.wslconfig` 的 `[wsl2]` 下 `networkingMode=mirrored`）才连得到；默认的 NAT 网络下连不到，事件会一直走上面的降级。

### 同一个回合只发一条

装了 hook 的会话，回合结束由 hook 报，网关不再重复发：

- 同一个会话有过 hook 上报，或同名 Agent 在同一侧（WSL / Windows）有过上报，12 小时内网关判定的「完成」不发。网关的会话 id 就是 Agent 自己的会话 id，所以两边名字不同也认得出来：桌面版 Code 标签页里跑的 Claude Code，hook 报的是 `claude`，网关叫它 `claude-desktop`。
- 「失败」只让给自己会报失败的 hook（Claude、OMO）。Codex 的 hook 没有失败事件，网关判定的失败对它照发；它的 hook 刚报过「已中断」（用户按了 Esc）时，那次失败不发。
- 插件宿主重启后不记得谁有 hook：同一会话的回合结束，网关刚发过，随后 hook 报的在 `dedupeSeconds` 内不再发。

同一侧不要把独立模式（`--channels`）和插件混着用：独立模式的 hook 自己发通知，插件不知道，网关还会再发一条。

## 选项

```jsonc
{
  "enabled": true,              // false：什么都不发，其余设置保留
  "channels": {
    "toast": { "enabled": true },                  // Windows 系统通知
    "wecom": { "enabled": false, "webhook": "" }   // 企业微信群机器人，msgtype: text
  },
  "events": ["permission_required", "input_required", "run_completed", "run_failed"],
                                // 另有 run_interrupted、subagent_completed、tool_failed
  "agents": [],                 // 留空为全部；否则只限这些 id（claude、codex、omo…）
  "minSeconds": 0,              // 短于它的回合不通知；等待授权和等待输入不受影响
  "includeText": true,          // 附上模型回复的开头（去掉 markdown 标记的纯文本）
  "maxTextLength": 200,
  "cooldownSeconds": 30,        // 网关来源：同一会话的通知间隔
  "dedupeSeconds": 10,          // hook 来源：同一条内容的去重窗口
  "failureHoldSeconds": 60,     // 网关来源的失败先压这么久，Agent 自己重试成功就丢掉
  "accounts": true,             // 订阅账号需要重新登录时通知
  "accountCooldownMinutes": 60,
  "allOrigins": false,          // 默认只通知本机自己的 Agent（见下）；true：其他网关密钥和其他设备的请求也通知
  "lang": "zh",                 // zh 或 en
  "port": 3497                  // 接收 hook 上报的本地端口
}
```

企业微信地址只接受 `https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=…`；填了别的地址这个渠道不发送。

「本机自己的 Agent」按 magpie 给请求标的来源认：`local`（本机上不带网关密钥的请求）和 `magpie-key`（magpie 自己生成、写进本机 Agent 的那把共享密钥，且请求来自本机）。其他具名密钥和其他设备的请求（`gateway-key`、`remote`）默认不通知，比如另一台电脑经本机 magpie 转发的会话。

## 排错

该来的通知没来时，看 `~/.agent-notify/magpie-plugin.log`（在 magpie 所在系统的用户主目录下）。发出去的事件记着各渠道的结果；没发的记着原因：

| 原因 | 含义 |
| --- | --- |
| `covered by hooks` | 这个会话由 hook 报，网关判定的这次不发 |
| `interrupted` | hook 刚报过用户中断，网关看到的那次失败不发 |
| `duplicate` | 去重窗口或冷却期内发过了 |
| `event off` / `agent filtered` / `too short` | 被 `events`、`agents`、`minSeconds` 挡掉 |
| `ignored (not this computer's own agent)` | 来源不是本机自己的 Agent；每种（Agent、来源）只记一次 |

Go 一侧转发失败的原因在那一侧的 `~/.agent-notify/agent-notify.log`，`agent-notify doctor` 会报告端点文件和端口能否连上。

## 隐私

通知带 Agent 名、项目目录名、用时、模型和来源。`includeText` 默认开启，会附上模型回复的开头；群里有其他人时请关掉。日志只记来源、Agent、事件、各渠道的结果和没发的原因，不记正文、目录和 Webhook 地址。

## 它怎么运行

- `notify.hooks.mjs`：magpie 的异步网关钩子，跑在 magpie 的 Bun 宿主里。回合状态只在内存里，magpie 重启后忘掉。钩子不改请求，发送在后台进行，每个渠道 10 秒超时。
- `service.mjs`：只监听 `127.0.0.1` 的本地服务，接收 hook 上报。端口和随机口令写在 `~/.agent-notify/magpie-endpoint.json`（magpie 所在系统的用户主目录），每个请求都要带口令；`Host` 只认本服务自己。
- `hub.mjs`：两个来源在这里汇合，统一过滤、算用时、去重、发送。
- `toast.mjs`：经 Windows PowerShell 调 WinRT。请求内容以 Base64 走 stdin，脚本是固定常量。

## 已知限制

- Windows 系统通知需要 Windows 或 WSL 互操作；其他系统上这个渠道报错并记日志。
- 终端里每条 `magpie` 命令会启动自己的插件宿主，可能短暂占用上报端口；应用里的宿主会在几秒内重试接回。这期间上报的事件照常发送，但「回合开始」记在了那个临时宿主里，随后一条完成通知可能不带用时。
- Claude Code 后台子代理未结束时主会话的 `Stop` hook 会先报完成（网关来源没有这个问题）。
- 没装 hook 的 Agent 按 Esc：magpie 只告诉钩子「回复没传完」，和上游失败分不开。`failureHoldSeconds` 内会话没有继续，就会收到一条「失败」。
- magpie 换插件宿主时（更新插件、改选项、内存回收），旧宿主如果还在应答调用，最多再活 10 分钟。这期间两个宿主各记各的，同一个回合可能发两条。
- 请求不带会话 id 的 Agent 只能按「同名 Agent 的同一侧」认 hook；宿主重启后，它正在进行的那个回合可能发两条。
- 打开 `allOrigins` 后，另一台设备转发来的请求 Agent 名显示为 Magpie，来源按工作目录的形态推断，可能被标成本机的 WSL 或 Windows。

## 测试

```bash
bun test magpie-plugin
```

`tests/turns.upstream.test.mjs` 是回合判定的回归测试，夹具是从真实 claude / codex 录下的请求体。

## 来源与许可

MIT。`turn.mjs`、`fixtures/`、回合跟踪逻辑、`page/style.css` 和 `tests/turns.upstream.test.mjs` 的测试体来自 [@magpie-community/gateway-notify](https://github.com/magpie-community/plugins/tree/main/packages/notify) 0.1.5（MIT，见 `LICENSE.magpie-community`）。

插件图标 `page/icon.svg` 是 Google [Material Icons](https://github.com/google/material-design-icons) 的 Notifications，与该仓库的 `src/social/notifications/materialicons/24px.svg` 逐字节相同（Apache License 2.0，见 `LICENSE.material-icons`）。它存在包里，不从网络加载：侧边栏页签用这个文件（`magpie.page.icon`），插件列表用内嵌在 `package.json` 里的同一份（`magpie.icon`）。图标是单色的，magpie 会按界面的文字颜色绘制。
