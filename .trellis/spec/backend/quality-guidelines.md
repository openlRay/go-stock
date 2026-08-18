# 后端质量与测试规范

## Go 测试文件位置

Go 从被测 package 目录内发现 `_test.go`。`foo_test.go` 与 `foo.go` 同目录是工具链标准约定，测试文件不会进入生产二进制。

| package 声明 | 使用场景 |
| --- | --- |
| `package xxx` | 白盒验证未导出 normalize/helper、迁移内部状态或 package 私有不变量 |
| `package xxx_test` | 只通过导出 API 验证公共行为，减少对内部实现耦合 |

不要为了物理隔离建立通用顶层 `tests/`，也不要仅为改成外部测试 package 而导出内部函数。真正跨 package 的集成测试可以拥有独立 package，但必须只依赖公开边界。

## 测试类型

- 纯 normalize、validator、resolver 使用 table-driven unit test，覆盖正常值、边界、空值和非法值。
- 外部 HTTP 使用 `httptest.Server` 或可替换 transport；常规测试不依赖实时行情网站、模型服务或消息机器人。
- SQLite 测试使用独立内存数据库并恢复全局 `db.Dao`。
- build tag 行为由对应 tag 的测试文件验证；Web RPC、Desktop 平台能力和 runtime path 不能只靠当前操作系统测试。
- 有真实外部副作用的 integration test 必须显式标识，不进入日常测试命令；只有用户明确允许发送消息、调用付费模型或访问真实服务时才能运行。

## 并发与生命周期断言

涉及 goroutine、stream、scheduler、SSE、latest-wins 或 cancel 时，测试不能只断言返回值，还要断言：

- 旧 request 不覆盖新状态；
- cancel/timeout 后不继续持久化或发送 completed；
- channel/hub 关闭不 panic、不阻塞其他消费者；
- teardown 释放 context、HTTP body、timer 和后台资源；
- 多步写入失败时没有部分状态。

对共享 map、全局 emitter 和 request state 的改动应考虑定点 `go test -race`；只有相关并发路径确实被覆盖时，race 结果才有意义。

## 代码注释

代码注释统一使用中文，技术标识符保留英文。必须解释类型和函数名无法表达的“为什么”：

- build tag、Desktop/Web 能力差异；
- 日历、Cron、时区和单位语义；
- 兼容/回退、迁移和历史数据处理；
- secret、安全、SSRF 或输入信任边界；
- request-local copy、latest-wins、cancel、事务和资源生命周期；
- 第三方库的非显然约束。

不要注释明显赋值或逐行翻译代码。导出 Go 标识符遵循 GoDoc 习惯，中文注释仍需明确点名符号及其契约。

## 按风险选择验证

| 修改类型 | 默认最小验证 |
| --- | --- |
| 纯文档、注释、格式 | diff review、`git diff --check`；不运行代码自动化测试 |
| 单个纯函数或校验分支 | 对应 package 的定点 unit test |
| 数据库查询/事务/迁移 | 对应 db/data tests，覆盖 rollback 和 legacy schema |
| 外部 HTTP/AI request builder | 本地 transport/`httptest` 契约测试，不访问 live service |
| Wails/Web RPC、build tag、runtime path | 对应生成 binding 检查、tag test/build |
| 并发、stream、scheduler、事件 | 定点生命周期测试；必要时 `-race` |
| 影响多个 package 或发布门禁 | 再考虑 `go test ./...` 与目标平台 build |

不要把单 package 测试描述为全仓测试，也不要把 build 成功描述成交互回归。

## 场景：限长通知的最终载荷预算

### 1. Scope / Trigger

当任务、告警或机器人通知存在字符数上限，并且正文还会经过 Markdown 转义、HTML 实体编码或统一标题包装时适用。限制必须针对最终可发送载荷，而不是只检查原始业务文本。

### 2. Signatures

- Cron 执行结果：`CronTaskExecutionResult{Summary, Markdown, PlainText}`。
- 通知包装入口：`App.pushCronTaskResult(task, result)`。
- 当前 Cron 详情上限：`cronNotificationMaxRunes = 4000`；具体 formatter 必须为统一标题、状态、时间和摘要预留空间。

### 3. Contracts

- 先 normalize、截断业务字段，再执行 Markdown/HTML 转义；预算按转义后的 rune 数计算。
- 列表按完整行逐条追加，每次同时检查 Markdown 与 PlainText，不能依赖最终统一截断切断表格或半行文本。
- 面向多个机器人渠道的 Markdown 只使用共同支持的标题、强调和单层列表；股票等业务明细不得使用标准 Markdown table，也不得把外层 `-` 与内层 `1.` 组合，因为飞书会忽略表格块或把序号解析为嵌套列表。
- formatter 返回的详情必须为通知包装层预留固定安全空间；超出时正文明确报告实际展示数量。
- `Summary`、`Markdown`、`PlainText` 都不得声称发送了未实际容纳的条目。

### 4. Validation & Error Matrix

| 条件 | 处理 |
| --- | --- |
| 原始文本可容纳，但转义后超限 | 缩短字段或减少完整列表行 |
| “全部”结果无法全部容纳 | 成功发送可容纳部分，并报告总数与实际展示数 |
| 固定数量仍因字段过长无法容纳全部 | 按完整行降级，并报告实际展示数 |
| Markdown 与 PlainText 预算不同 | 取两者都能安全容纳的条目数 |
| Markdown table 在飞书卡片中被忽略，或 `- 1.` 被解析为嵌套列表 | 改用 `- **01｜代码 名称**` 形式的单层列表 |

### 5. Good / Base / Bad Cases

- Good：使用全 `<`、`>`、`|` 等最坏转义输入测试最终载荷，业务明细用普通列表逐行试算。
- Base：普通短文本按配置数量完整展示。
- Bad：按原始字符串长度截断后再转义，使用渠道不兼容的 Markdown table，或先生成超长内容再由通用 `truncate` 从中间截断。

### 6. Tests Required

- 普通固定数量：断言总命中数、展示数和最后一条序号。
- 渠道兼容格式：断言 Markdown 使用列表且首尾股票代码、名称均存在，不包含 table header 或 `- 1.` 嵌套列表前缀。
- “全部”短列表：断言全部条目存在。
- “全部”超长列表：断言最终 Markdown/PlainText 不超过预算，且摘要中的实际展示数等于完整行数。
- 最坏转义输入：策略名、条件或条目字段只使用会膨胀的特殊字符，断言转义后仍在预算内。
- 通知边界集成测试：让结果经过真实统一包装入口，断言一次执行只产生一次事件和一次推送。

### 7. Wrong vs Correct

```go
// Wrong：raw 合法不代表 escapeMarkdown(raw) 仍在预算内。
raw = truncateRunes(raw, limit)
payload := escapeMarkdown(raw)

// Correct：为包装和最坏转义预留空间，并检查最终候选载荷。
candidate := renderEscapedContent(fields, rows[:next])
if runeCount(candidate.Markdown) > detailBudget || runeCount(candidate.PlainText) > detailBudget {
	break
}

// Wrong：飞书卡片会忽略标准 Markdown table。
line := fmt.Sprintf("| %d | %s | %s |", index, code, name)

// Wrong：飞书会把外层圆点和内层数字解析成两级列表。
line := fmt.Sprintf("- %d. **%s** %s", index, code, name)

// Correct：使用飞书和钉钉共同支持的单层列表，序号后不接点号。
line := fmt.Sprintf("- **%02d｜%s %s**", index, code, name)
```

## Trellis task 归档提交契约

### 1. 适用范围 / 触发

任何将 `.trellis/tasks/<task-name>/` 移入 `.trellis/tasks/archive/<year-month>/` 的操作都适用。目标是保留可审查的 task 历史，同时避免没有独立语义的归档专用 commit。

### 2. 命令签名

```bash
python3 ./.trellis/scripts/task.py archive <task-name> --no-commit
python3 ./.trellis/scripts/add_session.py --title "<title>" --commit "<hashes>" --summary "<summary>" --no-commit
git add -- <精确的归档、journal、spec/workflow 收尾路径>
git commit -m "chore(trellis): <本轮收尾说明>"
```

项目工作流不得省略两个脚本的 `--no-commit`，也不得依赖脚本自动创建归档或 journal commit。

### 3. 契约

- `task.py archive ... --no-commit` 负责把 task 标记为 `completed`、写入完成时间、移动目录并清理指向它的 session runtime pointer，但不触碰 Git index 或创建 commit。
- 归档移动必须与同轮 journal，或相关的 spec、workflow、task 元数据等收尾改动合并为一个 wrap-up commit；禁止 archive-only commit。
- 批量归档时，可以把所有已确认 task 的移动与同一轮规则/收尾改动放入一个 commit。
- 产品代码通常先按 Phase 3.4 独立提交；无关 dirty/staged 路径不得进入 wrap-up commit。
- 只允许 `git add -- <精确路径>`，禁止 `git add .`、`git add -A` 或强制加入整个 `.trellis/`。

### 4. 校验与错误矩阵

| 条件 | 处理 |
| --- | --- |
| 归档前存在无关 dirty/staged 文件 | 明确列出并排除；无法安全区分时停止提交 |
| `task.py archive` 任一命令失败 | 不继续提交，检查 task 是否发生部分移动 |
| staged diff 出现未确认路径 | 停止，不创建 commit |
| 只有归档移动、没有 journal 或相关收尾改动 | 补齐本轮 journal/收尾上下文后再提交，不创建 archive-only commit |
| 归档后 task 仍在 active tree 或状态不是 `completed` | 视为失败，修复前不提交 |

### 5. Good / Base / Bad Cases

- Good：批量归档多个已完成 task，并把归档移动、finish-work 规则与 spec 更新放入一个 `chore(trellis)` commit。
- Base：单个 task 归档与本轮 journal 使用两个 `--no-commit` 命令生成，再一起提交。
- Bad：运行不带 `--no-commit` 的 `task.py archive`，或创建只包含 `.trellis/tasks/archive/**` 的 commit。

### 6. 必需验证

- 归档命令执行前后记录 `git rev-parse HEAD`，确认合并提交前 HEAD 未变化。
- 确认 active task 列表为空或只剩明确不归档的 task。
- 确认每个目标目录位于对应月份的 archive 下，且 `task.json.status == "completed"`。
- 提交前运行 `git diff --cached --name-status` 与 `git diff --cached --check`，断言路径范围准确且无空白错误。
- 此类纯 Trellis 文档/元数据改动默认不运行产品代码测试。

### 7. Wrong vs Correct

#### Wrong

```bash
python3 ./.trellis/scripts/task.py archive 08-16-example
# 脚本立即产生 chore(task): archive ... 专用 commit
```

#### Correct

```bash
python3 ./.trellis/scripts/task.py archive 08-16-example --no-commit
python3 ./.trellis/scripts/add_session.py --title "Example" --commit "abc1234" --summary "完成 Example" --no-commit
git add -- .trellis/tasks/08-16-example .trellis/tasks/archive/2026-08/08-16-example ".trellis/workspace/<developer>/journal-N.md" ".trellis/workspace/<developer>/index.md"
git commit -m "chore(trellis): archive example and record session"
```

## 禁止模式

- 只断言自己刚构造的 mock 值，测试不经过生产逻辑。
- 正常单元测试访问 live API、真实数据库文件或发送外部消息。
- 通过 `time.Sleep` 猜测 goroutine 完成，能使用 channel/condition 时仍依赖时间。
- 为通过测试而暴露内部 API 或放宽生产校验。
- 改动 build tag 只在当前 OS 执行无 tag 的 `go test`。
- 共享工作树中运行全仓格式化、清理或覆盖无关 WIP。

## 审查清单

- 新规则的 owner package 和依赖方向是否清晰？
- 输入是否在进入网络、数据库或反射边界前验证？
- error 是否保留 cause，secret 是否被隔离？
- transaction/cancel/latest-wins 是否在最终副作用前再次检查？
- 测试范围是否覆盖当前 diff 的真实风险，并如实报告？
