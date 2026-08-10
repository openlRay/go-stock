# 创建轻量上游提交同步技能

## 目标

创建一个 Codex 与 Claude 共用的项目级技能，指导 AI 将
`https://github.com/ArvinLovegood/go-stock.git` 的 `dev` 分支正常合并到当前
已分叉项目。保留上游提交哈希；出现冲突时由 AI 结合本项目定制分析并解决。

## 已确认事实

- 当前项目 fork 自 `ArvinLovegood/go-stock:dev`，之后已经明显分叉。
- 用户选择普通 merge，不使用 rebase、cherry-pick 或 squash。
- 用户不需要审批令牌、Git 事务状态机、恢复 CLI 或完整性验证脚本。
- 允许升级 Git，但技能采用 Git 2.20.1 与新版均支持的命令，不把升级作为前置条件。
- 技能需要同时在 Codex 和 Claude 中生效，文档使用中文。
- `.agents/skills/sync-upstream-commits` 是唯一真源；Claude 使用项目内相对符号链接。
- `.codex/config.toml` 有任务开始前的 staged 改动，本任务不得修改、隐藏、提交或丢弃它。

## 范围内需求

### R1：轻量部署

- 创建精简的 `SKILL.md`、`agents/openai.yaml` 和一份项目适配 reference。
- 在 `.claude/skills/sync-upstream-commits` 建立指向唯一真源的相对符号链接。
- 删除当前未完成的 Python CLI、测试脚手架和缓存文件，不保留无用资源。

### R2：普通 Git 合并

- 明确使用上游 URL 和 `refs/heads/dev`，不得根据 `origin` 或 `upstream` 名称推断角色。
- 合并前确认当前目标分支、工作区状态和进行中的 Git 操作；工作区不干净时停止，
  不自动 stash、提交、reset 或搬运用户改动。
- 从明确 URL fetch 上游，立即记录固定上游 SHA，并列出当前分支缺失的 commits。
- 无缺失提交时报告无需同步并结束。
- 有缺失提交时展示固定 SHA、commit 数量与列表，请求一次用户确认。
- 确认后创建安全集成分支，执行普通
  `git merge --no-ff --no-commit <固定上游 SHA>`。

### R3：AI 冲突与语义适配

- 有文本冲突时，AI 必须查看 base、local、upstream 三方，逐文件理解意图后解决；
  禁止全局 `ours`/`theirs` 策略。
- 每次 merge 完成后都必须完整审查本轮合并 diff 及其调用链，保证客户端项目新增的
  代码不会破坏 fork 的 Web 兼容模式；该检查不以是否出现文本冲突为条件。
- 强制检查 Web/桌面 build tags、Wails runtime 调用、RPC/SSE/事件边界、前端桥接、
  Docker/runtime、浏览器路径、生成文件和依赖；其他子系统按实际影响扩展。
- 无法确定业务意图时停止并请求用户选择，不猜测。
- 冲突解决属于 merge commit；只有确实需要额外兼容修改时，才在 merge 后创建
  一个职责清晰的适配提交。

### R4：验证与发布边界

- 运行与受影响范围相符的现有测试、构建和 `git diff --check`。
- 使用 `git merge-base --is-ancestor <上游 SHA> HEAD` 证明上游 tip 已纳入。
- 默认停留在验证通过的集成分支；不自动 push、更新远端、force-push 或删除分支。
- 冲突处理中断时允许人工运行 `git merge --abort`；技能不得自动执行破坏性恢复。

## 验收标准

- [ ] AC1：Codex 可通过 `$sync-upstream-commits`、Claude 可通过
  `/sync-upstream-commits` 发现同一技能目录。
- [ ] AC2：技能无 Python 同步引擎，仅包含执行普通 merge 所需的精简说明和项目参考。
- [ ] AC3：技能明确执行“检查 → fetch 固定 SHA → 展示缺失 commits → 一次确认 →
  集成分支普通 merge → AI 解冲突 → 验证”的流程。
- [ ] AC4：上游所有原始提交哈希保持可达，且不使用 rebase、cherry-pick、squash
  或全局 ours/theirs。
- [ ] AC5：AI 会审查文本冲突和关键项目语义冲突，未知业务选择会阻断并询问用户。
- [ ] AC5a：每次 merge 后均执行完整 Web 兼容审查，桌面与 Web 的 Go 测试/构建及
  前端构建通过；必要修复位于 merge commit 后的适配提交。
- [ ] AC6：技能不自动 stash、提交用户改动、修改远端、push 或更新目标分支。
- [ ] AC7：`quick_validate.py`、符号链接校验、Codex/Claude 发现测试通过，且
  `.codex/config.toml` 的既有 staged 内容保持原样。

## 范围外

- 审批令牌、canonical JSON、CAS ref 事务和状态恢复引擎。
- `plan`、`prepare`、`recover`、`verify` Python 子命令。
- 通用化为适用于任意仓库的 Git 同步产品。
- 自动发布、自动更新 `dev`、push、force-push 或创建 PR。

## 关键决策

- 实现形态：纯技能说明，不实现 Git 操作脚本。
- 历史策略：固定上游 SHA 的普通 merge，保留完整可达历史。
- 冲突策略：由 AI 逐文件分析处理，必要时执行额外语义适配。
- 审批策略：合并前一次普通用户确认，不使用令牌。
- 安全策略：使用集成分支，默认不发布。

## 风险与延后事项

- 上游可能在执行期间继续推进；本轮只合并 fetch 后记录的固定 SHA，新增提交留待下轮。
- 部分业务冲突无法仅凭代码判断；此类情况必须请求用户决策。
- 将来若确实需要无人值守同步，再另行设计自动化脚本与恢复机制。

## 阻塞问题

无。
