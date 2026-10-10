// Package repository 承载数据访问逻辑。
//
// 职责边界：
//   - 只做数据存取（PostgreSQL / Milvus / MinIO / Redis），不含业务语义；
//   - 对外暴露 interface，便于替换实现与单测打桩；
//   - 实现统一把「记录不存在」翻译成 ErrNotFound，避免上层去理解 sql.ErrNoRows 之类的细节。
package repository

import "errors"

// ErrNotFound 表示记录不存在。
// 各存储实现遇到「查不到」时统一返回它，上层用 errors.Is 判断即可。
var ErrNotFound = errors.New("记录不存在")
