package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"einoflow-backend/internal/model"
	"einoflow-backend/internal/repository"
)

// 领域错误。handler 用 errors.Is 判断后映射成对应的 HTTP 状态码，
// 这样 handler 不必了解 repository 层的错误类型。
var (
	// ErrInvalidInput 表示入参不合法。
	ErrInvalidInput = errors.New("入参不合法")
	// ErrNotFound 表示资源不存在。
	ErrNotFound = errors.New("资源不存在")
	// ErrRateLimited 表示请求过于频繁（例如召回测试）。
	ErrRateLimited = errors.New("请求过于频繁")
)

// 分页约束：兜住非法入参，并防止 pageSize 过大把数据库拖垮。
const (
	defaultPageSize = 20
	maxPageSize     = 100
	maxAppNameRunes = 128
)

// CreateAppInput 是创建应用的入参。
type CreateAppInput struct {
	Name        string
	Description string
	Mode        model.AppMode
	Icon        string
}

// UpdateAppInput 是更新应用的入参。
// 字段用指针区分「未传」与「显式传空」——否则部分更新会把没传的字段覆盖成空值。
type UpdateAppInput struct {
	Name        *string
	Description *string
	Status      *model.AppStatus
	Icon        *string
}

// ListAppsInput 是列表查询入参。
type ListAppsInput struct {
	Mode     model.AppMode
	Status   model.AppStatus
	Keyword  string
	Page     int
	PageSize int
}

// AppListResult 是列表查询结果。
type AppListResult struct {
	Items    []*model.App
	Total    int64
	Page     int
	PageSize int
}

// AppService 承载应用管理的业务规则。
type AppService struct {
	apps repository.AppRepository
}

// NewAppService 构造应用服务。
func NewAppService(apps repository.AppRepository) *AppService {
	return &AppService{apps: apps}
}

// Create 创建应用。
func (s *AppService) Create(ctx context.Context, in CreateAppInput) (*model.App, error) {
	name, err := validateRequiredName(in.Name, "应用名称", maxAppNameRunes)
	if err != nil {
		return nil, err
	}
	if !in.Mode.IsValid() {
		return nil, fmt.Errorf("%w: 不支持的应用形态 %q", ErrInvalidInput, in.Mode)
	}

	app := &model.App{
		Name:        name,
		Description: strings.TrimSpace(in.Description),
		Mode:        in.Mode,
		Status:      model.AppStatusDraft, // 新建一律为草稿，发布走独立动作
		Icon:        in.Icon,
	}
	if err := s.apps.Create(ctx, app); err != nil {
		return nil, err
	}
	return app, nil
}

// Get 查询单个应用。
func (s *AppService) Get(ctx context.Context, id uint64) (*model.App, error) {
	app, err := s.apps.GetByID(ctx, id)
	if err != nil {
		return nil, translateError(err)
	}
	return app, nil
}

// List 分页查询应用。
func (s *AppService) List(ctx context.Context, in ListAppsInput) (*AppListResult, error) {
	page, pageSize := normalizePaging(in.Page, in.PageSize)

	apps, total, err := s.apps.List(ctx, repository.AppListFilter{
		Mode:    in.Mode,
		Status:  in.Status,
		Keyword: strings.TrimSpace(in.Keyword),
		Offset:  (page - 1) * pageSize,
		Limit:   pageSize,
	})
	if err != nil {
		return nil, err
	}
	return &AppListResult{Items: apps, Total: total, Page: page, PageSize: pageSize}, nil
}

// Update 更新应用；未传的字段保持原值。
func (s *AppService) Update(ctx context.Context, id uint64, in UpdateAppInput) (*model.App, error) {
	// 先读后改：既能拿到当前值做局部更新，也能自然地对不存在的记录返回 ErrNotFound。
	app, err := s.apps.GetByID(ctx, id)
	if err != nil {
		return nil, translateError(err)
	}

	if in.Name != nil {
		name, err := validateRequiredName(*in.Name, "应用名称", maxAppNameRunes)
		if err != nil {
			return nil, err
		}
		app.Name = name
	}
	if in.Description != nil {
		app.Description = strings.TrimSpace(*in.Description)
	}
	if in.Icon != nil {
		app.Icon = *in.Icon
	}
	if in.Status != nil {
		if !in.Status.IsValid() {
			return nil, fmt.Errorf("%w: 不支持的状态 %q", ErrInvalidInput, *in.Status)
		}
		app.Status = *in.Status
	}

	if err := s.apps.Update(ctx, app); err != nil {
		return nil, translateError(err)
	}
	return app, nil
}

// Delete 软删除应用。
func (s *AppService) Delete(ctx context.Context, id uint64) error {
	return translateError(s.apps.SoftDelete(ctx, id))
}

// normalizePaging 归一化分页参数。
func normalizePaging(page, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	return page, pageSize
}

// translateError 把 repository 层错误翻译成 service 层错误，
// 避免上层被迫理解存储实现细节。
func translateError(err error) error {
	if errors.Is(err, repository.ErrNotFound) {
		return ErrNotFound
	}
	return err
}
