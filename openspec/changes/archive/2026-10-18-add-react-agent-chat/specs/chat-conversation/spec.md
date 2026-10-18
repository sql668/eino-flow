# Spec Delta

## MODIFIED Requirements

### Requirement: 不支持的对话形态明确拒绝
系统 SHALL 按应用形态路由对话：智能体形态 SHALL 走智能体问答链路；工作流形态 SHALL 返回明确错误，
SHALL NOT 静默降级为普通对话或普通 RAG 问答。

#### Scenario: 智能体形态走智能体链路
- **WHEN** 客户端对一个形态为智能体的应用发起对话
- **THEN** 系统按智能体问答链路处理，响应中体现工具调用过程

#### Scenario: 未支持形态被拒绝
- **WHEN** 客户端对一个形态为工作流的应用发起对话
- **THEN** 系统返回明确的不支持提示，不返回普通对话回答
