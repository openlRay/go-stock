# 后端全局规范重构实施计划

1. 保存当前 backend spec 与相关 task 的状态清单，确认并行 WIP 文件。
2. 将 `ai-model-configuration.md` 中 task-specific 内容补入 AI 配置 task design。
3. 将 `announcement-ai-analysis.md` 当前 working tree 的增量决策补入公告 task design，重点保留 Eastmoney challenge 边界。
4. 把飞书签名协议迁入 `docs/integrations/feishu-webhook.md`。
5. 重写目录、数据库、错误、日志和质量规范，删除所有模板占位文本。
6. 新增 HTTP 与 AI 全局集成规范，从多个真实功能提炼共同规则。
7. 将旧 `web-runtime.md` 拆为运行时和部署规范；与外部请求相关的规则归入 HTTP 规范。
8. 更新 backend `index.md`，删除 4 个已迁出的旧 spec。
9. 执行文档质量检查并逐项修正：

```bash
rg -n "To be filled|TBD|TODO: fill|placeholder|待完善" .trellis/spec/backend
rg -n "ai-model-configuration|announcement-ai-analysis|feishu-webhook|web-runtime" .trellis/spec .trellis/tasks docs
git diff --check
```

10. 使用只读脚本检查 Markdown 相对链接和 spec 中引用的项目文件是否存在。
11. 审查最终 diff，确认没有 Go/前端/Docker/CI 修改，没有覆盖无关 WIP，并如实报告未运行代码测试。

## 风险点

- 公告分析 spec 含未提交并行改动；必须先迁移 working tree 内容再删除。
- 旧 `web-runtime.md` 内容跨度大；拆分后要防止环境变量、运行目录或安全边界遗漏。
- “源码证据”与“功能实现复制”边界容易漂移；每份 spec 最终需反向搜索具体功能 RPC/字段矩阵。

## 评审门槛

- 用户明确批准本 PRD、design 和 implement 摘要后，才运行 `task.py start` 并修改 backend spec。
- 若实施中发现必须修改 Go 代码或新增未约定的文档类别，回到规划阶段重新确认。
