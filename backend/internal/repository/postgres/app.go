package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"einoflow-backend/internal/model"
	"einoflow-backend/internal/repository"
)

// AppRepository 是基于 PostgreSQL（GORM）的实现。
type AppRepository struct {
	client *Client
}

// NewAppRepository 构造应用仓储。
func NewAppRepository(client *Client) *AppRepository {
	return &AppRepository{client: client}
}

// Create 写入新应用。
// GORM 会自动回填 ID 与 created_at / updated_at，因此不需要再回读一次。
func (r *AppRepository) Create(ctx context.Context, app *model.App) error {
	if err := r.client.DB(ctx).Create(app).Error; err != nil {
		return fmt.Errorf("创建应用失败: %w", err)
	}
	return nil
}

// GetByID 按主键查询；不存在或已软删除时返回 repository.ErrNotFound。
// 已软删除的行由 gorm.DeletedAt 自动过滤，不必手写 deleted_at IS NULL。
func (r *AppRepository) GetByID(ctx context.Context, id uint64) (*model.App, error) {
	var app model.App
	if err := r.client.DB(ctx).Where("id = ?", id).First(&app).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("查询应用失败: %w", err)
	}
	return &app, nil
}

// List 按条件分页查询，并返回满足条件的总数。
func (r *AppRepository) List(ctx context.Context, filter repository.AppListFilter) ([]*model.App, int64, error) {
	// 用闭包各构造一次查询：GORM 的链式调用会复用同一个语句对象，
	// 拿同一个对象分别做 Count 和 Find 容易互相污染条件。
	build := func() *gorm.DB {
		query := r.client.DB(ctx).Model(&model.App{})
		if filter.Mode != "" {
			query = query.Where("mode = ?", string(filter.Mode))
		}
		if filter.Status != "" {
			query = query.Where("status = ?", string(filter.Status))
		}
		if filter.Keyword != "" {
			// 用 ILIKE 而不是 LIKE：PostgreSQL 的 LIKE 区分大小写，
			// ILIKE 才与 MySQL 默认的排序规则行为一致（关键词搜索对用户更友好）。
			query = query.Where("name ILIKE ?", "%"+escapeLike(filter.Keyword)+"%")
		}
		return query
	}

	var total int64
	if err := build().Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("统计应用数量失败: %w", err)
	}
	if total == 0 {
		// 提前返回空切片而不是 nil，调用方序列化出来是 [] 而不是 null。
		return []*model.App{}, 0, nil
	}

	var rows []model.App
	if err := build().Order("id DESC").Limit(filter.Limit).Offset(filter.Offset).Find(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("查询应用列表失败: %w", err)
	}

	apps := make([]*model.App, 0, len(rows))
	for i := range rows {
		apps = append(apps, &rows[i])
	}
	return apps, total, nil
}

// Update 更新应用；记录不存在时返回 repository.ErrNotFound。
func (r *AppRepository) Update(ctx context.Context, app *model.App) error {
	// 用 Select 显式指定要更新的列：GORM 的 Updates(struct) 会跳过零值字段，
	// 会让「把描述改成空字符串」这类操作静默失效。这是用 ORM 最容易踩的坑。
	result := r.client.DB(ctx).
		Model(&model.App{}).
		Where("id = ?", app.ID).
		Select("name", "description", "status", "icon").
		Updates(app)
	if result.Error != nil {
		return fmt.Errorf("更新应用失败: %w", result.Error)
	}

	// 回读一次拿到数据库更新的 updated_at；同时天然覆盖「记录不存在」的情况。
	updated, err := r.GetByID(ctx, app.ID)
	if err != nil {
		return err
	}
	*app = *updated
	return nil
}

// SoftDelete 软删除应用。
// 模型带 gorm.DeletedAt 时，Delete 会被翻译成写 deleted_at 的 UPDATE。
func (r *AppRepository) SoftDelete(ctx context.Context, id uint64) error {
	result := r.client.DB(ctx).Where("id = ?", id).Delete(&model.App{})
	if result.Error != nil {
		return fmt.Errorf("删除应用失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		// 已删除的行会被自动过滤，所以重复删除同样落到这里 → 返回 NotFound，与之前行为一致。
		return repository.ErrNotFound
	}
	return nil
}

// escapeLike 转义 LIKE / ILIKE 的通配符，避免用户输入里的 % 和 _ 被当成通配符。
// PostgreSQL 默认用反斜杠作为转义符，因此这里用反斜杠转义。
func escapeLike(keyword string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(keyword)
}
