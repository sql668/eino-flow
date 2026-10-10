// Package engine 承载所有 AI 编排逻辑，是后端最核心的一层。
//
// 职责边界：
//   - DSL 解析、Eino Graph 动态组装、Workflow / Agent 运行时、节点调度；
//   - 模型适配、工具调用、RAG 检索编排。
//   - 除本包外，其它层禁止实现任何 AI 执行逻辑。
//
// 参考：Eino 官方文档 https://www.cloudwego.io/zh/docs/eino/
//
// TODO(V0.1): 接入 Eino ChatModel / Memory / ReAct Agent / Retriever 基础封装。
package engine
