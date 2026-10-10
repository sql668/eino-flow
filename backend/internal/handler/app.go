package handler

import (
	"context"
	"errors"
	"log"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/route"

	"einoflow-backend/internal/model"
	"einoflow-backend/internal/service"
)

// appHandler 承载应用管理的 HTTP 处理。
// 只做参数解析、响应组装与错误映射，业务规则一律在 service 层。
type appHandler struct {
	apps *service.AppService
}

// registerAppRoutes 注册 /api/v1 下的应用管理路由。
func registerAppRoutes(group *route.RouterGroup, apps *service.AppService) {
	h := &appHandler{apps: apps}
	group.POST("/apps", h.create)
	group.GET("/apps", h.list)
	group.GET("/apps/:id", h.get)
	group.PUT("/apps/:id", h.update)
	group.DELETE("/apps/:id", h.delete)
}

// ---------- 请求 / 响应结构 ----------

type createAppRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Mode        string `json:"mode"`
	Icon        string `json:"icon"`
}

// updateAppRequest 用指针字段，以便区分「未传该字段」和「显式传空值」。
type updateAppRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Status      *string `json:"status"`
	Icon        *string `json:"icon"`
}

// appResponse 是应用的对外结构，刻意不暴露 deleted_at 等内部字段。
type appResponse struct {
	ID          uint64 `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Mode        string `json:"mode"`
	Status      string `json:"status"`
	Icon        string `json:"icon"`
	// 时间统一输出 RFC3339（UTC），由前端转成本地时区展示。
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type appListResponse struct {
	Items    []appResponse `json:"items"`
	Total    int64         `json:"total"`
	Page     int           `json:"page"`
	PageSize int           `json:"pageSize"`
}

func newAppResponse(a *model.App) appResponse {
	return appResponse{
		ID:          a.ID,
		Name:        a.Name,
		Description: a.Description,
		Mode:        string(a.Mode),
		Status:      string(a.Status),
		Icon:        a.Icon,
		CreatedAt:   formatTime(a.CreatedAt),
		UpdatedAt:   formatTime(a.UpdatedAt),
	}
}

// ---------- 处理函数 ----------

func (h *appHandler) create(ctx context.Context, c *app.RequestContext) {
	var req createAppRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(consts.StatusBadRequest, Fail(CodeInvalidParam, "请求体不是合法 JSON"))
		return
	}

	created, err := h.apps.Create(ctx, service.CreateAppInput{
		Name:        req.Name,
		Description: req.Description,
		Mode:        model.AppMode(req.Mode),
		Icon:        req.Icon,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusCreated, OK(newAppResponse(created)))
}

func (h *appHandler) list(ctx context.Context, c *app.RequestContext) {
	// 解析失败一律按缺省处理，合法区间由 service 归一化，避免在 handler 里重复校验。
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("pageSize"))

	result, err := h.apps.List(ctx, service.ListAppsInput{
		Mode:     model.AppMode(c.Query("mode")),
		Status:   model.AppStatus(c.Query("status")),
		Keyword:  c.Query("keyword"),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}

	items := make([]appResponse, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, newAppResponse(item))
	}
	c.JSON(consts.StatusOK, OK(appListResponse{
		Items:    items,
		Total:    result.Total,
		Page:     result.Page,
		PageSize: result.PageSize,
	}))
}

func (h *appHandler) get(ctx context.Context, c *app.RequestContext) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}

	appItem, err := h.apps.Get(ctx, id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, OK(newAppResponse(appItem)))
}

func (h *appHandler) update(ctx context.Context, c *app.RequestContext) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}

	var req updateAppRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(consts.StatusBadRequest, Fail(CodeInvalidParam, "请求体不是合法 JSON"))
		return
	}

	in := service.UpdateAppInput{
		Name:        req.Name,
		Description: req.Description,
		Icon:        req.Icon,
	}
	if req.Status != nil {
		status := model.AppStatus(*req.Status)
		in.Status = &status
	}

	updated, err := h.apps.Update(ctx, id, in)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, OK(newAppResponse(updated)))
}

func (h *appHandler) delete(ctx context.Context, c *app.RequestContext) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}

	if err := h.apps.Delete(ctx, id); err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, OK(nil))
}

// ---------- 公共工具 ----------

// parseIDParam 解析路径中的 :id；失败时已写好响应，调用方直接返回即可。
func parseIDParam(c *app.RequestContext) (uint64, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(consts.StatusBadRequest, Fail(CodeInvalidParam, "id 必须是正整数"))
		return 0, false
	}
	return id, true
}

// writeServiceError 把 service 层错误映射成 HTTP 响应。
func writeServiceError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidInput):
		// 这类错误描述的是用户的输入，可以原样返回，便于调用方定位。
		c.JSON(consts.StatusBadRequest, Fail(CodeInvalidParam, err.Error()))
	case errors.Is(err, service.ErrNotFound):
		c.JSON(consts.StatusNotFound, Fail(CodeNotFound, err.Error()))
	default:
		// 未预期的错误只记日志，对外返回泛化信息，避免泄漏内部实现细节。
		log.Printf("处理请求失败: %v", err)
		c.JSON(consts.StatusInternalServerError, Fail(CodeInternal, "服务内部错误"))
	}
}
