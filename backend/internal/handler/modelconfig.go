package handler

import (
	"context"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/route"

	"einoflow-backend/internal/engine/model/providers"
	"einoflow-backend/internal/model"
	"einoflow-backend/internal/service"
)

// modelConfigHandler 承载模型配置（厂商 / 模型 / 绑定）的 HTTP 处理。
type modelConfigHandler struct {
	config *service.ModelConfigService
}

// registerModelConfigRoutes 注册模型配置路由。
//
// 统一放在 /model 前缀下：模型配置是全局资源，不属于某个应用或知识库，
// 而「应用 / 知识库级覆盖」通过绑定接口的 scope 参数表达。
func registerModelConfigRoutes(group *route.RouterGroup, config *service.ModelConfigService) {
	h := &modelConfigHandler{config: config}

	group.GET("/model/providers", h.listProviders)
	group.PUT("/model/providers/:provider", h.saveProvider)
	group.DELETE("/model/providers/:provider", h.deleteProvider)
	group.POST("/model/providers/:provider/test", h.testProvider)

	group.GET("/model/instances", h.listModels)
	group.POST("/model/instances", h.createModel)
	group.PUT("/model/instances/:id", h.updateModel)
	group.DELETE("/model/instances/:id", h.deleteModel)
	group.POST("/model/instances/:id/test", h.testModel)

	group.GET("/model/bindings", h.listBindings)
	group.PUT("/model/bindings", h.bindModel)
	group.DELETE("/model/bindings", h.unbindModel)

	group.GET("/model/effective", h.effectiveModel)
}

// providerResponse 是厂商的对外结构。
// 注意：没有任何存放原始凭证的字段——凭证只以掩码形式出现。
type providerResponse struct {
	Provider    string `json:"provider"`
	DisplayName string `json:"displayName"`
	// Description 是厂商用途说明，前端在名称下方展示。
	Description      string                    `json:"description"`
	Kinds            []string                  `json:"kinds"`
	DefaultBaseURL   string                    `json:"defaultBaseUrl"`
	BaseURL          string                    `json:"baseUrl"`
	CredentialFields []credentialFieldResponse `json:"credentialFields"`
	CredentialMask   string                    `json:"credentialMask"`
	Configured       bool                      `json:"configured"`
	Enabled          bool                      `json:"enabled"`
	UpdatedAt        string                    `json:"updatedAt,omitempty"`
}

type credentialFieldResponse struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Required    bool   `json:"required"`
	Secret      bool   `json:"secret"`
	Placeholder string `json:"placeholder,omitempty"`
}

type modelResponse struct {
	ID           uint64 `json:"id"`
	Provider     string `json:"provider"`
	ProviderName string `json:"providerName"`
	ModelName    string `json:"modelName"`
	ModelType    string `json:"modelType"`
	DisplayName  string `json:"displayName"`
	Enabled      bool   `json:"enabled"`
	BindingCount int64  `json:"bindingCount"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
}

type bindingResponse struct {
	Scope        string `json:"scope"`
	AppID        uint64 `json:"appId"`
	DatasetID    uint64 `json:"datasetId"`
	ModelType    string `json:"modelType"`
	InstanceID   uint64 `json:"instanceId"`
	ModelName    string `json:"modelName"`
	Provider     string `json:"provider"`
	ProviderName string `json:"providerName"`
	Enabled      bool   `json:"enabled"`
}

// probeResponse 是连通性测试结果。
type probeResponse struct {
	OK             bool     `json:"ok"`
	Message        string   `json:"message"`
	Models         []string `json:"models"`
	ModelAvailable bool     `json:"modelAvailable"`
	LatencyMS      int      `json:"latencyMs"`
}

type effectiveModelResponse struct {
	// InstanceID 为 0 表示来自配置兜底（数据库里没有任何绑定）。
	InstanceID   uint64 `json:"instanceId"`
	Provider     string `json:"provider"`
	ProviderName string `json:"providerName"`
	ModelName    string `json:"modelName"`
	ModelType    string `json:"modelType"`
	BaseURL      string `json:"baseUrl"`
	Dimension    int    `json:"dimension"`
	Scope        string `json:"scope"`
	// FromConfig 表示当前生效模型来自配置文件而不是数据库配置。
	FromConfig bool `json:"fromConfig"`
}

func newProviderResponse(view service.ProviderView) providerResponse {
	kinds := make([]string, 0, len(view.Kinds))
	for _, kind := range view.Kinds {
		kinds = append(kinds, string(kind))
	}
	fields := make([]credentialFieldResponse, 0, len(view.CredentialFields))
	for _, field := range view.CredentialFields {
		fields = append(fields, credentialFieldResponse{
			Key:         field.Key,
			Label:       field.Label,
			Required:    field.Required,
			Secret:      field.Secret,
			Placeholder: field.Placeholder,
		})
	}
	response := providerResponse{
		Provider:         view.Provider,
		DisplayName:      view.DisplayName,
		Description:      view.Description,
		Kinds:            kinds,
		DefaultBaseURL:   view.DefaultBaseURL,
		BaseURL:          view.BaseURL,
		CredentialFields: fields,
		CredentialMask:   view.CredentialMask,
		Configured:       view.Configured,
		Enabled:          view.Enabled,
	}
	if !view.UpdatedAt.IsZero() {
		response.UpdatedAt = formatTime(view.UpdatedAt)
	}
	return response
}

func newModelResponse(view service.ModelView) modelResponse {
	return modelResponse{
		ID:           view.ID,
		Provider:     view.Provider,
		ProviderName: view.ProviderName,
		ModelName:    view.ModelName,
		ModelType:    string(view.ModelType),
		DisplayName:  view.DisplayName,
		Enabled:      view.Enabled,
		BindingCount: view.BindingCount,
		CreatedAt:    formatTime(view.CreatedAt),
		UpdatedAt:    formatTime(view.UpdatedAt),
	}
}

func newBindingResponse(view service.BindingView) bindingResponse {
	return bindingResponse{
		Scope:        string(view.Scope),
		AppID:        view.AppID,
		DatasetID:    view.DatasetID,
		ModelType:    string(view.ModelType),
		InstanceID:   view.InstanceID,
		ModelName:    view.ModelName,
		Provider:     view.Provider,
		ProviderName: view.ProviderName,
		Enabled:      view.Enabled,
	}
}

func newProbeResponse(result providers.ProbeResult) probeResponse {
	models := result.Models
	if models == nil {
		models = []string{}
	}
	return probeResponse{
		OK:             result.OK,
		Message:        result.Message,
		Models:         models,
		ModelAvailable: result.ModelAvailable,
		LatencyMS:      result.LatencyMS,
	}
}

func (h *modelConfigHandler) listProviders(ctx context.Context, c *app.RequestContext) {
	views, err := h.config.ListProviders(ctx)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	items := make([]providerResponse, 0, len(views))
	for _, view := range views {
		items = append(items, newProviderResponse(view))
	}
	c.JSON(consts.StatusOK, OK(items))
}

type saveProviderRequest struct {
	BaseURL string `json:"baseUrl"`
	// Credentials 为 null 表示不改动已存凭证；传 {} 表示清空。
	Credentials map[string]string `json:"credentials"`
	Enabled     *bool             `json:"enabled"`
}

func (h *modelConfigHandler) saveProvider(ctx context.Context, c *app.RequestContext) {
	var req saveProviderRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(consts.StatusBadRequest, Fail(CodeInvalidParam, "请求体不是合法 JSON"))
		return
	}
	view, err := h.config.SaveProvider(ctx, service.SaveProviderInput{
		Provider:    c.Param("provider"),
		BaseURL:     req.BaseURL,
		Credentials: req.Credentials,
		Enabled:     req.Enabled,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, OK(newProviderResponse(*view)))
}

func (h *modelConfigHandler) deleteProvider(ctx context.Context, c *app.RequestContext) {
	if err := h.config.DeleteProvider(ctx, c.Param("provider")); err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, OK(nil))
}

type testProviderRequest struct {
	BaseURL     string            `json:"baseUrl"`
	ModelName   string            `json:"modelName"`
	Credentials map[string]string `json:"credentials"`
}

func (h *modelConfigHandler) testProvider(ctx context.Context, c *app.RequestContext) {
	var req testProviderRequest
	// 允许空请求体：不带凭证时用已保存的配置测试。
	if len(c.Request.Body()) > 0 {
		if err := c.BindJSON(&req); err != nil {
			c.JSON(consts.StatusBadRequest, Fail(CodeInvalidParam, "请求体不是合法 JSON"))
			return
		}
	}
	result, err := h.config.TestProvider(ctx, service.TestProviderInput{
		Provider:    c.Param("provider"),
		BaseURL:     req.BaseURL,
		ModelName:   req.ModelName,
		Credentials: req.Credentials,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, OK(newProbeResponse(result)))
}

func (h *modelConfigHandler) listModels(ctx context.Context, c *app.RequestContext) {
	views, err := h.config.ListModels(ctx, model.ModelType(c.Query("type")))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	items := make([]modelResponse, 0, len(views))
	for _, view := range views {
		items = append(items, newModelResponse(view))
	}
	c.JSON(consts.StatusOK, OK(items))
}

type createModelRequest struct {
	Provider    string `json:"provider"`
	ModelName   string `json:"modelName"`
	ModelType   string `json:"modelType"`
	DisplayName string `json:"displayName"`
	Enabled     *bool  `json:"enabled"`
}

func (h *modelConfigHandler) createModel(ctx context.Context, c *app.RequestContext) {
	var req createModelRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(consts.StatusBadRequest, Fail(CodeInvalidParam, "请求体不是合法 JSON"))
		return
	}
	view, err := h.config.CreateModel(ctx, service.CreateModelInput{
		Provider:    req.Provider,
		ModelName:   req.ModelName,
		ModelType:   model.ModelType(req.ModelType),
		DisplayName: req.DisplayName,
		Enabled:     req.Enabled,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusCreated, OK(newModelResponse(*view)))
}

type updateModelRequest struct {
	DisplayName *string `json:"displayName"`
	Enabled     *bool   `json:"enabled"`
}

func (h *modelConfigHandler) updateModel(ctx context.Context, c *app.RequestContext) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	var req updateModelRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(consts.StatusBadRequest, Fail(CodeInvalidParam, "请求体不是合法 JSON"))
		return
	}
	view, err := h.config.UpdateModel(ctx, service.UpdateModelInput{
		ID:          id,
		DisplayName: req.DisplayName,
		Enabled:     req.Enabled,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, OK(newModelResponse(*view)))
}

func (h *modelConfigHandler) deleteModel(ctx context.Context, c *app.RequestContext) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	if err := h.config.DeleteModel(ctx, id); err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, OK(nil))
}

func (h *modelConfigHandler) testModel(ctx context.Context, c *app.RequestContext) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	result, err := h.config.TestModel(ctx, id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, OK(newProbeResponse(result)))
}

func (h *modelConfigHandler) listBindings(ctx context.Context, c *app.RequestContext) {
	appID, _ := strconv.ParseUint(c.Query("appId"), 10, 64)
	datasetID, _ := strconv.ParseUint(c.Query("datasetId"), 10, 64)
	views, err := h.config.ListBindings(ctx, model.ModelScope(c.Query("scope")), appID, datasetID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	items := make([]bindingResponse, 0, len(views))
	for _, view := range views {
		items = append(items, newBindingResponse(view))
	}
	c.JSON(consts.StatusOK, OK(items))
}

type bindModelRequest struct {
	Scope           string `json:"scope"`
	AppID           uint64 `json:"appId"`
	DatasetID       uint64 `json:"datasetId"`
	ModelType       string `json:"modelType"`
	ModelInstanceID uint64 `json:"modelInstanceId"`
}

func (h *modelConfigHandler) bindModel(ctx context.Context, c *app.RequestContext) {
	var req bindModelRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(consts.StatusBadRequest, Fail(CodeInvalidParam, "请求体不是合法 JSON"))
		return
	}
	if err := h.config.BindModel(ctx, service.BindModelInput{
		Scope:           model.ModelScope(req.Scope),
		AppID:           req.AppID,
		DatasetID:       req.DatasetID,
		ModelType:       model.ModelType(req.ModelType),
		ModelInstanceID: req.ModelInstanceID,
	}); err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, OK(nil))
}

func (h *modelConfigHandler) unbindModel(ctx context.Context, c *app.RequestContext) {
	appID, _ := strconv.ParseUint(c.Query("appId"), 10, 64)
	datasetID, _ := strconv.ParseUint(c.Query("datasetId"), 10, 64)
	if err := h.config.UnbindModel(
		ctx,
		model.ModelScope(c.Query("scope")),
		appID,
		datasetID,
		model.ModelType(c.Query("modelType")),
	); err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, OK(nil))
}

func (h *modelConfigHandler) effectiveModel(ctx context.Context, c *app.RequestContext) {
	appID, _ := strconv.ParseUint(c.Query("appId"), 10, 64)
	datasetID, _ := strconv.ParseUint(c.Query("datasetId"), 10, 64)
	// 默认查向量化模型：它是当前唯一被检索链路真正消费的模型类型。
	kind := model.ModelType(c.Query("type"))
	if kind == "" {
		kind = model.ModelTypeEmbedding
	}
	effective, err := h.config.EffectiveModel(ctx, kind, appID, datasetID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, OK(effectiveModelResponse{
		InstanceID:   effective.InstanceID,
		Provider:     effective.Provider,
		ProviderName: effective.ProviderName,
		ModelName:    effective.ModelName,
		ModelType:    string(effective.ModelType),
		BaseURL:      effective.BaseURL,
		Dimension:    effective.Dimension,
		Scope:        string(effective.Scope),
		FromConfig:   effective.InstanceID == 0,
	}))
}
