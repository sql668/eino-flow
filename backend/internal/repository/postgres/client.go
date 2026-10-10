package postgres

import (
	"context"
	"database/sql"

	"gorm.io/gorm"
)

// txKey 是事务句柄在 context 中的键。用未导出空结构体，避免与其它包的键冲突。
type txKey struct{}

// Client 持有 GORM 连接，并提供事务能力。
//
// 仓储统一通过 DB(ctx) 取会话：context 中带事务就用事务，否则用主连接。
// 这样事务边界由 service 层划定，仓储代码不必知道自己是否在事务里。
type Client struct {
	db *gorm.DB
}

// NewClient 构造数据访问客户端。
func NewClient(db *gorm.DB) *Client {
	return &Client{db: db}
}

// DB 返回当前应使用的 GORM 会话。
// 仓储的所有查询都必须经过它，否则不会加入事务。
func (c *Client) DB(ctx context.Context) *gorm.DB {
	if tx, ok := ctx.Value(txKey{}).(*gorm.DB); ok {
		return tx.WithContext(ctx)
	}
	return c.db.WithContext(ctx)
}

// SQL 返回底层连接池，供健康检查等基础设施使用。
func (c *Client) SQL() (*sql.DB, error) {
	return c.db.DB()
}

// WithinTransaction 在一个事务内执行 fn。
//
// 事务句柄通过 context 传递，fn 中调用仓储方法时自动取到同一个事务，
// 因此跨多个仓储的事务不需要额外传参。
func (c *Client) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	return c.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(context.WithValue(ctx, txKey{}, tx))
	})
}
