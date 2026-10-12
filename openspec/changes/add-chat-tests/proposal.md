# Proposal

## Why

对话链路是目前唯一没有单测的模块：`service/chat.go` 与 `engine/chat` 的 Eino RAG chain 都没有测试
（`docs/PROGRESS.md` 第 4 节 #10、技术债与阶段 5 遗留）。这已经在实际操作中付出代价——
修 SSE 被压缩攒流、思考过程事件、打字机节奏这些问题时，只能靠手工点页面确认，
每次回归都要人来跑一遍。

## What Changes

- 为对话链路补测试：事件顺序、思考过程与正文的分离、引用来源先发、错误传播、形态路由；
- 建立可复用的测试替身（假模型 / 假检索实现），让测试不依赖真实 Ollama / 知识库；
- 把「对话链路的关键不变量」固化成断言，避免 D13 记录过的四类坑（攒流、思考混入正文、打字机落后、事件乱序）再次复发。

不做：不改动对话链路的实现行为（如发现必须改，另开 change）；
不引入新的测试框架（沿用现有 Go 测试与 `make backend-test`）。

## Capabilities

### New Capabilities

（无。）

### Modified Capabilities

（无：本 change 不改变任何对外行为，只增加测试覆盖，因此 `.openspec.yaml` 标记 `skip_specs: true`。
规格描述行为，行为没变就不该改规格。）

## Impact

- 后端：新增测试文件（`internal/service`、`internal/engine/chat` 等），不改生产代码；
- 门禁：`make backend-check` 覆盖范围变宽，回归成本下降；
- 无接口、无数据、无依赖变化。
