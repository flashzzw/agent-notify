# Agent Notify

这是一个 Go 命令行应用，首期支持 Claude Code、Codex、新版 Senpi-based OMO、企业微信机器人和 Windows Toast，覆盖 Windows 原生与 WSL。

## 常用命令

```bash
make test
bun test magpie-plugin
make build
make package VERSION=v0.1.0
go test ./internal/notify -run TestName
go vet ./...
```

## 架构

- `internal/cli`：Cobra 命令和 Survey 交互向导。
- `internal/config`：`~/.agent-notify/config.yaml`，包含 Agent、事件、渠道和行为配置；保存前校验 Webhook 与超时。
- `internal/app`：安装、启用、卸载 Agent 接入的生命周期。
- `internal/claudehooks`、`internal/codexhooks`、`internal/senpihooks`：读取 hook / Senpi 扩展事件并转换为统一通知消息。
- `internal/agenthooks`：按 Agent 配置选择发送器、去重、超时和日志；通知模板（`behavior.title_template` / `body_template`）在 Dispatch 层渲染。
- `internal/notify`：企业微信 HTTP Sender 与 Windows/WSL PowerShell Toast Sender（按 Agent 区分的通知身份与小图标、attribution 项目名；正文只有文字）。
- `internal/magpieforward`：转发模式——找到 magpie 插件的端点文件并上报事件。
- `internal/state`：发送去重、冻结和日志文件。
- `internal/common`：配置文件原子写入、hook 事件大小限制和命令路径处理。

`magpie-plugin/` 是 magpie 网关插件（Bun，`bun test magpie-plugin`），通知的中枢：网关钩子判断回合完成与失败，本地服务接收 Go 程序转发的 hook 事件，两者在 `hub.mjs` 汇合后发送。开启转发（`install --magpie`）时，Go 端不再按本机 `events` 过滤，文案与渠道由插件决定；取舍见 `docs/adr/0003-magpie-plugin-as-notification-hub.md`。改上报协议时两端同步修改。

OMO 使用新版 Senpi Extension。用户级扩展位于 `~/.omo/agent/extensions` 或 `~/.senpi/agent/extensions`，项目级位于 `.senpi/extensions`。不要把新版 OMO 当作旧版 OpenCode 插件或 OMP。

通知是单向的；代码不包含通知点击后返回终端、窗口聚焦或终端 helper 逻辑。hook 处理必须无交互、快速失败并写日志，不能阻断 Agent。

Webhook 属于敏感配置，错误和诊断输出不得泄露完整 URL。修改公共配置结构时同步更新迁移、校验和测试。测试从公开接口验证行为，不测试私有实现细节。
