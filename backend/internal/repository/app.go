package repository

import (
	"context"

	"einoflow-backend/internal/model"
)

// AppListFilter 是应用列表的查询条件。
// 零值表示不过滤，由各实现自行拼装 SQL，避免为每种组合单独写一个方法。
type AppListFilter struct {
	Mode    model.AppMode
	Status  model.AppStatus
	Keyword string // 按名称模糊匹配
	Offset  int
	Limit   int
}

// AppRepository 描述 AI 应用的持久化能力。
type AppRepository interface {
	// Create 写入新应用，成功后回填 app 的 ID 与时间戳。
	Create(ctx context.Context, app *model.App) error
	// GetByID 按主键查询；不存在时返回 ErrNotFound。
	GetByID(ctx context.Context, id uint64) (*model.App, error)
	// List 按条件分页查询，同时返回满足条件的总数。
	List(ctx context.Context, filter AppListFilter) ([]*model.App, int64, error)
	// Update 更新应用；不存在时返回 ErrNotFound。
	Update(ctx context.Context, app *model.App) error
	// SoftDelete 软删除应用（写 deleted_at）；不存在时返回 ErrNotFound。
	SoftDelete(ctx context.Context, id uint64) error
}
