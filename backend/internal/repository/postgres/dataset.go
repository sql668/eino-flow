package postgres

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"einoflow-backend/internal/model"
	"einoflow-backend/internal/repository"
)

// DatasetRepository 是基于 PostgreSQL（GORM）的实现。
type DatasetRepository struct {
	client *Client
}

// NewDatasetRepository 构造知识库仓储。
func NewDatasetRepository(client *Client) *DatasetRepository {
	return &DatasetRepository{client: client}
}

// Create 写入新知识库。
func (r *DatasetRepository) Create(ctx context.Context, dataset *model.Dataset) error {
	if err := r.client.DB(ctx).Create(dataset).Error; err != nil {
		return fmt.Errorf("创建知识库失败: %w", err)
	}
	return nil
}

// GetByID 按主键查询；不存在或已软删除时返回 repository.ErrNotFound。
func (r *DatasetRepository) GetByID(ctx context.Context, id uint64) (*model.Dataset, error) {
	var dataset model.Dataset
	if err := r.client.DB(ctx).Where("id = ?", id).First(&dataset).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("查询知识库失败: %w", err)
	}
	return &dataset, nil
}

// List 按条件分页查询知识库。
func (r *DatasetRepository) List(ctx context.Context, filter repository.DatasetListFilter) ([]*model.Dataset, int64, error) {
	build := func() *gorm.DB {
		query := r.client.DB(ctx).Model(&model.Dataset{})
		if filter.Keyword != "" {
			query = query.Where("name ILIKE ?", "%"+escapeLike(filter.Keyword)+"%")
		}
		return query
	}

	var total int64
	if err := build().Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("统计知识库数量失败: %w", err)
	}
	if total == 0 {
		return []*model.Dataset{}, 0, nil
	}

	var rows []model.Dataset
	if err := build().Order("id DESC").Limit(filter.Limit).Offset(filter.Offset).Find(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("查询知识库列表失败: %w", err)
	}

	datasets := make([]*model.Dataset, 0, len(rows))
	for i := range rows {
		datasets = append(datasets, &rows[i])
	}
	return datasets, total, nil
}

// Update 更新知识库；记录不存在时返回 repository.ErrNotFound。
func (r *DatasetRepository) Update(ctx context.Context, dataset *model.Dataset) error {
	result := r.client.DB(ctx).
		Model(&model.Dataset{}).
		Where("id = ?", dataset.ID).
		// 显式列出可更新列：GORM 的 Updates(struct) 会跳过零值，
		// 而空白描述、空状态都是合法值；同时新增列（分段/检索配置）必须在这里出现，
		// 否则调用方以为存上了、实际连 SQL 都没生成（这次就踩到了）。
		Select("name", "description", "embedding_model", "status",
			"chunk_config", "retrieval_config").
		Updates(dataset)
	if result.Error != nil {
		return fmt.Errorf("更新知识库失败: %w", result.Error)
	}

	updated, err := r.GetByID(ctx, dataset.ID)
	if err != nil {
		return err
	}
	*dataset = *updated
	return nil
}

// SoftDelete 软删除知识库。
func (r *DatasetRepository) SoftDelete(ctx context.Context, id uint64) error {
	result := r.client.DB(ctx).Where("id = ?", id).Delete(&model.Dataset{})
	if result.Error != nil {
		return fmt.Errorf("删除知识库失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return repository.ErrNotFound
	}
	return nil
}
