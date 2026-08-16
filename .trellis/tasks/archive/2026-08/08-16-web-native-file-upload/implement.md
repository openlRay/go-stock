# Web 浏览器原生文件上传实施计划

## 1. 后端上传与生命周期

- [x] 在 `web_server.go` 增加交易记录、知识库单文件和知识库批量文件三个 multipart route。
- [x] 抽取受限 multipart 临时文件 helper，覆盖 media type、扩展名、数量和大小校验。
- [x] 交易记录 handler 复用 `StockDataApi.ImportTradingRecords`，同步清理临时文件并返回原结果结构。
- [x] 知识库单文件 handler 复用 `KnowledgeBaseApi.UploadFile`，同步返回 docIDs。
- [x] 为知识库批量导入增加 cleanup-aware 启动路径，确保后台完成、错误或 panic 后删除临时目录。
- [x] 保持五个服务器路径型 App 方法在 `webDesktopOnlyMethods` 中，不开放通用 JSON RPC。

## 2. Web Bridge 与 UI

- [x] 扩展 `frontend/src/web-bridge.js` 的文件选择 helper，支持多选和页面内 File token 注册表。
- [x] 为 `ImportTradingRecordsFromExcel`、`PickKBFilePath(s)`、`UploadKBFile(s)` 增加 special method。
- [x] 上传成功消费 token，失败保留 token 供重试。
- [x] 移除交易记录导入按钮和知识库文件上传标签的 Web 隐藏条件，并清理未使用的 `isWebMode` import。
- [x] 不修改生成的 Wails binding；静态确认 App 方法签名未变化。

## 3. 自动化验证

- [x] 扩展 `web_server_test.go`，覆盖合法上传、错误 media type、非法扩展名、大小/数量限制和路径型 RPC 仍被排除。
- [x] 为知识库 cleanup-aware 批量启动补不依赖真实 embedding 的生命周期测试，至少证明启动失败和后台完成路径会执行 cleanup。
- [x] 验证同源安全中间件仍覆盖新增 route。
- [x] 运行 `go test -tags web .` 的相关定点测试。
- [x] 运行相关 Agent/Data package 定点测试。

## 4. 技能更新

- [x] 更新 `.agents/skills/sync-upstream-commits/SKILL.md` 的强制 Web 兼容检查。
- [x] 更新 `references/project-adaptation.md` 的 Desktop/Web 文件能力规则、检查矩阵和验证要求。
- [x] 执行技能文档 diff review 与 `git diff --check`。

## 5. 构建与最终检查

- [x] `npm --prefix frontend run build`
- [x] `go build .`
- [x] `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -tags web .`
- [x] 若相关平台文件受影响，执行 Windows Web 交叉编译（本次未修改平台专用文件，无需执行）。
- [x] 检查工作区只包含本任务文件，报告未执行的真实浏览器验收或外部依赖测试。

## 6. Docker 向量记忆持久化

- [x] 在 `compose.yaml` 增加 `go-stock-memory:/app/memory` 和顶层命名卷声明。
- [x] 在 `Dockerfile` 为 UID/GID 10001 预创建可写的 `/app/memory`。
- [x] 更新部署 spec 与同步适配参考中的四目录持久化契约。
- [x] 运行 `docker compose config`，确认 `go-stock-memory` 展开正确。
- [x] 检查本地 Docker daemon；当前 daemon 未启动，因此镜像 build/smoke 与 volume 可写性验证未执行并如实记录。

## Verification Results

- Agent/Web 定点测试、JS 语法检查、前端生产构建、桌面构建、Linux Web 交叉构建、
  `git diff --check` 和技能 `quick_validate.py` 均通过。
- `go test ./...` 与 `go test -tags web ./...` 已在沙箱内外执行；本任务相关 package 通过，
  但全量命令被仓库既有实时外部接口测试失败/挂起阻断（节假日实时结果与硬编码预期不一致、
  AI 响应缺字段、行情 crawler 长时间等待）。未修改这些无关测试。
- 未执行真实浏览器交互验收；静态链路、HTTP handler 测试和前端 build 已覆盖本次主要风险。
- `docker compose config` 已确认 `go-stock-memory` 映射到 `/app/memory`；本机 Docker daemon
  未启动，无法执行镜像 build、非 root 可写性和 recreate 持久化 smoke。

## Rollback Points

- 后端 route 与 Web Bridge special method 应成对回滚，避免 UI 可见但上传无实现。
- cleanup-aware Agent helper 可独立回滚到现有 `StartBatchImport`，但必须同时恢复知识库 Web UI 隐藏，防止临时文件生命周期错误。
