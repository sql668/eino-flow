package repository

import (
	"context"

	"einoflow-backend/internal/model"
)

// ModelProviderRepository 描述厂商凭证配置的持久化能力。
type ModelProviderRepository interface {
	// Upsert 按厂商标识写入或覆盖配置（一个厂商只有一条）。
	Upsert(ctx context.Context, provider *model.ModelProvider) error
	// GetByProvider 按厂商标识查询；不存在返回 ErrNotFound。
	GetByProvider(ctx context.Context, provider string) (*model.ModelProvider, error)
	// GetByID 按主键查询；不存在返回 ErrNotFound。
	GetByID(ctx context.Context, id uint64) (*model.ModelProvider, error)
	// List 返回全部厂商配置（数量很少，不分页）。
	List(ctx context.Context) ([]*model.ModelProvider, error)
	// Delete 删除厂商配置。调用方必须先确认它下面没有模型，
	// 否则会留下「模型还在但凭证没了」的悬空引用。
	Delete(ctx context.Context, provider string) error
}

// ModelInstanceFilter 是模型列表的查询条件。零值表示不过滤。
type ModelInstanceFilter struct {
	ModelType  model.ModelType
	ProviderID uint64
	Enabled    *bool
}

// ModelInstanceRepository 描述模型清单的持久化能力。
type ModelInstanceRepository interface {
	Create(ctx context.Context, instance *model.ModelInstance) error
	GetByID(ctx context.Context, id uint64) (*model.ModelInstance, error)
	List(ctx context.Context, filter ModelInstanceFilter) ([]*model.ModelInstance, error)
	// Update 更新模型；不存在返回 ErrNotFound。
	Update(ctx context.Context, instance *model.ModelInstance) error
	// Delete 删除模型，并**同时删除指向它的全部绑定**（同一事务）。
	// 级联放在仓储层而不是让 service 分两步：分两步就会出现「删了模型、绑定还在」的中间态。
	Delete(ctx context.Context, id uint64) error
	// CountByProvider 统计某厂商下的模型数量，用于阻止删除仍有模型的厂商。
	CountByProvider(ctx context.Context, providerID uint64) (int64, error)
}

// ModelBindingRepository 描述模型绑定的持久化能力。
type ModelBindingRepository interface {
	// Upsert 按（作用域, 应用, 知识库, 类型）写入或覆盖绑定。
	Upsert(ctx context.Context, binding *model.ModelBinding) error
	// Get 查询绑定；不存在返回 ErrNotFound。
	Get(ctx context.Context, scope model.ModelScope, appID, datasetID uint64, kind model.ModelType) (*model.ModelBinding, error)
	// ListByScope 列出某作用域下的全部绑定（用于配置页展示）。
	ListByScope(ctx context.Context, scope model.ModelScope, appID, datasetID uint64) ([]*model.ModelBinding, error)
	// Delete 删除绑定；不存在返回 ErrNotFound。
	Delete(ctx context.Context, scope model.ModelScope, appID, datasetID uint64, kind model.ModelType) error
	// CountByInstance 统计指向某模型的绑定数量，用于删除前的风险提示。
	CountByInstance(ctx context.Context, instanceID uint64) (int64, error)
}
