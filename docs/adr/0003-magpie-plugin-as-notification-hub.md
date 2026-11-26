# ADR 0003：magpie 插件作为通知中枢

## 状态

已接受

## 背景

首期（ADR 0001）每一侧各有一份 `~/.agent-notify/config.yaml`，Webhook 按 Agent 保存：WSL 与 Windows 各三个 Agent，最多要填六次，容易漂移。用户的模型流量主入口已是 magpie 本地网关，它运行在 Windows 上，两侧的 CLI 都能连到。参考 `@magpie-community/gateway-notify`：配置只有一份，存在插件选项里，插件在网关上判断回合结束。

## 决策

1. **插件是渠道、文案、过滤和去重的唯一所在**，放在仓库的 `magpie-plugin/`，本地原位加载。渠道仍只有 Windows Toast 与企业微信（`msgtype: text`）。
2. **两个事件来源汇合到一处**（`hub.mjs`）：
   - 网关钩子判断回合完成、失败和账号掉登录；回合判定逻辑与录制夹具移植自 gateway-notify 0.1.5。
   - 网关看不到「等待授权 / 等待输入」，也看不到不走网关的 Agent。这些由 Go 程序的 hook 以「转发模式」上报到插件的本地服务。
3. **转发协议**：`POST http://127.0.0.1:<port>/event`，口令在请求头 `x-agent-notify-token`。端口与口令由插件写入 magpie 所在系统的 `~/.agent-notify/magpie-endpoint.json`；WSL 侧通过 `magpie.endpoint_file` 指向 Windows 用户目录下的那一份。请求体是未排版的原始字段（Agent、事件、会话、目录、来源、工具、原始文本、模型）。
4. **去重规则**：装了 hook 的会话，回合结束由 hook 报，网关不重复发。
   - 同一个会话有 hook 上报（网关的会话 id 就是 Agent 自己的会话 id），或同名 Agent 在同一侧（WSL / Windows）有 hook 上报的 12 小时内，网关判定的完成通知不再发。按会话认是因为两边给 Agent 起的名字可能不同：桌面版里跑的 Claude Code，hook 报 `claude`，网关叫它 `claude-desktop`。
   - 失败只让给自己会报失败的 hook（Claude、OMO）。Codex 的 hook 没有失败事件，网关判定的失败对它照发；hook 刚报过中断的那一次除外。
   - 同一会话的回合结束网关刚发过时，随后 hook 报的在去重窗口内不再发：宿主重启后中枢不记得谁有 hook。
5. **降级契约**：转发失败时，Go 程序把本机 `events` 里订阅的事件降级为本机 Windows Toast 并写日志；企业微信不参与降级；`turn_started` 既不通知也不记日志。
6. **新事件**：`turn_started`（Claude / Codex 的 `UserPromptSubmit`、OMO 主会话的 `agent_start`）只用于计算用时，只在开启转发时订阅；`tool_failed`（Claude 的 `PostToolUseFailure`）从 `run_failed` 拆出，默认不订阅。
7. **正文是纯文本**：模型回复去掉 markdown 标记后截断；`includeText` 默认开启，`minSeconds` 默认 0。
8. **Go 程序的独立模式保留**：不装 magpie 时仍按 ADR 0001 / 0002 工作。
9. **网关一侧只通知本机自己的 Agent**：magpie 标为 `local` 和 `magpie-key`（它自己生成、写进本机 Agent 的共享密钥，且请求来自本机）的请求。上游 gateway-notify 0.1.5 只认 `local`；开了共享之后本机 Agent 带的就是这把密钥，只认 `local` 会漏掉它们。其他密钥和其他设备的请求要显式打开 `allOrigins`。

## 已知取舍

- 插件的回合状态只在内存：magpie 重启时进行中的回合不通知。谁有 hook 也只记在内存里，宿主重启后要等下一次上报才重新知道，这段时间靠决策 4 的最后一条兜底。两个宿主并存时（被换下的宿主最多再活 10 分钟）各记各的，同一个回合可能发两条；把状态落盘可以解决，目前没有做。
- 网关钩子拿不到状态码和中止标记：没装 hook 的 Agent 按 Esc 和上游失败分不开，只能靠 `failureHoldSeconds` 压一段时间。
- hook 上报连的是插件所在系统的 `127.0.0.1`。WSL 中的 hook 通过 Windows 互操作上报到 Windows 上的插件，并在连接前核对 Windows 监听进程与端点文件记录的 PID，避免把 WSL 自己的转发端口误作插件。
- 插件不直接读 Agent 的 hook；每一侧仍要运行一次 `install --magpie`。
- 服务处理上报时自己读 magpie 的 `plugins.json` 取最新选项：选项变更后宿主要到下一个网关请求才重启，只靠 hook 上报的 Agent 等不到。这依赖 `plugins.json` 的 `{plugins:[{spec, options}]}` 形态，读不到时退回宿主启动时给的选项。
- 终端里的 `magpie` 命令各自启动插件宿主，会短暂占用上报端口；应用里的宿主按 2 / 5 / 15 秒重试接回。
- 点击聚焦终端仍不做（ADR 0001 第 4 条）。
