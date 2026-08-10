# 轻量上游提交同步技能设计

## 1. 设计原则

技能只规定可靠的人工/AI Git 流程，不把 Git 封装成新的同步引擎。Git 自身负责
fetch、merge、冲突状态和历史完整性；AI 负责读取差异、理解本项目定制并做冲突选择。

## 2. 文件结构

```text
.agents/skills/sync-upstream-commits/
├── SKILL.md
├── agents/
│   └── openai.yaml
└── references/
    └── project-adaptation.md

.claude/skills/sync-upstream-commits -> ../../.agents/skills/sync-upstream-commits
```

不包含 `scripts/`、`tests/`、恢复文档或额外说明文件。

## 3. 执行流程

### 3.1 合并前检查

1. 确认目标仓库、当前分支和目标分支。
2. 检查 staged、unstaged、untracked 文件以及 merge/rebase/cherry-pick 状态。
3. 工作区不干净或已有 Git 操作时停止，不自动处理用户改动。
4. 记录当前目标 SHA，明确上游 URL 和 `refs/heads/dev`。

### 3.2 获取与确认

1. 直接从明确 URL fetch 上游 `dev`，不新增或改写命名远端。
2. 从 `FETCH_HEAD` 立即解析并保存完整上游 SHA。
3. 用 `git rev-list`/`git log` 列出目标尚未包含的上游 commits。
4. 若列表为空，结束。
5. 展示目标 SHA、上游 SHA、缺失数量与 commit 列表，停止并请求一次确认。

### 3.3 普通 merge

确认后从目标 SHA 创建唯一的 `codex/sync-upstream-dev-*` 集成分支，执行：

```bash
git merge --no-ff --no-commit <固定上游 SHA>
```

禁止 rebase、cherry-pick、squash、`--allow-unrelated-histories`、`-X ours`、
`-X theirs` 和 `-s ours`。

### 3.4 AI 解决冲突

对每个冲突文件：

1. 查看冲突列表和三方内容；
2. 读取相关调用方、测试和项目 reference；
3. 判断上游改动意图与本地定制契约；
4. 编写兼容后的最终内容并逐个暂存；
5. 无法确定业务选择时停止询问用户；
6. 冲突全部解决并审查 staged diff 后创建 merge commit。

二进制文件、压缩产物和生成文件发生冲突时，先找到源文件和项目内生成命令；
不能可靠重生成时阻断，不直接手改产物。

### 3.5 语义适配与验证

merge commit 创建后，无论是否出现文本冲突，都按 `project-adaptation.md` 执行一次
完整 Web 兼容审查：审查本轮合并 diff、受影响调用链、build tags、Wails runtime
边界、RPC/SSE、前端桥接、Docker/runtime 和生成链路。必要兼容修改放在 merge
commit 后的独立适配提交；无影响时记录结论但不制造空提交。

至少运行：

```bash
git diff --check
git merge-base --is-ancestor <固定上游 SHA> HEAD
```

桌面与 Web 的 Go 测试/构建及主前端构建是每次同步的强制验证；其他验证按影响补充。
验证完成后停留在集成分支，
向用户报告 merge commit、可选适配 commit、测试结果和待人工发布步骤。

## 4. 项目适配参考

`project-adaptation.md` 保留下列本项目特有检查面，其中 Web 兼容审查每次必做：

- Web/桌面双模式与 build tags；
- Docker、compose 和运行时环境；
- Agent/沙箱工具；
- Web/Wails 前端桥接与生成绑定；
- AI Web 前端与静态产物；
- Go/Node 依赖和生成链路；
- Trellis、Codex、Claude 项目配置。

每个检查面记录路径、需要保护的契约、验证命令和何时阻断。

## 5. 恢复与发布

- merge 进行中且用户希望取消时，说明并等待用户确认后使用 `git merge --abort`。
- 不自动 reset、clean、stash、删除分支或 worktree。
- 不自动更新目标分支或 push；发布由用户另行决定。

## 6. 取舍

本设计放弃机器可证明的两阶段令牌和故障恢复状态机，换取更短、更容易理解的技能。
保留固定 SHA、集成分支、一次确认和最终祖先验证，足以覆盖用户要求的正常人工/AI
合并场景。
