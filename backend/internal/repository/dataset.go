package repository

import (
	"context"

	"einoflow-backend/internal/model"
)

// DatasetListFilter 是知识库列表的查询条件。
type DatasetListFilter struct {
	Keyword string // 按名称模糊匹配
	Offset  int
	Limit   int
}

// DatasetRepository 描述知识库的持久化能力。
type DatasetRepository interface {
	Create(ctx context.Context, dataset *model.Dataset) error
	GetByID(ctx context.Context, id uint64) (*model.Dataset, error)
	List(ctx context.Context, filter DatasetListFilter) ([]*model.Dataset, int64, error)
	Update(ctx context.Context, dataset *model.Dataset) error
	SoftDelete(ctx context.Context, id uint64) error
}
