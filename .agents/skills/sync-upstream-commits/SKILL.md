---
name: sync-upstream-commits
description: 将 ArvinLovegood/go-stock 的 dev 分支普通合并到当前分叉项目，并由 AI 分析冲突和本地兼容性。用于同步上游提交、检查 fork 落后情况、解决上游 merge 冲突或适配上游变更；保留原始提交哈希，不执行 rebase、cherry-pick 或 squash。
---

# 同步上游提交

把 Git 当作合并工具，把 AI 判断集中在冲突和项目适配。默认上游为
`https://github.com/ArvinLovegood/go-stock.git` 的 `refs/heads/dev`，默认目标为
当前项目的 `dev`。

## 硬性边界

- 不根据 `origin`、`upstream` 等远端名称推断上游角色，不新增或改写命名远端。
- 不自动 stash、提交、暂存、reset、clean 或搬运用户已有改动。
- 不使用 rebase、cherry-pick、squash、`--allow-unrelated-histories`、全局
  `ours`/`theirs` 或 `-s ours`。
- 不自动更新目标分支、push、创建 PR 或删除安全分支。只有用户在验证结果之后再次
  明确要求交付，才执行“第三阶段：显式交付”；用户在最初同步请求中提前写明 push
  也不算验证后的交付授权。任何情况下都不 force-push。
- 遇到无法判断的业务选择、不可重生成的产物或新增测试失败时停止并询问用户。

## 两道授权闸门

必须按时间顺序取得两条独立的新用户消息，禁止用早期消息预授权后续阶段：

1. 展示缺失提交和重叠路径后，等待第一条新消息批准创建集成分支并 merge。
2. 完成适配、验证并报告结果后，停在集成分支，等待第二条新消息批准更新 `dev`
   和/或 push。

即使最初请求写着“同步、验证后直接推送”，也必须经过上述两次暂停。任何测试失败、
远端变化或范围变化都会使未执行阶段的旧授权失效，需要重新报告并等待新消息。

## 第一阶段：检查并展示缺失提交

0. 预检 Git 运行时：

   ```bash
   command -v git
   git --version
   git status --short --branch
   ```

   若任一命令出现 `bad fsmonitor version`、无法读取 index extension 或同类兼容性
   错误，立即停止，不得信任 staged/unstaged 判断。查找本机已有的较新 Git 并验证，
   或取得用户批准后升级；选定后整轮使用同一个绝对路径，不混用不同 Git 二进制。
   禁止通过临时/永久关闭 fsmonitor、改写仓库配置或环境变量来绕过兼容错误。

1. 确认当前仓库、当前分支和目标 `refs/heads/dev`。记录：

   ```bash
   git rev-parse --show-toplevel
   git symbolic-ref --quiet --short HEAD
   git rev-parse --verify refs/heads/dev
   git status --short --branch
   ```

2. 若存在 staged、unstaged、untracked 文件，或已有 merge、rebase、cherry-pick、
   revert、bisect 操作，立即停止。不要替用户清理工作区。

3. 从明确 URL 获取上游，不修改 Git remote 配置：

   ```bash
   git fetch --no-tags https://github.com/ArvinLovegood/go-stock.git refs/heads/dev
   git rev-parse --verify 'FETCH_HEAD^{commit}'
   ```

   立即把输出保存为本轮固定上游 SHA，同时保存目标 `refs/heads/dev` 的完整 SHA。
   后续只合并这个固定 SHA；上游再次推进留给下一轮。

4. 验证双方有共同祖先。无共同祖先时停止，禁止添加
   `--allow-unrelated-histories`。

5. 列出目标尚未包含的上游提交，并检查双方从共同祖先开始修改的路径：

   ```bash
   git rev-list --reverse <目标SHA>..<上游SHA>
   git log --oneline --decorate <目标SHA>..<上游SHA>
   git diff --name-status <共同祖先>..<目标SHA>
   git diff --name-status <共同祖先>..<上游SHA>
   ```

6. 若没有缺失提交，报告“无需同步”并结束。否则向用户展示目标 SHA、固定上游
   SHA、缺失数量、完整 commit 列表和明显的重叠路径，然后停止并请求一次合并确认。
   未收到明确确认前，不创建分支、不 merge。

## 第二阶段：普通 merge

只在用户确认后继续。

1. 重新确认工作区仍干净、目标分支仍指向第一阶段记录的目标 SHA；任何变化都
   返回第一阶段。

2. 从固定目标 SHA 创建唯一的 `codex/sync-upstream-dev-*` 集成分支，不直接在
   `dev` 上工作。使用已通过预检的 Git：

   ```bash
   git checkout -b <唯一集成分支名> <固定目标SHA>
   ```

3. 在固定目标 SHA 上记录合并前质量基线。至少运行后续强制验证中的同一组命令；
   若全量测试依赖外网、用户配置或本地数据库，保留准确的失败、超时和环境信息，
   不把既有失败当作本轮回归，也不擅自修改无关测试。

4. 执行普通合并：

   ```bash
   git merge --no-ff --no-commit <固定上游SHA>
   ```

5. 若没有文本冲突，仍检查双方重叠路径和上游影响的关键子系统。处理冲突时按需
   读取 [项目适配参考](references/project-adaptation.md)；merge commit 创建后还要
   无条件执行其中的“强制 Web 兼容审查”。

## AI 处理冲突

对每个未合并文件逐项处理：

1. 用 `git diff --name-only --diff-filter=U` 获取冲突清单。
2. 查看 stage 1（共同祖先）、stage 2（当前 fork）、stage 3（上游）以及相关调用方、
   测试和提交说明。add/delete 等冲突不存在某个 stage 时按真实状态分析。
3. 先说明双方意图和必须保留的本地契约，再编辑最终内容；不要机械删除冲突标记。
4. 对需要项目判断的路径读取
   [项目适配参考](references/project-adaptation.md)，同时检查无文本冲突的关联文件。
5. 二进制、压缩静态产物和生成文件必须从源文件用项目内可重复命令生成；找不到
   来源或生成命令时阻断。
6. 无法确定业务意图时列出可选方案和影响，停止等待用户选择。
7. 逐文件暂存已确认的解决结果。冲突全部清空后审查 staged diff，运行
   `git diff --cached --check`，再创建正常 merge commit。

冲突解决属于 merge commit。只有 merge 后仍需额外兼容修改时，才创建一个职责
明确的后续适配 commit；无影响时不要制造空提交。

## 强制 Web 兼容审查

每次创建 merge commit 后都读取
[项目适配参考](references/project-adaptation.md)，完整审查本轮合并 diff 及其调用链。
不要因为没有文本冲突或上游主要是客户端改动而跳过。

必须确认：

- 桌面入口与 `web` build-tag 入口都能编译，Web 路径不会调用 Wails 窗口、托盘、
  对话框、退出或自更新能力；
- 上游新增/修改的 `App` 方法在 Web RPC 允许列表、参数解码、错误返回和前端桥接中
  保持可用；事件同时兼容 Wails 与 SSE；
- 对上游新增/修改的选文件、上传、保存和下载能力先判断浏览器是否有原生等价方案；
  能等价实现时必须补 Web Bridge、受限同源接口和回归测试，不得仅隐藏入口；
- Docker/Web 启动方式、端口、运行时环境、浏览器路径和数据持久化契约未回归；
- 生成绑定和静态产物由合并后的源重新生成，前端 Web/Wails 两条调用路径一致。

若发现不兼容，在 merge commit 后完成修复并创建独立适配 commit，再运行验证。
无法确定如何兼容时停止并请求用户决策。

## 验证与交付

1. 每次同步强制运行以下 Web/桌面验证：

   ```bash
   git diff --check
   go test ./...
   go test -tags web ./...
   go build .
   GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -tags web .
   npm --prefix frontend run build
   ```

   再根据 [项目适配参考](references/project-adaptation.md) 和实际受影响路径补充
   Docker smoke、AI Web 前端构建或其他专项测试。把结果与合并前基线对比：本轮新增
   失败必须修复或阻断；既有失败必须如实报告，除非用户扩大范围，否则不要顺手修复。
2. 验证固定上游 tip 已纳入：

   ```bash
   git merge-base --is-ancestor <固定上游SHA> HEAD
   git rev-list <固定上游SHA> --not HEAD
   ```

   第一条必须成功，第二条必须为空。

3. 报告目标 SHA、上游 SHA、merge commit、可选适配 commit、冲突决策和测试结果。
   停留在集成分支，不自动更新 `dev` 或 push。

## 第三阶段：显式交付

只在用户看过验证结果后发出一条新的明确指令，要求“合并到 dev”“推送远端”或
等价操作时执行。若最初请求已经同时要求同步和 push，仍先停在集成分支报告验证结果，
等待报告之后的再次确认。交付授权不改变上游来源，也不授权 force-push。

1. 独立确认目标分支和交付远端 URL；`origin` 只是常见的 fork 交付远端，不能把它
   当作第一阶段的上游来源。获取远端 `dev`，固定交付前 SHA：

   ```bash
   git remote get-url --all <交付远端>
   git fetch <交付远端> dev
   git rev-parse refs/heads/dev
   git rev-parse refs/remotes/<交付远端>/dev
   ```

2. 要求远端 `dev` 与本地目标一致，或是已验证集成分支的祖先。若远端出现新提交，
   停止并重新评估普通 merge；不得用 rebase、reset 或强推绕过。
3. 确认工作区干净，再将本地 `dev` 快进到集成分支：

   ```bash
   git checkout dev
   git merge --ff-only <集成分支>
   ```

4. 推送普通 `dev`，然后从远端重新读取 SHA 并要求与本地 `dev` 完全一致：

   ```bash
   git push <交付远端> dev
   git ls-remote --heads <交付远端> refs/heads/dev
   git rev-parse refs/heads/dev
   ```

5. 报告最终 SHA、远端 URL 和工作区状态。若 Web 方法或生成绑定发生变化，提醒用户
   Git 交付不会自动重启已运行的 Go Web 服务或重建 Docker 镜像。

## 中断处理

若 merge 进行中且用户明确要求取消，先说明 `git merge --abort` 会丢弃本轮尚未
提交的冲突解决，再等待确认后执行。abort 失败或现场与本轮不一致时停止取证，
不要使用 reset/clean 兜底。
