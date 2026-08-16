# Web 浏览器原生文件上传设计

## 1. 边界与原则

保留“生成 binding → `window.go.main.App`”这一条业务组件调用链。Desktop 继续调用真实 App 方法；Web Bridge 对文件方法提供 special method，不把客户端文件路径发送给通用 JSON RPC。

`webDesktopOnlyMethods` 继续排除以下方法：

- `ImportTradingRecordsFromExcel`
- `PickKBFilePath`
- `PickKBFilePaths`
- `UploadKBFile`
- `UploadKBFiles`

它们在 Web 中由浏览器选择器和专用 multipart endpoint 等价实现。这样既保留调用签名，又阻止构造 JSON RPC 读取任意服务器路径。

## 2. 数据流

### 2.1 交易记录

```text
TradingRecordManager
  → ImportTradingRecordsFromExcel binding
  → Web Bridge special method
  → 浏览器单文件选择
  → POST /api/trading-records/import (multipart file)
  → 受限临时文件
  → StockDataApi.ImportTradingRecords
  → TradingRecordImportResult JSON
  → 现有前端提示与列表刷新
```

请求完成后同步删除临时文件。浏览器取消选择时 special method 返回 `null`，不发 HTTP 请求。

### 2.2 知识库单文件

```text
PickKBFilePath → 浏览器 File → 页面内 opaque token
UploadKBFile(kbName, token)
  → POST /api/knowledge-base/file/import
  → 临时文件 → AddFileToKB → docIDs
  → 删除临时文件 → 返回 docIDs
```

### 2.3 知识库批量文件

```text
PickKBFilePaths → 浏览器 File[] → 页面内 opaque token[]
UploadKBFiles(kbName, tokens)
  → POST /api/knowledge-base/files/import
  → 每请求独立临时目录
  → StartBatchImportWithCleanup
  → 立即返回成功并沿用现有状态轮询
  → 后台 AddFilesToKB 完成/失败
  → cleanup 删除整个临时目录
```

cleanup 的 owner 是启动后台导入的 Agent 层 helper，因为只有该层准确知道异步读取何时结束。Desktop 的 `StartBatchImport` 传空 cleanup，行为不变。

## 3. Web Bridge 文件注册表

- 将现有 `selectFile` 扩展为支持单选/多选的共享选择器。
- Web Bridge 内维护 `Map<opaqueToken, File>`，token 只在当前页面生命周期有效。
- token 带可展示的安全文件名尾段，组件现有 `split(/[\\/]/).pop()` 可以继续显示名称；服务端从不解析该 token。
- 同一浏览器文件签名（name、size、lastModified）复用 token，使现有列表去重仍有效。
- 上传成功后删除已消费 token；请求失败时保留，允许用户重试。
- `UploadKBFile(s)` 只接受注册表中存在的 token，未知 token 在浏览器侧直接报错，不发空请求。

## 4. HTTP 上传实现

在 `web_server.go` 注册三个同源 endpoint：

- `POST /api/trading-records/import`
- `POST /api/knowledge-base/file/import`
- `POST /api/knowledge-base/files/import`

抽取聚焦的 multipart 保存 helper，统一负责：

- media type 检查；
- `MaxBytesReader` 总量限制；
- `ParseMultipartForm` 后 `RemoveAll`；
- 文件数量、扩展名、单文件大小检查；
- `filepath.Base` 文件名清理；
- 每请求独立临时目录和受控文件权限；
- copy/close 错误链和清理。

业务 handler 只负责字段验证、调用现有 service，以及把错误转换为稳定 HTTP 响应。测试通过 handler 依赖回调或最小 service seam 避免访问真实 AI/外部服务。

## 5. Agent 临时文件生命周期

将 `StartBatchImport` 的 goroutine 启动逻辑收敛到内部 helper：

```text
startBatchImport(kbName, filePaths, cleanup)
```

- `StartBatchImport` 调用时 cleanup 为 nil。
- 新的 Web 上传入口调用时传入删除请求临时目录的 cleanup。
- 启动前校验失败由 HTTP handler 立即清理。
- goroutine 使用 `defer cleanup()`，覆盖成功、业务错误和 panic。

不得由 HTTP handler 在返回 200 后立即 `defer os.RemoveAll`，否则后台向量化会读取已删除文件。

## 6. UI 与 Desktop 兼容

- 删除交易记录按钮和知识库文件标签上的 `v-if="!isWebMode"`。
- 若组件不再使用 `isWebMode`，删除对应 import。
- 业务组件继续调用生成的 Wails binding，不直接 `fetch`，避免 Desktop/Web 分叉。
- App 方法签名不变，因此不重新生成 binding；验证生成文件没有手工差异。

## 7. 技能契约

`sync-upstream-commits` 的强制 Web 审查新增决策顺序：

1. 识别 Desktop-only 文件能力及调用链。
2. 判断浏览器是否有原生等价能力。
3. 有等价能力：实现 bridge + 专用安全 endpoint + UI 可达性 + 测试。
4. 无等价能力：隐藏 UI、排除 RPC，并提供明确不可用语义。

交易 Excel 导入和知识库文本文件上传作为正例；窗口、托盘、管理员重启作为确实不可替代的反例。

## 8. 风险与回滚

- 最大风险是知识库临时文件被过早删除；由 Agent cleanup owner 和生命周期测试覆盖。
- 浏览器 File 注册表只存在于页面内，刷新页面会清空未上传选择，属于浏览器原生选择器的预期行为。
- 上传接口仍是单用户同源模型，不引入登录或租户隔离。
- 回滚时可以恢复 UI 隐藏并移除三个 endpoint/special method；Desktop 不受影响。

## 9. Docker 向量记忆持久化

Web 模式通过 `runtimepath.RootDir()` 使用 `/app`，因此 chromem-go 的持久化目录是
`/app/memory/.vectorstore`。Compose 使用独立命名卷 `go-stock-memory` 挂载整个
`/app/memory`，同时 Dockerfile 预创建该目录并设置 UID/GID 10001，避免首次挂载后
非 root 进程无写权限。

独立 memory volume 与 data volume 分离，避免把 SQLite 备份策略和向量文件备份策略
混为一体。已有容器首次启用该卷前需先复制旧容器层的 `/app/memory`；空命名卷不会
自动迁移历史数据。回滚该挂载不会影响 Desktop，但容器重建将重新具有数据丢失风险。
