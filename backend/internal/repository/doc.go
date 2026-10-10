// Package repository 承载数据访问逻辑。
//
// 职责边界：
//   - 只做数据存取（MySQL / Milvus / MinIO / Redis），不含业务语义；
//   - 对外暴露 interface，便于替换实现与单测打桩。
//
// TODO(V0.1): 实现 MySQL 基础表（应用 / 知识库 / 对话记录）访问层。
package repository
