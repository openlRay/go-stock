# Web 浏览器原生文件上传

## Goal

Web 模式中凡是可以由浏览器原生文件选择、multipart 上传或 Blob 下载等标准能力等价实现的文件操作，不再仅隐藏入口；本次补齐交易记录文件导入和知识库单/多文件上传，确保 Docker 中的向量记忆目录可跨容器重建持久化，同时保持桌面 Wails 行为与现有 App 方法签名不变，并把相关兼容原则固化到 `sync-upstream-commits` 技能。

## Background

- Web Bridge 已为 `ExportConfig`、`ImportSkillPackage`、`SaveAsMarkdown`、`SaveImage` 和 `SaveWordFile` 提供浏览器原生替代，证据位于 `frontend/src/web-bridge.js:168-204`。
- 交易记录的 `ImportTradingRecordsFromExcel` 当前使用 Wails 文件对话框，Web UI 在 `frontend/src/components/TradingRecordManager.vue:783` 隐藏入口。
- 知识库的 `PickKBFilePath(s)` 与 `UploadKBFile(s)` 当前传递本地绝对路径，Web UI 在 `frontend/src/components/knowledge-base-manager.vue:597` 隐藏文件上传标签。
- 上述五个知识库/交易文件方法均被 `web_server.go:31-46` 排除在通用 Web RPC 表外；该安全边界应保留，浏览器文件内容通过专用 multipart API 传输，不能把客户端路径交给服务端读取。
- 知识库批量导入是后台异步流程，`StartBatchImport` 返回后仍读取文件，因此上传临时文件必须保留到后台处理完成，再由任务 owner 清理。
- 知识库现有单文件限制为 10MB，支持 `.txt`、`.md`、`.markdown`；交易记录解析支持 `.xls`、`.xlsx`、`.txt`、`.csv`，内容契约是 UTF-8/GBK 的 Tab 分隔文本。
- Web runtime root 是 `/app`，chromem-go 向量库、知识库元数据和知识图谱写入 `/app/memory/.vectorstore`；现有 Compose 未挂载 `/app/memory`，容器重建会丢失这些数据。

## Requirements

### R1. 交易记录浏览器上传

- Web 模式展示“导入记录”入口，并使用浏览器单文件选择器选择 `.xls`、`.xlsx`、`.txt` 或 `.csv`。
- 文件通过同源 multipart API 上传；服务端写入受控临时文件后复用 `StockDataApi.ImportTradingRecords`，返回结构与桌面 `ImportTradingRecordsFromExcel` 一致。
- 用户取消文件选择时保持现有 `nil`/无提示语义；上传或解析失败显示明确中文错误。
- 单文件上传设置 20MB 上限，避免 Web 请求和 `os.ReadFile` 无界占用内存；桌面导入行为不因本次 Web 适配改变。

### R2. 知识库浏览器上传

- Web 模式展示“上传文件（支持多选）”标签，复用现有选择列表、移除、批量入库和进度轮询交互。
- `PickKBFilePath` 与 `PickKBFilePaths` 在 Web Bridge 中使用浏览器文件选择器，并返回仅在当前页面有效的 opaque token；token 不包含可由服务端直接读取的客户端路径。
- `UploadKBFile` 与 `UploadKBFiles` 在 Web Bridge 中解析 token 对应的 `File`，通过同源 multipart API 上传。
- 单文件导入保持同步返回文档 ID；批量导入保持立即返回、后台向量化和现有状态轮询语义。
- 单文件限制 10MB，仅允许 `.txt`、`.md`、`.markdown`；批量最多 20 个文件且请求总大小不超过 100MB。
- 批量上传临时目录必须在后台导入完成或启动失败后清理，不得提前删除，也不得长期残留。

### R3. Web 安全与错误边界

- 新上传接口继续经过现有同源校验和安全响应头，不增加通配 CORS。
- 使用 `http.MaxBytesReader`、扩展名 allowlist、文件数量限制、逐文件大小限制和 `filepath.Base` 清理上传文件名。
- multipart 格式错误、缺少字段、类型不支持、文件过大和业务失败使用明确的 4xx/5xx 状态与安全中文错误；不得暴露服务端临时路径。
- `ImportTradingRecordsFromExcel`、`PickKBFilePath(s)`、`UploadKBFile(s)` 继续从通用反射 RPC 表排除，防止浏览器绕过上传边界传入服务器路径。

### R4. Desktop/Web 兼容

- Desktop 继续使用 Wails 文件对话框与原 App 方法，不改变生成 binding 的方法签名。
- Web Bridge 的 special method 保持现有 Wails Promise 返回形状，使业务组件不需要维护两套导入结果处理逻辑。
- 除移除 Web 隐藏条件外，不改变交易记录和知识库页面的桌面交互。

### R5. 同步技能规则

- 更新 `.agents/skills/sync-upstream-commits/SKILL.md` 与 `references/project-adaptation.md`：Web 兼容审查发现桌面文件选择、上传、保存或下载能力时，必须先判断浏览器是否有安全等价能力。
- 浏览器能够等价实现时，必须补 Web Bridge + 专用上传/下载接口及测试，不得仅隐藏入口；只有窗口、托盘、管理员重启等确无浏览器等价能力的功能才隐藏并拒绝。

### R6. Docker 向量记忆持久化

- Compose 增加独立命名卷 `go-stock-memory:/app/memory`，使向量库、知识库元数据、知识图谱和 Agent 长期记忆不依赖容器可写层。
- Dockerfile 在切换到 UID/GID 10001 前创建 `/app/memory` 并授予 `go-stock:go-stock` 写权限，保持与其他持久化目录一致。
- 部署 spec 与同步适配参考同步记录 `/app/memory` 持久化契约，防止后续上游同步覆盖。

## Acceptance Criteria

- [x] Web 交易记录页面展示导入按钮，选择合法文件后返回并展示与桌面一致的导入汇总；取消选择不报错。
- [x] Web 知识库页面展示文件上传标签，可多次选择、移除、批量上传合法文本文件，并继续通过现有状态轮询展示后台向量化进度。
- [x] Web Bridge 同时覆盖知识库单文件和多文件 App 方法；上传成功后释放已消费的浏览器 File token。
- [x] 交易文件类型/20MB 限制和知识库类型/10MB 单文件/20 文件/100MB 总量限制均由服务端验证，并有不依赖真实外部服务的 HTTP 测试。
- [x] 批量知识库上传在后台读取完成前保留临时文件，完成或失败后清理临时目录。
- [x] 五个服务器路径型 App 方法仍不进入通用 Web RPC；跨站上传请求仍被拒绝。
- [x] Desktop 与 Web Go 构建、Web 定点测试及前端构建通过；生成 bindings 无需变更或手工编辑。
- [x] 同步技能明确规定“浏览器可等价实现的文件能力不得仅隐藏”，并将交易记录/知识库上传作为示例。
- [x] Docker Compose 将 `/app/memory` 映射到独立命名卷，镜像内目录归非 root 用户所有，配置展开后可确认该挂载。

## Out of Scope

- 在浏览器中模拟窗口、托盘、管理员重启、客户端退出或桌面自更新。
- 扩展知识库对 PDF、DOC/DOCX 的解析能力。
- 将交易记录导入改造成真正的二进制 Excel 解析；继续沿用现有 Tab 分隔文本契约。
- 多用户上传隔离、登录、对象存储、断点续传或跨实例后台任务。
