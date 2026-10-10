// Package service 承载业务编排逻辑。
//
// 职责边界（见 backend/AGENTS.md）：
//   - 只做业务编排、事务边界、权限校验与跨模块组合；
//   - 数据访问走 repository，AI 编排走 engine，本层不直接实现这两者。
//
// TODO(V0.1): 实现应用管理、知识库管理、对话会话等基础服务。
package service
