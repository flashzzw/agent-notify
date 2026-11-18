# ADR 0002：Toast 品牌化、通知模板与事件扩展

## 状态

已接受

## 背景

首期通知（ADR 0001）只有两段纯文本与 AUMID 小图标，事件覆盖也缺少「子代理完成」「用户中断」两类高价值信号。参考 Claude Desktop 的通知形态（品牌图标醒目、信息结构化），本次在不推翻 ADR 0001 第 4 条（通知单向、无点击激活）的前提下增强视觉与信息表达。

## 决策

1. **Toast 视觉**：在 ToastGeneric binding 中加入 `image placement="appLogoOverride" hint-crop="circle"`，展示 Agent 品牌大圆 logo；图标复用既有 go:embed 资产与 `%LOCALAPPDATA%\AgentNotify\icons` 的 sha256 内容寻址缓存。Go/WSL 侧不知道 Windows 侧真实路径，XML 中以占位串 `urn:agent-notify:logo` 标记，PowerShell 侧在 `LoadXml` 前用 `[Uri]::new($iconPath).AbsoluteUri` 替换——`[Uri]` 自动产出正确 percent-encoding 的 `file:///` URI，`&` 需再转义为 `&amp;` 后拼入 XML 属性。
2. **attribution 行**：binding 末尾追加 `text placement="attribution"` 显示项目名（工作目录末段，`projectBase`），替代原先正文里的工作目录行；正文不再手拼时间行（Windows 通知自带时间显示）。不加 `displayTimestamp`：hook 近实时转发，到达时间即事件时间，保持 XML 最小。
3. **通知模板**：`behavior.title_template` / `body_template` 在 Dispatch 层统一渲染（`notify.ApplyTemplates`），三个 hooks 解析器只产默认文案；占位符集合 `{agentName} {event} {project} {workspace} {session} {message} {time}`；`strings.NewReplacer` 单趟替换防止正文字面占位符链式展开；未知占位符原样保留。企微渠道共享模板后的 Message，排版逻辑不变。`send`/`test` 显式命令不走模板。
4. **事件扩展（默认不启用）**：Claude Code 新订阅 `StopFailure`（→ run_failed）与 `SubagentStop`（→ subagent_completed）；Codex 新订阅 `Interrupt`（→ run_interrupted）与 `SubagentStop`；OMO 扩展 worker 会话 settled → subagent_completed、主会话 aborted → run_interrupted。`config.Events` 白名单与 `defaultEvents` 默认集分离：升级与新装用户默认保持首期事件集合，新事件须显式勾选。
5. **升级语义**：hook 订阅是安装时快照，升级二进制不自动补订阅；需重跑 `install`（增量，保留用户 hooks）。OMO 扩展重跑 install 即整体重渲染。
6. **点击聚焦终端仍不做**（ADR 0001 第 4 条继续有效）：本次不加 activation/launch/按钮。声音维持系统默认音。

## 已知取舍

- attribution 取项目末段而非两段路径：一行放不下两段，完整路径在企微渠道已有。
- OMO worker 会话出错也报 `subagent_completed`（不区分成败）；Codex `SubagentStop` 不把 `agent_type` 拼进正文——留作后续增强。
- 用户正文恰含 `urn:agent-notify:logo` 时会被替换为 file URI 文本：纯展示噪音，无注入面（替换值是固定 URI，不进入脚本）。
