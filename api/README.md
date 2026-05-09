# API 辅助产物

旧 `docs/API/*.md` 已退役并移除。本目录只保留仍被当前后端 / CLI 实现直接引用的辅助产物,不得作为前端契约来源。

## 当前结构

- `protobuf/aitask/v1/*.proto`: CLI / Agent ConnectRPC 仍使用的 protobuf 定义。
- `protobuf/docs/aitask-proto.md`: protobuf 参考文档。
- `websocket/agent-room-envelope.schema.json`: Room WebSocket envelope JSON Schema。
- `websocket/agent-room-envelope.ts`: Room WebSocket envelope TypeScript 类型。

## 已废弃

- 不再维护 `openapi/openapi.yaml`。
- 不再维护 `mock/*.json`。
- 不再运行 OpenAPI / mock 自动同步或差异门禁。

## 约束

- 前后端 REST API 修改必须手动同步后端 handler 与前端 client / DTO,并在对应组件 README 或任务交接文档中记录契约变化。
- 禁止新增依赖 `openapi.yaml` 的生成、校验、CI 或任务说明。
- 前端开发不使用 mock fixture,必须直连真实后端或本地开发后端。
