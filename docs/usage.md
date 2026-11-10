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

| Event | 含义 | Agent |
| --- | --- | --- |
| `permission_required` | 等待授权 / Approval needed | Claude, Codex, OMO |
| `input_required` | 等待输入 / Input needed | Claude, OMO |
| `run_completed` | 任务完成 / Completed | Claude, Codex, OMO |
| `run_failed` | 运行失败 / Failed | Claude, OMO |

```bash
agent-notify install --agent claude --channels system --events permission_required,run_completed
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

Windows 通知来源分别为 `Codex Notify`、`Claude Notify`、`OMO Notify`，顶部来源名称旁显示对应小图标，正文不显示大 logo。OMO 图标由 `assets/omo-icon-light.svg` 转为 PNG 后内置。各自使用独立的通知身份，可在 Windows「设置 → 系统 → 通知」中分别控制。图标内置于程序，首次使用时写入 `%LOCALAPPDATA%\AgentNotify\icons`；WSL 也写入此 Windows 本地目录，无需保留旧版工具或单独安装图片。

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

Extract updates into the existing installation directory, rerun setup and reload the agent. Back up existing settings when migrating from upstream. Legacy channels are inactive in this edition. Remove an older OpenCode plugin using the tool that installed it before installing the Senpi extension.

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
