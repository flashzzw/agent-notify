<p align="center"><img src="docs/assets/logo.svg" width="88" alt="Agent Notify bell"></p>

# Agent Notify

**Know when your coding agent needs you.** Send approval requests, input prompts, completions and failures to Windows notifications or a WeCom (WeChat Work) group bot.

[简体中文](README.zh-CN.md) · [Downloads](https://github.com/flashzzw/agent-notify/releases/latest) · [Report a bug](https://github.com/flashzzw/agent-notify/issues)

[![Test](https://github.com/flashzzw/agent-notify/actions/workflows/test.yml/badge.svg)](https://github.com/flashzzw/agent-notify/actions/workflows/test.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

This edition supports **Claude Code, Codex, and Senpi-based Oh My OpenCode (OMO)** on **Windows and WSL**. Each agent can independently enable desktop notifications, WeCom, or both. The command-line wizard currently uses Chinese labels.

## Install

Download an archive from [Releases](https://github.com/flashzzw/agent-notify/releases/latest). Most Intel/AMD computers use `amd64`; ARM devices use `arm64`.

| Where the agent runs | Archive |
| --- | --- |
| Native Windows 10/11 | `agent-notify-<version>-windows-amd64.zip` or `windows-arm64.zip` |
| WSL | `agent-notify-<version>-linux-amd64.tar.gz` or `linux-arm64.tar.gz` |

Extract to a permanent directory. The archive contains `agent-notify.exe` (Windows) or `agent-notify` (WSL), documentation and the MIT license. No Go or separate Node.js installation is required to run the notifier. Your coding agent still needs its own runtime.

Windows PowerShell, from the extracted directory:

```powershell
.gent-notify.exe
```

WSL, from the extracted directory:

```bash
chmod +x agent-notify
./agent-notify
```

The wizard selects an agent, installation scope, channels and events. Choose **当前用户** (current user) for the usual setup. Repeat for each agent. Add the directory to `PATH` to use the commands below from anywhere; otherwise use `./agent-notify` or `.gent-notify.exe`.

WSL desktop notifications require Windows interoperability and access to `powershell.exe`. Plain Linux has no desktop channel in this edition. Release binaries are unsigned; `SHA256SUMS` is provided for download integrity checks.

## Quick start

For desktop notifications:

```bash
agent-notify install --agent claude --channels system
agent-notify test system --agent claude
agent-notify doctor
```

Replace `claude` with `codex` or `omo`. For WeCom, create a group bot and paste its webhook into the wizard's hidden input. Automation can supply `AGENT_NOTIFY_WECOM_WEBHOOK` before `install`:

```bash
agent-notify install --agent claude --channels system,wechat-work
agent-notify test wechat-work --agent claude
```

The environment variable is read during setup and saved to the local configuration; hooks subsequently read that saved value. Inject it through your secret manager or a hidden prompt. Typing a literal secret into an environment assignment can still save it in shell history.

## Integrations

| Agent (`--agent`) | Default user integration | Events |
| --- | --- | --- |
| Claude Code (`claude`) | `~/.claude/settings.json` | permission, input, completed, failed |
| Codex (`codex`) | `~/.codex/hooks.json` and the hooks feature in `config.toml` | permission, completed |
| OMO / Senpi (`omo`) | `~/.omo/agent/extensions/agent-notify.ts` when that agent directory exists; otherwise `~/.senpi/agent/extensions/agent-notify.ts` | permission, input, completed, failed |

This OMO integration targets the Senpi extension interface (`agent_settled` and `herdr:blocked`). It does not install the older OpenCode plugin. Restart Codex after installation and run `/hooks` to review and trust hook changes. In OMO, run `/reload` or restart to load the extension. See [configuration and troubleshooting](docs/usage.md) for custom directories, project scope and event names.

## Daily use

```bash
agent-notify config show                 # Redacts webhook values
agent-notify disable --agent codex
agent-notify enable --agent codex
agent-notify freeze --for 30m            # Pause WeCom; desktop notifications continue
agent-notify unfreeze
agent-notify uninstall --agent claude    # Preserve other hooks and notification config
agent-notify --help
```

Settings live in `~/.agent-notify/config.yaml` (`%USERPROFILE%\.agent-notify\config.yaml` on Windows). Use `--config <path>` or `AGENT_NOTIFY_CONFIG` to select a different file. The same directory holds state, logs and configuration backups.

Hooks contain the absolute executable path. After moving or updating the install directory, rerun `install` or the wizard. Hook errors are logged and return successfully to the agent; sends have timeouts and deduplication. Notifications are one-way reminders without terminal focus on click.

## Privacy

WeCom receives the notification's event, message and, where present, workspace information. Agent messages can contain code or private project details. Local configuration and backups contain the webhook secret; keep them private. See [SECURITY.md](https://github.com/flashzzw/agent-notify/blob/main/SECURITY.md) before sharing logs or configuration.

## Development

Use Go 1.25 or newer. Node.js 22 or newer runs the Senpi extension contract test; without Node that test is skipped. Python 3 and Make are needed for `make package`.

```bash
make test
make vet
make build
make package VERSION=v0.1.0
```

The binary goes to `bin/`; release archives and `SHA256SUMS` go to `dist/`. Native Windows users can run `go test ./...`, `go vet ./...`, `go build -o bin/agent-notify.exe ./cmd/agent-notify` and `python scripts/package.py --version v0.1.0` directly. See [CONTRIBUTING.md](https://github.com/flashzzw/agent-notify/blob/main/CONTRIBUTING.md).

## License and origin

[MIT](LICENSE). Derived from [hellolib/agent-notify](https://github.com/hellolib/agent-notify), with the original copyright and Git history preserved. This repository maintains its own Windows/WSL-focused edition and release series. Upstream npm packages and releases are separate from this edition.
