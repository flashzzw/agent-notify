# 使用与排错 / Usage and troubleshooting

命令前加 `agent-notify`；程序未加入 `PATH` 时，Windows 使用 `.\agent-notify.exe`，WSL 使用 `./agent-notify`。Run commands with the executable prefix appropriate to your shell.

## 安装位置与事件 / Scope and events

默认安装到当前用户。只对当前项目生效时，在项目根目录执行：

```bash
agent-notify install --agent claude --scope project --channels system
agent-notify install --agent codex --scope project --channels system
agent-notify install --agent omo --scope project --channels system
```

Project scope writes `.claude/settings.json`, `.codex/hooks.json` (and `config.toml`), or `.senpi/extensions/agent-notify.ts` under the current directory. These contain local executable/config paths; keep the generated entries out of shared configuration. Agent-side loading and trust rules still apply.

可用事件 / Event names:

| Event | 含义 | Agent | 默认订阅 / Default |
| --- | --- | --- | --- |
| `permission_required` | 等待授权 / Approval needed | Claude, Codex, OMO | 是 / Yes |
| `input_required` | 等待输入 / Input needed | Claude, OMO | 是 / Yes |
| `run_completed` | 任务完成 / Completed | Claude, Codex, OMO | 是 / Yes |
| `run_failed` | 运行失败 / Failed | Claude, OMO | 是 / Yes |
| `tool_failed` | 工具失败 / A tool call failed | Claude | 否 / No |
| `run_interrupted` | 运行中断 / Interrupted by the user | Codex, OMO | 否 / No |
| `subagent_completed` | 子代理完成 / Subagent finished | Claude, Codex, OMO | 否 / No |

`run_interrupted` / `subagent_completed` 为 v0.2 新增事件，默认不订阅：升级或全新安装都不会自动开启，需要显式勾选或通过 `--events` 指定。`run_interrupted` 在 Codex 来自 `Interrupt` hook（用户 ESC 中断），在 OMO 来自主会话 `aborted`；`subagent_completed` 来自 Claude Code / Codex 的 `SubagentStop` hook 与 OMO 的 worker 会话完成。Claude Code 没有 Interrupt 类 hook，因此不提供 `run_interrupted`。

```bash
agent-notify install --agent claude --channels system --events permission_required,run_completed,subagent_completed
```

`--channels` 和 `--events` 会替换对应 Agent 当前的选择；省略时沿用现有配置。Defaults for a new configuration subscribe to all events supported by that agent; channels must be selected explicitly.

企业微信使用 `msgtype: text` 纯文本消息，微信端可直接显示。示例：

```text
✅ Codex · 运行完成
消息内容：
任务已完成，请查看结果
时间：2026-11-10 14:30:00
项目：agent-notify
工作目录：/home/user/agent-notify
会话：example-session
```

缺失的项目、工作目录或会话字段会省略；消息总长度限制为 2048 UTF-8 字节，超长正文保留开头并以 `…` 结束。WeCom uses plain text, omits unavailable metadata, and truncates long content without splitting a Unicode character.

Windows 通知来源分别为 `Codex Notify`、`Claude Notify`、`OMO Notify`，顶部来源名称旁显示对应小图标，底部 attribution 行显示项目名（工作目录末段）。正文只有文字：只包含事件消息本身，不放图片，也不再附带工作目录与时间行——Windows 通知自带时间显示。OMO 图标由 `assets/omo-icon-light.svg` 转为 PNG 后内置。各自使用独立的通知身份，可在 Windows「设置 → 系统 → 通知」中分别控制。图标内置于程序，首次使用时写入 `%LOCALAPPDATA%\AgentNotify\icons`；WSL 也写入此 Windows 本地目录，无需保留旧版工具或单独安装图片。

## 通过 magpie 插件发送 / Sending through the magpie plugin

使用 [magpie](https://usemagpie.ai) 时，可以把渠道配置集中到 `magpie-plugin/` 插件里：WSL 与 Windows 两侧共用一份，企业微信 Webhook 只填一次。安装和选项见 [magpie-plugin/README.md](../magpie-plugin/README.md)。

插件装好后，在需要「等待授权 / 等待输入」通知、或 Agent 不走 magpie 网关的每一侧运行：

```bash
agent-notify install --agent claude --magpie
agent-notify install --agent codex --magpie
agent-notify install --agent omo --magpie
agent-notify test magpie
agent-notify doctor
```

`--magpie` 开启转发模式：hook 事件交给插件发送，这一侧不需要 `--channels`，事件过滤、文案、去重都由插件决定。它还会多订阅一个「回合开始」hook（Claude / Codex 的 `UserPromptSubmit`、OMO 的 `agent_start`）用于计算用时，提问内容不会被转发。重装时省略 `--magpie` 沿用原设置；`--magpie=false` 关闭并摘掉回合开始 hook。

WSL 里的 `install --magpie` 会把 `magpie.endpoint_file` 指向 Windows 用户目录下的 `.agent-notify/magpie-endpoint.json`（magpie 通常运行在 Windows 上）；定位不到时按提示在配置里手工填写。插件不可达时，本机 `events` 里订阅的事件降级为本机 Windows 通知，原因写入日志。

转发连的是 `127.0.0.1`：magpie 在 Windows、Agent 在 WSL 时，WSL 需要 mirrored 网络（`%USERPROFILE%\.wslconfig` 的 `[wsl2]` 下 `networkingMode=mirrored`）；默认的 NAT 网络下连不到插件，事件会一直走降级。通知没来时先运行 `agent-notify doctor`，再看 magpie 所在系统的 `~/.agent-notify/magpie-plugin.log`：没发的事件都记着原因，含义见[插件 README 的排错一节](../magpie-plugin/README.md#排错)。同一侧不要把 `--channels` 的独立模式和转发模式混着用，否则同一个回合会各发一条。

With the magpie plugin installed, `install --magpie` forwards hook events to it instead of sending locally; channels, filtering and wording are configured once in the plugin. If the plugin cannot be reached, subscribed events fall back to a local Windows toast. Forwarding targets `127.0.0.1`, so an agent in WSL reaching magpie on Windows needs WSL mirrored networking. The plugin's log (`~/.agent-notify/magpie-plugin.log` on the magpie side) records why an event was not sent.

## 通知模板 / Notification templates

`behavior.title_template` 与 `behavior.body_template`（配置文件或「通知超时与去重」向导）可自定义通知标题与正文；留空使用各事件的默认文案。占位符 / Placeholders:

| Placeholder | 展开为 / Expands to |
| --- | --- |
| `{agentName}` | Agent 显示名（如 Claude Code）/ Agent display name |
| `{event}` | 事件显示名（如 运行完成）/ Event display name |
| `{project}` | 项目名（工作目录末段）/ Last workspace segment |
| `{workspace}` | 完整工作目录 / Full workspace path |
| `{session}` | 会话 ID 前 8 位 / First 8 characters of the session ID |
| `{message}` | 事件默认正文 / Default event body |
| `{time}` | 发送时刻 HH:MM:SS / Send time |

未知占位符原样保留。标题模板最长 100 字符且不允许换行，正文模板最长 500 字符。模板对两个渠道同时生效；`send` / `test` 显式命令不经过模板。

```yaml
behavior:
  title_template: "{agentName} · {event}"
  body_template: "[{session}] {message}"
```

## 自定义目录 / Custom paths

| Setting | Meaning |
| --- | --- |
| `--config <file>` | Agent Notify 配置文件，优先于环境变量 / Overrides the config environment variable |
| `AGENT_NOTIFY_CONFIG` | Agent Notify 配置文件 / Config path |
| `CLAUDE_CONFIG_DIR` | Claude 用户配置目录 / User settings directory |
| `CODEX_HOME` | Codex 用户配置目录 / User settings directory |
| `OMO_CODING_AGENT_DIR` | OMO agent 目录 / Agent directory |
| `SENPI_CODING_AGENT_DIR`, `SENPI_HOME` | OMO 目录未设置时依次使用 / Fallback agent directories |
| `AGENT_NOTIFY_WECOM_WEBHOOK` | 向导或 install 时读取并保存 / Webhook saved during setup |

`install --settings <file>` overrides the integration destination; `--binary <file>` sets the permanent notifier executable. Hooks retain the absolute config and binary paths used at install time.

## 升级与卸载 / Updating and uninstalling

将新程序解压到原位置，重新运行 `install` 更新接入，随后按 Agent 的要求重新加载 hooks 或扩展。配置保存前会备份现有文件。升级自原仓库时，先备份配置，旧渠道不会在本版本发送通知；旧版 OpenCode 插件需在原工具中卸载，再安装 Senpi/OMO 扩展。

Hook 订阅是安装时的快照：升级二进制不会自动补新事件订阅。v0.2 为 Claude Code 新增 `StopFailure` / `SubagentStop`、为 Codex 新增 `Interrupt` / `SubagentStop` 订阅，升级后需对相应 Agent 重跑 `install`（增量安装，保留用户自己的 hooks），再在配置中勾选 `run_interrupted` / `subagent_completed`。OMO 扩展重跑 `install` 即整体重渲染。

Extract updates into the existing installation directory, rerun setup and reload the agent. Hook subscriptions are a snapshot taken at install time: upgrading the binary alone does not subscribe new events — rerun `install` per agent (incremental; user hooks are preserved) and opt in to the new events in your configuration. Back up existing settings when migrating from upstream. Legacy channels are inactive in this edition. Remove an older OpenCode plugin using the tool that installed it before installing the Senpi extension.

```bash
agent-notify uninstall --agent all
```

卸载只移除 Agent Notify 管理的接入并关闭通知，保留其他 hooks、本地通知配置、备份、日志及 Codex 通用 hooks 开关。Windows Toast 首次发送时创建的当前用户 `AgentNotify.Desktop`、`AgentNotify.Desktop.Codex`、`AgentNotify.Desktop.Claude`、`AgentNotify.Desktop.OMO` 注册项及 `%LOCALAPPDATA%\AgentNotify\icons` 缓存也保留。确认不再使用后，可自行删除程序目录及私有配置目录。

Uninstall preserves unrelated hooks, local configuration/backups/logs, the shared Codex hooks feature flag, the per-user Windows `AgentNotify.Desktop` registrations (including the `.Codex`, `.Claude` and `.OMO` identities), and cached icons created when sending a toast.

## 排错 / Troubleshooting

1. Run `agent-notify doctor`. It checks configuration, installed entries, executable paths and PowerShell availability without sending messages. 它检查接入文件，不代表 Agent 已实际加载。
2. Run `agent-notify test system --agent <id>` or `agent-notify test wechat-work --agent <id>` to verify the selected channel. These are explicit sends and bypass enable flags, event filters, freeze and deduplication.
3. If the channel works but agent events do not, reload the integration. For Codex, restart and review `/hooks`; for OMO, `/reload` or restart. Check the configured agent and subscribed events.
4. If Windows notifications do not appear, check Windows notification settings, Do Not Disturb and PowerShell availability. WSL must be able to run `powershell.exe`; a successful submission cannot prove a visible toast appeared.
5. If WeCom fails, check the bot webhook, network access, bot limits and group policy. 更新 Webhook 后需要重新运行安装或向导。
6. Read `agent-notify.log` beside the selected config. Review and redact private content before sharing. `agent-notify config show` hides webhook values; raw config and `.bak` files contain secrets.

## 校验下载 / Verify downloads

WSL: place the four archives and `SHA256SUMS` in one directory and run `sha256sum --check SHA256SUMS`; if you downloaded only one archive, compare its `sha256sum <archive>` output with the matching line.

Windows PowerShell: run `Get-FileHash .\agent-notify-<version>-windows-amd64.zip -Algorithm SHA256` and compare with the same filename in `SHA256SUMS`.
