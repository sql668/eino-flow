package postgres

import (
	"context"
	"fmt"

	"einoflow-backend/internal/model"
)

// RetrievalRepository 是基于 PostgreSQL（GORM）的检索留痕实现。
type RetrievalRepository struct {
	client *Client
}

// NewRetrievalRepository 构造检索留痕仓储。
func NewRetrievalRepository(client *Client) *RetrievalRepository {
	return &RetrievalRepository{client: client}
}

// CreateRun 写入检索记录，GORM 会把自增主键回填到入参对象上。
func (r *RetrievalRepository) CreateRun(ctx context.Context, run *model.RetrievalRun) error {
	if err := r.client.DB(ctx).Create(run).Error; err != nil {
		return fmt.Errorf("写入检索记录失败: %w", err)
	}
	return nil
}

// CreateHits 批量写入命中片段。
func (r *RetrievalRepository) CreateHits(ctx context.Context, hits []*model.RetrievalHit) error {
	if len(hits) == 0 {
		return nil
	}
	if err := r.client.DB(ctx).CreateInBatches(hits, segmentBatchSize).Error; err != nil {
		return fmt.Errorf("写入检索命中记录失败: %w", err)
	}
	return nil
}
