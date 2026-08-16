# 数据库与迁移规范

## 适用范围

修改 SQLite/GORM model、查询、写入、事务、启动迁移或数据库测试时使用本规范。数据库是单进程本地状态源，Web 部署同样使用 SQLite，不得按多节点数据库假设实现。

## 初始化与 SQLite 约束

`backend/db/db.go` 的 `db.Init` 是数据库初始化入口：

- 默认数据库位于 `data/stock.db`。
- 使用 WAL、`busy_timeout=10000` 和 `synchronous=NORMAL`。
- SQLite 写入采用串行锁模型，连接池保持受控；不要通过增加写连接规避 `SQLITE_BUSY`。
- `SkipDefaultTransaction=true` 表示 GORM 不为每次写入自动包事务，多步一致性必须由业务显式建立事务。
- 数据库初始化失败属于进程启动失败；普通 service 查询/写入失败必须返回 error，不能终止进程。

部署时必须持久化整个 `data` 目录，使 `stock.db`、WAL 和 SHM 文件位于同一 volume。具体部署约束见[部署与持久化规范](./deployment-guidelines.md)。

## Model 与表结构

- 跨层持久化 model 放入 `backend/models`；只属于单个 data service 的表结构可以留在该 package，但不得与 UI 状态混合。
- 已存在稳定表名时实现 `TableName()`，不要依赖重命名 Go type 后的自动复数规则。
- JSON tag 与 GORM tag 分别服务 RPC/持久化边界；业务唯一键必须显式使用 `uniqueIndex` 或明确迁移。
- 可选数值需要区分“未配置”和显式 `0` 时使用 pointer 或单独 configured 标记，不能依赖 Go 零值猜测。
- 软删除只用于确实需要保留历史的表；不要把软删除当作所有 model 的默认选项。

参考：`backend/models/models.go`、`backend/models/announcement_ai_analysis.go`、`backend/db/chat_memory.go`。

## 迁移规则

- 新增列和表优先使用 additive `AutoMigrate`；删除列、改类型、重建表等破坏性迁移必须单独设计并获得确认。
- 需要修复旧数据时，使用有名称、可重复执行的同步 migration 函数；schema 变更与数据修复必须在后续读取前完成。
- migration 必须幂等：重复启动不能改变已经合法的数据，也不能产生多个默认记录或重复业务行。
- 不得在高频 getter 中懒执行 schema migration；并发读取会放大失败并隐藏启动阶段问题。
- 不忽略 migration error。入口必须记录明确上下文并阻止依赖该 schema 的流程继续运行。

参考模式：`backend/db/chat_memory.go` 的同步 migration 与 `backend/db/ai_config_default_migration_test.go` 的 legacy schema 测试。

## 事务与写入

以下场景必须使用 `db.Dao.Transaction`：

- 多行或多表必须同时成功；
- “检查引用后删除”；
- 切换唯一默认记录；
- 先写数据再更新 metadata；
- 失败时必须保留上一份成功结果。

事务闭包只使用传入的 `tx`，不得在闭包中混用全局 `db.Dao`。任何一步返回 error 都让事务回滚。

更新含 `false`、`0`、空字符串或 `nil` 语义时使用显式字段 map 或明确 `Select`，避免 GORM struct updates 静默跳过零值。Upsert 必须声明冲突键与更新列，不使用不受控的全字段覆盖。

参考：`backend/db/stock_transaction_cache.go`、`backend/data/ai_config_service.go`、`backend/data/announcement_ai_analysis.go`。

## 查询与错误语义

- 查询前先 normalize 和 validate 外部输入；分页必须限制 page size、offset 和排序字段。
- `gorm.ErrRecordNotFound` 与真实数据库错误分开处理。不存在是否属于正常空结果，由 service 契约决定。
- 返回 slice 时保持稳定排序；需要倒序读取再正序展示时，在拥有查询语义的层完成转换。
- 不把 GORM error 文本直接作为前端协议；在 App/RPC 边界转换为用户安全错误。

## 场景：`default:true` 布尔字段的显式关闭

### 1. Scope / Trigger

持久化 model 使用 `gorm:"default:true"`，同时创建 API 允许调用方显式传入 `false` 时适用。

### 2. Signatures

- DB 字段：`enable BOOLEAN NOT NULL DEFAULT TRUE`。
- 创建输入：`Enable bool`，其中 `false` 是有效业务值，不得解释为“未提供”。

### 3. Contracts

GORM 创建带默认值 tag 的 struct 时可能把 Go 零值替换为数据库默认值。创建 service 必须通过 pointer/独立 configured 标记、显式字段 map，或同一事务内的显式列写入保存 `false`；返回给调用方的 model 也必须与数据库一致。

### 4. Validation & Error Matrix

| 条件 | 行为 |
| --- | --- |
| 调用方显式传 `false` | 数据库保存 `false`，后续依赖该开关的副作用不启动 |
| 显式零值写入失败 | 整个创建事务回滚并返回带 cause 的 error |
| 字段确实需要“未提供”语义 | 改用 `*bool` 或单独 configured 字段，不用零值猜测 |

### 5. Good / Base / Bad Cases

- Good：事务创建后显式写入 `false`，并把 struct 字段恢复为 `false`。
- Base：使用 `*bool` 区分未提供与关闭。
- Bad：直接 `Create(&model{Enable:false})` 并假设 `default:true` 不会覆盖零值。

### 6. Tests Required

- 内存 SQLite 创建 `false`，同时断言输入对象和重新读取记录均为 `false`。
- 模拟第二步写入失败时断言没有残留创建记录。

### 7. Wrong vs Correct

```go
// Wrong：default:true 可能把 false 改成 true。
db.Create(&task)

// Correct：在同一事务中明确保存调用方拥有的零值。
db.Transaction(func(tx *gorm.DB) error {
    if err := tx.Create(&task).Error; err != nil { return err }
    return tx.Model(&task).UpdateColumn("enable", false).Error
})
```

## 测试隔离

- 数据库单元测试使用独立内存 SQLite DSN，并对同名测试启用独立 cache key。
- 测试临时替换全局 `db.Dao` 时，使用 `t.Cleanup` 恢复旧值，避免污染同进程其他测试。
- migration 测试应从旧 schema 手工建表，再断言升级、数据修复和第二次执行结果。
- 事务测试至少覆盖中途失败时没有部分写入。

## 禁止模式

- 通过增加 SQLite 写连接数解决锁竞争。
- 在 transaction 中一部分使用 `tx`、另一部分使用全局 `Dao`。
- 用 `Save` 隐式覆盖调用方未拥有的列。
- 删除旧结果后再执行可能失败的网络或 AI 生成。
- 测试复用生产数据库文件或依赖执行顺序。

## 验证

- 纯查询变化运行对应 data/db package 定点测试。
- schema/migration 变化运行 legacy schema、幂等和回滚测试。
- 修改 SQLite 初始化或 volume 约束时额外执行 Web/Docker 持久化检查。
