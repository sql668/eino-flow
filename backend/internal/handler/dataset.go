package handler

import (
	"context"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/route"

	"einoflow-backend/internal/model"
	"einoflow-backend/internal/service"
)

// datasetHandler 承载知识库管理的 HTTP 处理。
type datasetHandler struct {
	datasets *service.DatasetService
}

// registerDatasetRoutes 注册 /api/v1 下的知识库路由。
func registerDatasetRoutes(group *route.RouterGroup, datasets *service.DatasetService) {
	h := &datasetHandler{datasets: datasets}
	group.POST("/datasets", h.create)
	group.GET("/datasets", h.list)
	group.GET("/datasets/:id", h.get)
	group.PUT("/datasets/:id", h.update)
	group.DELETE("/datasets/:id", h.delete)
}

type createDatasetRequest struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	EmbeddingModel string `json:"embeddingModel"`
}

type updateDatasetRequest struct {
	Name           *string `json:"name"`
	Description    *string `json:"description"`
	EmbeddingModel *string `json:"embeddingModel"`
	Status         *string `json:"status"`
}

type datasetResponse struct {
	ID             uint64 `json:"id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	EmbeddingModel string `json:"embeddingModel"`
	Status         string `json:"status"`
	CreatedAt      string `json:"createdAt"`
	UpdatedAt      string `json:"updatedAt"`
}

type datasetListResponse struct {
	Items    []datasetResponse `json:"items"`
	Total    int64             `json:"total"`
	Page     int               `json:"page"`
	PageSize int               `json:"pageSize"`
}

func newDatasetResponse(d *model.Dataset) datasetResponse {
	return datasetResponse{
		ID:             d.ID,
		Name:           d.Name,
		Description:    d.Description,
		EmbeddingModel: d.EmbeddingModel,
		Status:         string(d.Status),
		CreatedAt:      formatTime(d.CreatedAt),
		UpdatedAt:      formatTime(d.UpdatedAt),
	}
}

func (h *datasetHandler) create(ctx context.Context, c *app.RequestContext) {
	var req createDatasetRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(consts.StatusBadRequest, Fail(CodeInvalidParam, "请求体不是合法 JSON"))
		return
	}

	created, err := h.datasets.Create(ctx, service.CreateDatasetInput{
		Name:           req.Name,
		Description:    req.Description,
		EmbeddingModel: req.EmbeddingModel,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusCreated, OK(newDatasetResponse(created)))
}

func (h *datasetHandler) list(ctx context.Context, c *app.RequestContext) {
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("pageSize"))

	result, err := h.datasets.List(ctx, service.ListDatasetsInput{
		Keyword:  c.Query("keyword"),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}

	items := make([]datasetResponse, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, newDatasetResponse(item))
	}
	c.JSON(consts.StatusOK, OK(datasetListResponse{
		Items:    items,
		Total:    result.Total,
		Page:     result.Page,
		PageSize: result.PageSize,
	}))
}

func (h *datasetHandler) get(ctx context.Context, c *app.RequestContext) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}

	dataset, err := h.datasets.Get(ctx, id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, OK(newDatasetResponse(dataset)))
}

func (h *datasetHandler) update(ctx context.Context, c *app.RequestContext) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}

	var req updateDatasetRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(consts.StatusBadRequest, Fail(CodeInvalidParam, "请求体不是合法 JSON"))
		return
	}

	in := service.UpdateDatasetInput{
		Name:           req.Name,
		Description:    req.Description,
		EmbeddingModel: req.EmbeddingModel,
	}
	if req.Status != nil {
		status := model.DatasetStatus(*req.Status)
		in.Status = &status
	}

	updated, err := h.datasets.Update(ctx, id, in)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, OK(newDatasetResponse(updated)))
}

func (h *datasetHandler) delete(ctx context.Context, c *app.RequestContext) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}

	if err := h.datasets.Delete(ctx, id); err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, OK(nil))
}
