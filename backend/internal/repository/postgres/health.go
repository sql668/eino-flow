package postgres

import "context"

// HealthRepository 用底层连接池实现 repository.HealthRepository。
type HealthRepository struct {
	client *Client
}

// NewHealthRepository 构造 PostgreSQL 健康检查仓储。
func NewHealthRepository(client *Client) *HealthRepository {
	return &HealthRepository{client: client}
}

// Ping 检查数据库连通性。
func (r *HealthRepository) Ping(ctx context.Context) error {
	sqlDB, err := r.client.SQL()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}
