package repository

import "context"

// TxManager 提供事务能力。
//
// 放在 repository 包是为了让 service 层能划定事务边界，又不必知道底层用的是哪种数据库
// （见 backend/AGENTS.md「事务边界放在 service 层」）。具体实现由存储层提供。
type TxManager interface {
	// WithinTransaction 在同一个事务内执行 fn；fn 返回错误则整体回滚。
	// 事务句柄通过 fn 的 context 透传，fn 内调用仓储方法时自动复用同一事务。
	WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}
