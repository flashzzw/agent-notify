# Agent Notify

Agent Notify 将编程 Agent 的生命周期事件转换成桌面和企业微信通知，并通过命令行向导完成安装与配置。

## Language

**Agent**:
用户正在使用的 AI 编程工具。首期支持 Claude Code、Codex 和 Oh My OpenCode。
_Avoid_: 把 OMO 与旧版 OMP（oh-my-pi）混称。

**Agent event**:
Agent 发出的等待授权、等待输入、运行完成或运行失败事件。

**Channel**:
发送通知的目标。首期包含企业微信机器人和 Windows 系统通知。

**OMO**:
Oh My OpenCode。它运行在 Senpi Extension 体系中，Agent Notify 通过 Senpi 生命周期事件接收 OMO 会话状态。

**独立包**:
包含已编译 Agent Notify 程序及运行所需资源的、解压即可运行的目标平台压缩包。

**WSL**:
Windows Subsystem for Linux。WSL 中运行的 Agent 使用 Linux 版 Agent Notify，通过 Windows PowerShell 转发 Windows 系统通知。
