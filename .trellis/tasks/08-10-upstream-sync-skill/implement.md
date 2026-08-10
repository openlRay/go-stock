# 轻量上游同步技能实施计划

## 目标文件

- `.agents/skills/sync-upstream-commits/SKILL.md`
- `.agents/skills/sync-upstream-commits/agents/openai.yaml`
- `.agents/skills/sync-upstream-commits/references/project-adaptation.md`
- `.claude/skills/sync-upstream-commits`（相对符号链接）

## Task 1：清理过度实现

- [ ] 记录 `.codex/config.toml` 的 index blob、cached diff 和工作树哈希。
- [ ] 删除本任务当前未完成的 `scripts/`、`tests/`、`__pycache__` 和 `.pyc`。
- [ ] 保留官方 `init_skill.py` 创建的 canonical 目录、元数据目录和 Claude symlink。
- [ ] 确认没有产品代码、Git refs、remote 或目标分支变化。

## Task 2：编写精简中文技能

- [ ] 将 `SKILL.md` frontmatter 收敛为仅 `name`、`description`。
- [ ] 正文使用命令式中文，描述：前置检查、明确 URL fetch、固定 SHA、缺失 commit
  报告、一次确认、集成分支 merge、AI 冲突处理、语义适配、验证和停止条件。
- [ ] 明确禁止自动 stash/reset/push/remote 修改以及 rebase/cherry-pick/squash/
  全局 ours/theirs。
- [ ] 正文直接链接并说明何时读取 `references/project-adaptation.md`。

## Task 3：编写项目适配参考

- [ ] 以中文记录 7 个项目子系统的主要路径、本地契约、验证命令和阻断条件。
- [ ] 将 merge 后完整 Web 兼容审查设为每次必做，明确桌面/Web build tags、Wails
  runtime、RPC/SSE、桥接、Docker/runtime、浏览器路径和生成链路检查。
- [ ] 保持内容面向当前 go-stock fork，不复制通用 Git 教程。
- [ ] 生成文件或二进制冲突必须先定位源和可重复生成命令，否则阻断。

## Task 4：生成元数据并验证双平台部署

- [ ] 使用官方 `generate_openai_yaml.py` 生成且仅生成 `display_name`、
  `short_description`、`default_prompt`；默认提示显式包含 `$sync-upstream-commits`。
- [ ] 对 canonical path 和 Claude symlink path 分别运行 `quick_validate.py`。
- [ ] 使用 `lstat`/`readlink`/`realpath` 验证 Claude 相对链接指向唯一真源。
- [ ] 检查 `SKILL.md` 少于 500 行、无 TODO、无脚本引用和无多余文件。

## Task 5：前向验证

- [ ] 在隔离临时 Git 仓库中，让新 Codex 子代理调用 `$sync-upstream-commits`，只输出
  合并计划，不执行 mutation；验证其使用明确 URL、固定 SHA、一次确认和普通 merge。
- [ ] 验证前向输出会在 merge 后无条件执行 Web 兼容审查，而非仅处理文本冲突。
- [ ] 使用 Claude 非交互调用 `/sync-upstream-commits` 做相同的只读发现测试；若认证、
  客户端能力或超时阻塞，明确报告，不伪造结果。
- [ ] 不在真实仓库运行 fetch/merge；真实仓库只做文件和 Git 状态检查。

## Task 6：质量门与保护用户改动

- [ ] 检查 diff 只包含技能、Claude symlink 和 Trellis 任务工件。
- [ ] 重新计算 `.codex/config.toml` 三重基线并逐字比对。
- [ ] 运行精确 pathspec 的 `git diff --check`。
- [ ] 不 staging/commit，除非用户另行明确要求；若后续提交，必须使用精确路径，
  排除 `.codex/config.toml`。

## 回滚点

- 所有实现改动仅位于新技能目录和 Claude symlink。
- 双平台验证失败时保留 canonical skill 并报告 Claude 阻塞，不复制第二份技能。
- 本任务不运行真实同步操作，因此不产生需要恢复的 refs、分支或 merge 现场。

## 启动实现前检查

- [ ] 用户已确认“无脚本、一次确认、AI 处理冲突”的轻量方案。
- [ ] 简化后的 PRD、design、implement 已完成复审。
- [ ] 用户在简化方案最终摘要之后再次明确批准实施。
