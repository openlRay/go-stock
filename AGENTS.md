<!-- TRELLIS:START -->
# Trellis Instructions

These instructions are for AI assistants working in this project.

This project is managed by Trellis. The working knowledge you need lives under `.trellis/`:

- `.trellis/workflow.md` — development phases, when to create tasks, skill routing
- `.trellis/spec/` — package- and layer-scoped coding guidelines (read before writing code in a given layer)
- `.trellis/workspace/` — per-developer journals and session traces
- `.trellis/tasks/` — active and archived tasks (PRDs, research, jsonl context)

If a Trellis command is available on your platform (e.g. `/trellis:finish-work`, `/trellis:continue`), prefer it over manual steps. Not every platform exposes every command.

If you're using Codex or another agent-capable tool, additional project-scoped helpers may live in:
- `.agents/skills/` — reusable Trellis skills
- `.codex/agents/` — optional custom subagents

Managed by Trellis. Edits outside this block are preserved; edits inside may be overwritten by a future `trellis update`.

<!-- TRELLIS:END -->

## Codex 浏览器验收

- 在 Codex 中确需进行 Web 页面交互、视觉或回归验收时，连接并复用电脑上已经打开的本地外部浏览器、现有标签页和登录态。
- 不使用 Codex in-app Browser，不使用独立的 `playwright-cli`，也不创建或依赖 `.playwright-cli/` 目录。
- 如果本地外部浏览器未连接或无法访问目标页面，明确报告浏览器验收被阻塞；不要自动回退到 in-app Browser 或 `playwright-cli`。

## 验证策略

- 验证范围必须与修改风险相匹配，不要求每次改动都运行自动化测试。
- 纯文案、注释、spec、局部样式或不改变行为的小范围修改，默认执行差异检查和必要的人工核对即可；除非用户明确要求，不运行完整自动化测试或全量构建。
- 业务逻辑、跨层契约、数据转换、bug 回归、公共组件行为或影响多个入口的修改，运行最小相关的定点测试；只有发布前检查、依赖面广或用户明确要求时才运行全量测试。
- 浏览器自动化只用于存在真实交互风险且静态检查不足以证明正确的场景。跳过自动化测试时，在结果中如实说明验证范围。
