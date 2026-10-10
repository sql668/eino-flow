package postgres

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"einoflow-backend/internal/model"
	"einoflow-backend/internal/repository"
)

// ModelProviderRepository 是基于 PostgreSQL（GORM）的厂商凭证实现。
type ModelProviderRepository struct {
	client *Client
}

// NewModelProviderRepository 构造厂商凭证仓储。
func NewModelProviderRepository(client *Client) *ModelProviderRepository {
	return &ModelProviderRepository{client: client}
}

// Upsert 按厂商标识写入或覆盖配置。
//
// 用 ON CONFLICT 而不是「先查再决定插入还是更新」：后者在并发保存时会撞唯一索引，
// 而且两次往返换来的只是「少一次 upsert」。
func (r *ModelProviderRepository) Upsert(ctx context.Context, provider *model.ModelProvider) error {
	err := r.client.DB(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "provider"}},
		// 只更新用户可改的字段：凭证为空时也应覆盖（表示用户清空了凭证）。
		DoUpdates: clause.AssignmentColumns([]string{
			"display_name", "base_url", "credentials", "enabled", "updated_at",
		}),
	}).Create(provider).Error
	if err != nil {
		return fmt.Errorf("保存厂商配置失败: %w", err)
	}
	return nil
}

// GetByProvider 按厂商标识查询。
func (r *ModelProviderRepository) GetByProvider(ctx context.Context, provider string) (*model.ModelProvider, error) {
	var row model.ModelProvider
	if err := r.client.DB(ctx).Where("provider = ?", provider).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("查询厂商配置失败: %w", err)
	}
	return &row, nil
}

// GetByID 按主键查询。
func (r *ModelProviderRepository) GetByID(ctx context.Context, id uint64) (*model.ModelProvider, error) {
	var row model.ModelProvider
	if err := r.client.DB(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("查询厂商配置失败: %w", err)
	}
	return &row, nil
}

// List 返回全部厂商配置。
func (r *ModelProviderRepository) List(ctx context.Context) ([]*model.ModelProvider, error) {
	var rows []model.ModelProvider
	if err := r.client.DB(ctx).Order("provider ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("查询厂商配置列表失败: %w", err)
	}
	items := make([]*model.ModelProvider, 0, len(rows))
	for i := range rows {
		items = append(items, &rows[i])
	}
	return items, nil
}

// Delete 删除厂商配置。
func (r *ModelProviderRepository) Delete(ctx context.Context, provider string) error {
	result := r.client.DB(ctx).Where("provider = ?", provider).Delete(&model.ModelProvider{})
	if result.Error != nil {
		return fmt.Errorf("删除厂商配置失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return repository.ErrNotFound
	}
	return nil
}

// ModelInstanceRepository 是基于 PostgreSQL（GORM）的模型清单实现。
type ModelInstanceRepository struct {
	client *Client
}

// NewModelInstanceRepository 构造模型仓储。
func NewModelInstanceRepository(client *Client) *ModelInstanceRepository {
	return &ModelInstanceRepository{client: client}
}

// Create 写入模型。
func (r *ModelInstanceRepository) Create(ctx context.Context, instance *model.ModelInstance) error {
	if err := r.client.DB(ctx).Create(instance).Error; err != nil {
		return fmt.Errorf("创建模型失败（同厂商下同类型同名模型是否已存在？）: %w", err)
	}
	return nil
}

// GetByID 按主键查询。
func (r *ModelInstanceRepository) GetByID(ctx context.Context, id uint64) (*model.ModelInstance, error) {
	var row model.ModelInstance
	if err := r.client.DB(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("查询模型失败: %w", err)
	}
	return &row, nil
}

// List 按条件查询模型。
func (r *ModelInstanceRepository) List(ctx context.Context, filter repository.ModelInstanceFilter) ([]*model.ModelInstance, error) {
	query := r.client.DB(ctx).Model(&model.ModelInstance{})
	if filter.ModelType != "" {
		query = query.Where("model_type = ?", string(filter.ModelType))
	}
	if filter.ProviderID != 0 {
		query = query.Where("provider_id = ?", filter.ProviderID)
	}
	if filter.Enabled != nil {
		query = query.Where("enabled = ?", *filter.Enabled)
	}

	var rows []model.ModelInstance
	if err := query.Order("model_type ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("查询模型列表失败: %w", err)
	}
	items := make([]*model.ModelInstance, 0, len(rows))
	for i := range rows {
		items = append(items, &rows[i])
	}
	return items, nil
}

// Update 更新模型的可改字段。
//
// 用 Select 显式指定列：GORM 的 Updates(struct) 会跳过零值，
// 而 enabled=false 正是零值——不指定列会出现「关不掉的开关」。
func (r *ModelInstanceRepository) Update(ctx context.Context, instance *model.ModelInstance) error {
	result := r.client.DB(ctx).
		Model(&model.ModelInstance{}).
		Where("id = ?", instance.ID).
		Select("display_name", "enabled", "updated_at").
		Updates(instance)
	if result.Error != nil {
		return fmt.Errorf("更新模型失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return repository.ErrNotFound
	}
	return nil
}

// Delete 删除模型并级联删除其绑定（同一事务）。
func (r *ModelInstanceRepository) Delete(ctx context.Context, id uint64) error {
	return r.client.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := r.client.DB(txCtx).
			Where("model_instance_id = ?", id).
			Delete(&model.ModelBinding{}).Error; err != nil {
			return fmt.Errorf("删除模型绑定失败: %w", err)
		}
		result := r.client.DB(txCtx).Where("id = ?", id).Delete(&model.ModelInstance{})
		if result.Error != nil {
			return fmt.Errorf("删除模型失败: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return repository.ErrNotFound
		}
		return nil
	})
}

// CountByProvider 统计某厂商下的模型数量。
func (r *ModelInstanceRepository) CountByProvider(ctx context.Context, providerID uint64) (int64, error) {
	var count int64
	if err := r.client.DB(ctx).
		Model(&model.ModelInstance{}).
		Where("provider_id = ?", providerID).
		Count(&count).Error; err != nil {
		return 0, fmt.Errorf("统计厂商模型数量失败: %w", err)
	}
	return count, nil
}

// ModelBindingRepository 是基于 PostgreSQL（GORM）的绑定实现。
type ModelBindingRepository struct {
	client *Client
}

// NewModelBindingRepository 构造绑定仓储。
func NewModelBindingRepository(client *Client) *ModelBindingRepository {
	return &ModelBindingRepository{client: client}
}

// Upsert 写入或覆盖绑定。
func (r *ModelBindingRepository) Upsert(ctx context.Context, binding *model.ModelBinding) error {
	err := r.client.DB(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "scope"}, {Name: "app_id"}, {Name: "dataset_id"}, {Name: "model_type"},
		},
		DoUpdates: clause.AssignmentColumns([]string{"model_instance_id", "updated_at"}),
	}).Create(binding).Error
	if err != nil {
		return fmt.Errorf("保存模型绑定失败: %w", err)
	}
	return nil
}

// Get 查询绑定。
func (r *ModelBindingRepository) Get(
	ctx context.Context, scope model.ModelScope, appID, datasetID uint64, kind model.ModelType,
) (*model.ModelBinding, error) {
	var row model.ModelBinding
	err := r.client.DB(ctx).
		Where("scope = ? AND app_id = ? AND dataset_id = ? AND model_type = ?",
			string(scope), appID, datasetID, string(kind)).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("查询模型绑定失败: %w", err)
	}
	return &row, nil
}

// ListByScope 列出某作用域下的全部绑定。
func (r *ModelBindingRepository) ListByScope(
	ctx context.Context, scope model.ModelScope, appID, datasetID uint64,
) ([]*model.ModelBinding, error) {
	var rows []model.ModelBinding
	if err := r.client.DB(ctx).
		Where("scope = ? AND app_id = ? AND dataset_id = ?", string(scope), appID, datasetID).
		Order("model_type ASC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("查询模型绑定失败: %w", err)
	}
	items := make([]*model.ModelBinding, 0, len(rows))
	for i := range rows {
		items = append(items, &rows[i])
	}
	return items, nil
}

// Delete 删除绑定。
func (r *ModelBindingRepository) Delete(
	ctx context.Context, scope model.ModelScope, appID, datasetID uint64, kind model.ModelType,
) error {
	result := r.client.DB(ctx).
		Where("scope = ? AND app_id = ? AND dataset_id = ? AND model_type = ?",
			string(scope), appID, datasetID, string(kind)).
		Delete(&model.ModelBinding{})
	if result.Error != nil {
		return fmt.Errorf("删除模型绑定失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return repository.ErrNotFound
	}
	return nil
}

// CountByInstance 统计指向某模型的绑定数量。
func (r *ModelBindingRepository) CountByInstance(ctx context.Context, instanceID uint64) (int64, error) {
	var count int64
	if err := r.client.DB(ctx).
		Model(&model.ModelBinding{}).
		Where("model_instance_id = ?", instanceID).
		Count(&count).Error; err != nil {
		return 0, fmt.Errorf("统计模型绑定数量失败: %w", err)
	}
	return count, nil
}
