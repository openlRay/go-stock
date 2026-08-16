# 定时任务执行时间设置设计

## Boundaries

- `frontend/src/components/common/AppModalShell.vue`：拥有通用弹框的标题栏、关闭按钮、可滚动正文、底部操作栏、响应式尺寸和稳定视觉 token；不拥有 Cron 业务面板样式。
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

本节只记录本任务的实现契约，不作为其他业务组件必须复用的全局规范。

Wails RPC 使用以下结构：

```go
type CronScheduleParseRequest struct {
    Text       string `json:"text"`
    AIConfigID int    `json:"aiConfigId"`
}

type CronScheduleParseResult struct {
    Mode          string `json:"mode"`
    IntervalValue int    `json:"intervalValue,omitempty"`
    IntervalUnit  string `json:"intervalUnit,omitempty"`
    Time          string `json:"time,omitempty"`
    Weekdays      []int  `json:"weekdays,omitempty"`
    MonthDay      int    `json:"monthDay,omitempty"`
    Timezone      string `json:"timezone"`
    Summary       string `json:"summary"`
    CronExpr      string `json:"cronExpr"`
}
```

结构化意图包含：

- `mode`: `interval | daily | weekly | monthly`
- `intervalValue`, `intervalUnit`: 间隔模式使用；单位为 `minute | hour | day`，范围分别为 `1..59`、`1..23`、`1..31`
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

- 通用弹框实现遵循[表单弹框规范](../../spec/frontend/dialog-guidelines.md)中的全局视觉规则；本节只记录当前任务如何应用这些规则。
- Cron 编辑器通过 `AppModalShell` 的默认插槽和 footer 插槽组合业务内容；绿色 AI 区、模式切换、蓝色摘要和专家区仍由 Cron 编辑器拥有。
- 模态框宽度约 760px，主体和标题/底部操作区分隔，外层大圆角。
- AI 区为浅绿色背景、绿色细边框和 12px 圆角；输入与主按钮同一行，示例为浅绿胶囊按钮。
- 模式切换为四等分分段控件，激活项使用浅绿色底和绿色文字。
- 普通字段保持宽松垂直节奏；时区显示规范值 `Asia/Shanghai`。
- 摘要区为浅蓝背景和蓝色细边框，展示中文摘要及紧凑的未来时间行。
- 专家区默认折叠，使用顶部分隔线，不在主流程堆叠大块说明。

## Compatibility

- 数据库仍只保存 `CronTask.CronExpr`。
- 调度器仍使用六段式 `robfig/cron/v3`。
- “每 N 天”使用 `0 0 0 */N * *`，在 `00:00` 按月内日期步进并在跨月后重新计算，不表示严格持续时间间隔。
- 无法映射到常用模式的合法表达式以自定义 Cron 保存，不丢失原值。

## Error Handling

- AI 配置不存在、未配置、模型失败、JSON 无法解析或意图无效均返回错误。
- 错误信息不得包含 API Key、完整模型响应或其他敏感配置。
- 前端只有在 RPC 成功且结果通过本地转换后才覆盖当前编辑状态。

## UI Implementation Notes

- `n-time-picker` 通过 `format="HH:mm"` 和 `value-format="HH:mm"` 控制精度，不传空的 `seconds` 选项；空选项会造成时间不可选或文字出现异常中划线。
- 每周日期使用标准 checkbox group 表达多选语义，星期值为 `0..6`，其中 `0` 表示周日。
- `n-modal` 的直属子节点保持为真实 DOM shell。通用组件内部完成这层结构，避免 focus trap 无法定位根节点而使专家输入框失去编辑能力。
- 专家表达式只有在后端返回精确成功值 `有效表达式` 时才应用；不能使用 `includes("有效")`，因为错误文案 `无效表达式` 同样包含“有效”。
- 普通模式不能无损表达的合法 Cron 保留为自定义表达式，不能为了回填表单而改写原值。

## Validation Matrix

| 场景 | 本任务行为 |
| --- | --- |
| AI 文本为空 | 构造模型前返回可理解的错误，不覆盖表单。 |
| 没有默认 AI 配置 | 提示用户配置模型，手动和专家编辑仍然可用。 |
| 模型输出字段缺失或越界 | 拒绝结果，不猜测修复。 |
| 每天间隔不在 `1..31` | 拒绝结果并保留当前编辑状态。 |
| 专家 Cron 合法但无法映射普通模式 | 原样保留为自定义 Cron。 |
| 专家 Cron 非法 | 展示校验错误并禁止确认。 |
| 宿主机不是中国时区 | Scheduler 与预览仍使用 `Asia/Shanghai`。 |
