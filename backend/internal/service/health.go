package service

import (
	"context"
	"fmt"

	"einoflow-backend/internal/repository"
)

// HealthService 汇总各依赖组件的连通性，供就绪探针使用。
type HealthService struct {
	health repository.HealthRepository
}

// NewHealthService 构造健康检查服务。
func NewHealthService(health repository.HealthRepository) *HealthService {
	return &HealthService{health: health}
}

// Check 检查所有依赖；任一不可用即视为未就绪。
// 后续接入 Redis、Milvus、MinIO 时在这里继续追加检查。
func (s *HealthService) Check(ctx context.Context) error {
	if err := s.health.Ping(ctx); err != nil {
		return fmt.Errorf("PostgreSQL 不可用: %w", err)
	}
	return nil
}
