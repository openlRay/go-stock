# 定时任务执行时间设置设计

## Boundaries

- `frontend/src/components/cron-schedule-editor.vue`：拥有交互状态、常用模式与 Cron 的双向转换、预览和专家说明。
- `frontend/src/components/cron-task-manager.vue`：继续拥有任务创建/编辑表单，只接收编辑器确认后的 Cron 字符串。
- `backend/agent/cron_schedule_ai.go`：调用现有 AI 模型，将自然语言解析为结构化调度意图，并执行确定性校验与 Cron 生成。
- `app_cron_schedule.go`：提供 Wails/Web RPC，不修改现有 `app.go` 中其他进行中的功能。
- `backend/data/ai_config_service.go`：拥有全局默认模型的唯一性、切换、创建兜底和解析规则。
- `frontend/src/components/ai-config-manager.vue`：展示默认标记与“设为默认”动作。

## Data Flow

```text
自然语言
  → ParseCronScheduleText RPC
  → 解析全局默认 AI 配置
  → AI JSON 输出
  → 后端解析、字段校验、确定性生成 Cron
  → robfig/cron 校验
  → 前端应用结构化意图
  → 用户继续手动编辑并确认
```

手动设置与专家 Cron 不依赖 AI：

```text
手动字段 ↔ 常用模式转换器 ↔ Cron 字符串 → CalculateNextRunTimes
```

## Contract

结构化意图包含：

- `mode`: `interval | daily | weekly | monthly`
- `intervalValue`, `intervalUnit`: 间隔模式使用
- `time`: `HH:mm`，固定时间模式使用
- `weekdays`: `0..6`，0 为周日
- `monthDay`: `1..31`
- `timezone`: 固定规范化为 `Asia/Shanghai`
- `summary`, `cronExpr`: 由后端确定性生成，不信任模型直接提供的值

全局默认模型：

- `AIConfig.isDefault` 持久化默认标记。
- `SetDefaultAIConfig(id)` 在事务内清除其他默认并设置目标配置。
- 后端解析 AI 配置时遵循“显式 ID → 默认配置 → 兼容旧数据的首项”顺序。
- 新建第一条配置时自动设为默认；复制配置不继承默认；删除默认配置后为剩余配置选择稳定兜底。

## Visual Contract

- 模态框宽度约 760px，主体和标题/底部操作区分隔，外层大圆角。
- AI 区为浅绿色背景、绿色细边框和 12px 圆角；输入与主按钮同一行，示例为浅绿胶囊按钮。
- 模式切换为四等分分段控件，激活项使用浅绿色底和绿色文字。
- 普通字段保持宽松垂直节奏；时区显示规范值 `Asia/Shanghai`。
- 摘要区为浅蓝背景和蓝色细边框，展示中文摘要及紧凑的未来时间行。
- 专家区默认折叠，使用顶部分隔线，不在主流程堆叠大块说明。

## Compatibility

- 数据库仍只保存 `CronTask.CronExpr`。
- 调度器仍使用六段式 `robfig/cron/v3`。
- 无法映射到常用模式的合法表达式以自定义 Cron 保存，不丢失原值。

## Error Handling

- AI 配置不存在、未配置、模型失败、JSON 无法解析或意图无效均返回错误。
- 错误信息不得包含 API Key、完整模型响应或其他敏感配置。
- 前端只有在 RPC 成功且结果通过本地转换后才覆盖当前编辑状态。
