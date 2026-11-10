<p align="center"><img src="docs/assets/logo.svg" width="88" alt="Agent Notify 铃铛"></p>

# Agent Notify

**Agent 需要你时，及时收到提醒。** 将等待授权、等待输入、任务完成和任务失败事件发送到 Windows 系统通知或企业微信群机器人。

[English](README.md) · [下载](https://github.com/flashzzw/agent-notify/releases/latest) · [反馈问题](https://github.com/flashzzw/agent-notify/issues)

[![Test](https://github.com/flashzzw/agent-notify/actions/workflows/test.yml/badge.svg)](https://github.com/flashzzw/agent-notify/actions/workflows/test.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

本版本面向 **Windows 和 WSL**，接入 **Claude Code、Codex、基于 Senpi 的新版 Oh My OpenCode（OMO）**。每个 Agent 可以单独配置系统通知、企业微信，或同时启用两个渠道。交互向导目前使用中文。

## 下载安装

打开 [Releases](https://github.com/flashzzw/agent-notify/releases/latest)，按 Agent 所在环境下载。普通 Intel/AMD 电脑选择 `amd64`，ARM 设备选择 `arm64`。

| Agent 运行环境 | 压缩包 |
| --- | --- |
| Windows 10/11 原生 | `agent-notify-<版本>-windows-amd64.zip` 或 `windows-arm64.zip` |
| WSL | `agent-notify-<版本>-linux-amd64.tar.gz` 或 `linux-arm64.tar.gz` |

解压到固定目录。包内包含 `agent-notify.exe`（Windows）或 `agent-notify`（WSL）、使用文档和 MIT 许可证。通知工具本身不需要安装 Go 或额外的 Node.js；编程 Agent 仍需它自己的运行环境。

在解压目录中打开 Windows PowerShell：

```powershell
.gent-notify.exe
```

WSL 用户在解压目录中运行：

```bash
chmod +x agent-notify
./agent-notify
```

按向导选择 Agent、安装范围、渠道和事件。一般选择「当前用户」，多个 Agent 分别配置。将解压目录加入 `PATH` 后，可以直接使用下文的 `agent-notify` 命令；否则使用 `./agent-notify` 或 `.gent-notify.exe`。

WSL 系统通知需要启用 Windows 互操作，并能够调用 `powershell.exe`。本版本不提供普通 Linux 桌面通知。发行二进制暂未签名，下载完整性可用随附的 `SHA256SUMS` 校验。

## 快速开始

只接收桌面通知：

```bash
agent-notify install --agent claude --channels system
agent-notify test system --agent claude
agent-notify doctor
```

把 `claude` 替换为 `codex` 或 `omo` 即可配置对应 Agent。使用企业微信时，在群内创建机器人，然后在向导的隐藏输入框粘贴 Webhook。自动化安装也可以预先注入 `AGENT_NOTIFY_WECOM_WEBHOOK`：

```bash
agent-notify install --agent claude --channels system,wechat-work
agent-notify test wechat-work --agent claude
```

环境变量在安装时读取并保存到本地配置，后续 hook 从配置读取。建议通过密钥管理工具或隐藏输入提供变量；直接在终端输入含密钥的赋值命令，仍可能被 shell 历史记录。

## Agent 接入

| Agent（`--agent`） | 默认用户级接入位置 | 支持事件 |
| --- | --- | --- |
| Claude Code（`claude`） | `~/.claude/settings.json` | 授权、输入、完成、失败 |
| Codex（`codex`） | `~/.codex/hooks.json`，并在 `config.toml` 中启用 hooks | 授权、完成 |
| OMO / Senpi（`omo`） | 若 `~/.omo/agent` 存在，写入其 `extensions/agent-notify.ts`；否则使用 `~/.senpi/agent/extensions/agent-notify.ts` | 授权、输入、完成、失败 |

OMO 接入使用 Senpi 的 `agent_settled` 和 `herdr:blocked` 扩展接口，不安装旧版 OpenCode 插件。Codex 安装后请重启并运行 `/hooks` 审核、信任变更；OMO 运行 `/reload` 或重启以加载扩展。自定义目录、项目级安装和事件名称见[使用与排错](docs/usage.md)。

## 日常使用

```bash
agent-notify config show                 # Webhook 自动隐藏
agent-notify disable --agent codex
agent-notify enable --agent codex
agent-notify freeze --for 30m            # 暂停企业微信，系统通知继续
agent-notify unfreeze
agent-notify uninstall --agent claude    # 保留其他 hooks 和通知配置
agent-notify --help
```

配置位于 `~/.agent-notify/config.yaml`（Windows 为 `%USERPROFILE%\.agent-notify\config.yaml`），可用 `--config <路径>` 或 `AGENT_NOTIFY_CONFIG` 修改。同目录保存状态、日志和配置备份。

Agent hook 保存程序绝对路径。移动或更换安装目录后，重新运行 `install` 或向导。发送具有超时和去重保护；hook 错误写入日志后返回成功，避免通知失败中断 Agent。通知为单向提醒，不支持点击后返回或聚焦终端。

## 隐私

企业微信会收到通知事件、消息正文及可用的工作区信息，Agent 消息中可能包含代码或项目资料。本地配置及其备份包含 Webhook 密钥，请妥善保管。分享配置或日志前请阅读 [SECURITY.md](https://github.com/flashzzw/agent-notify/blob/main/SECURITY.md)。

## 开发

需要 Go 1.25 或更新版本。Senpi 扩展契约测试需要 Node.js 22 或更新版本，未安装 Node 时该测试会跳过。`make package` 还需要 Python 3 和 Make。

```bash
make test
make vet
make build
make package VERSION=v0.1.0
```

程序输出到 `bin/`，压缩包和 `SHA256SUMS` 输出到 `dist/`。原生 Windows 可直接使用 `go test ./...`、`go vet ./...`、`go build -o bin/agent-notify.exe ./cmd/agent-notify` 和 `python scripts/package.py --version v0.1.0`。更多说明见 [CONTRIBUTING.md](https://github.com/flashzzw/agent-notify/blob/main/CONTRIBUTING.md)。

## 许可证与来源

使用 [MIT](LICENSE) 许可证，基于 [hellolib/agent-notify](https://github.com/hellolib/agent-notify) 改造，保留原作者版权和 Git 历史。本仓库维护面向 Windows/WSL 的独立版本及发布序列；上游 npm 包和 Release 与本仓库版本分开。
