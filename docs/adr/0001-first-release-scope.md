# ADR 0001：首期产品范围与通知边界

## 状态

已接受

## 决策

1. 首期支持 Claude Code、Codex、新版基于 Senpi 的 OMO。
2. 首期通知渠道为企业微信机器人和 Windows Toast；WSL 通过 powershell.exe 转发 Windows Toast。
3. OMO 使用 Senpi Extension 事件作为接入边界，用户级扩展遵循 OMO/Senpi 的 agent directory，项目级扩展写入 .senpi/extensions。
4. 通知是单向提醒，不提供点击后激活终端或返回工作区。
5. 首期使用解压即用的 Windows/Linux 独立包，不要求安装 Go 或 Node.js。
6. 配置、事件去重、Webhook 校验和 Agent 接入通过命令行向导完成。
