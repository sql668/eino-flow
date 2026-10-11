package handler

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/route"

	"einoflow-backend/internal/model"
	"einoflow-backend/internal/service"
)

// 分段工作台的 HTTP 入口：可选项、预览、入库、重切，以及知识库级配置。

type chunkWorkbenchHandler struct {
	documents *service.DocumentService
}

// registerChunkWorkbenchRoutes 注册分段工作台相关路由。
//
// 预览走 POST 而不是 GET：配置是一棵嵌套结构（清洗规则是数组），
// 塞进 query string 既难读又有长度限制。
func registerChunkWorkbenchRoutes(group *route.RouterGroup, documents *service.DocumentService) {
	h := &chunkWorkbenchHandler{documents: documents}
	group.GET("/datasets/:id/chunk-options", h.options)
	group.GET("/datasets/:id/chunk-config", h.getChunkConfig)
	group.PUT("/datasets/:id/chunk-config", h.saveChunkConfig)
	group.GET("/datasets/:id/retrieval-config", h.getRetrievalConfig)
	group.PUT("/datasets/:id/retrieval-config", h.saveRetrievalConfig)

	group.POST("/documents/:id/preview", h.preview)
	group.POST("/documents/:id/ingest", h.ingest)
	group.POST("/documents/:id/rechunk", h.rechunk)
	group.POST("/documents/:id/parse", h.reparse)
}

// ---------- 可选项 ----------

type chunkModeOptionResponse struct {
	Mode        string `json:"mode"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Implemented bool   `json:"implemented"`
}

type cleaningRuleResponse struct {
	Key          string `json:"key"`
	Label        string `json:"label"`
	Description  string `json:"description"`
	NeedsPattern bool   `json:"needsPattern"`
	Risk         string `json:"risk,omitempty"`
}

type chunkLimitsResponse struct {
	MinMaxRunes       int `json:"minMaxRunes"`
	MaxMaxRunes       int `json:"maxMaxRunes"`
	MaxMinRunes       int `json:"maxMinRunes"`
	DefaultMaxRunes   int `json:"defaultMaxRunes"`
	DefaultMinRunes   int `json:"defaultMinRunes"`
	MaxOverlapPercent int `json:"maxOverlapPercent"`
}

type chunkOptionsResponse struct {
	Modes         []chunkModeOptionResponse `json:"modes"`
	CleaningRules []cleaningRuleResponse    `json:"cleaningRules"`
	Defaults      chunkConfigResponse       `json:"defaults"`
	Limits        chunkLimitsResponse       `json:"limits"`
}

// chunkConfigResponse 是分段配置的对外结构。
type chunkConfigResponse struct {
	Mode         string                `json:"mode"`
	Separator    string                `json:"separator"`
	MaxRunes     int                   `json:"maxRunes"`
	OverlapRunes int                   `json:"overlapRunes"`
	MinRunes     int                   `json:"minRunes"`
	KeepNoise    bool                  `json:"keepNoise"`
	Cleaning     []cleaningRuleRequest `json:"cleaning"`
	// Version 是配置指纹，入库后写进分段；前端展示它便于识别「这批片段是哪套参数切的」。
	Version string `json:"version,omitempty"`
}

type cleaningRuleRequest struct {
	Key     string `json:"key"`
	Pattern string `json:"pattern,omitempty"`
}

func newChunkConfigResponse(config model.ChunkConfig) chunkConfigResponse {
	rules := make([]cleaningRuleRequest, 0, len(config.Cleaning))
	for _, rule := range config.Cleaning {
		rules = append(rules, cleaningRuleRequest{Key: rule.Key, Pattern: rule.Pattern})
	}
	return chunkConfigResponse{
		Mode:         string(config.Mode),
		Separator:    config.Separator,
		MaxRunes:     config.MaxRunes,
		OverlapRunes: config.OverlapRunes,
		MinRunes:     config.MinRunes,
		KeepNoise:    config.KeepNoise,
		Cleaning:     rules,
		Version:      config.Version(),
	}
}

func (r chunkConfigResponse) toModel() model.ChunkConfig {
	rules := make([]model.CleaningRuleConfig, 0, len(r.Cleaning))
	for _, rule := range r.Cleaning {
		rules = append(rules, model.CleaningRuleConfig{Key: rule.Key, Pattern: rule.Pattern})
	}
	return model.ChunkConfig{
		Mode:         model.ChunkMode(r.Mode),
		Separator:    r.Separator,
		MaxRunes:     r.MaxRunes,
		OverlapRunes: r.OverlapRunes,
		MinRunes:     r.MinRunes,
		KeepNoise:    r.KeepNoise,
		Cleaning:     rules,
	}
}

func (h *chunkWorkbenchHandler) options(ctx context.Context, c *app.RequestContext) {
	datasetID, ok := parseIDParam(c)
	if !ok {
		return
	}
	options, err := h.documents.ChunkOptionsFor(ctx, datasetID)
	if err != nil {
		writeServiceError(c, err)
		return
	}

	modes := make([]chunkModeOptionResponse, 0, len(options.Modes))
	for _, mode := range options.Modes {
		modes = append(modes, chunkModeOptionResponse{
			Mode:        string(mode.Mode),
			Label:       mode.Label,
			Description: mode.Description,
			Implemented: mode.Implemented,
		})
	}
	rules := make([]cleaningRuleResponse, 0, len(options.CleaningRules))
	for _, rule := range options.CleaningRules {
		rules = append(rules, cleaningRuleResponse{
			Key:          rule.Key,
			Label:        rule.Label,
			Description:  rule.Description,
			NeedsPattern: rule.NeedsPattern,
			Risk:         rule.Risk,
		})
	}

	c.JSON(consts.StatusOK, OK(chunkOptionsResponse{
		Modes:         modes,
		CleaningRules: rules,
		Defaults:      newChunkConfigResponse(options.Defaults),
		Limits: chunkLimitsResponse{
			MinMaxRunes:       options.Limits.MinMaxRunes,
			MaxMaxRunes:       options.Limits.MaxMaxRunes,
			MaxMinRunes:       options.Limits.MaxMinRunes,
			DefaultMaxRunes:   options.Limits.DefaultMaxRunes,
			DefaultMinRunes:   options.Limits.DefaultMinRunes,
			MaxOverlapPercent: options.Limits.MaxOverlapPercent,
		},
	}))
}

func (h *chunkWorkbenchHandler) getChunkConfig(ctx context.Context, c *app.RequestContext) {
	datasetID, ok := parseIDParam(c)
	if !ok {
		return
	}
	config, err := h.documents.GetChunkConfig(ctx, datasetID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, OK(newChunkConfigResponse(config)))
}

type saveChunkConfigRequest struct {
	Config chunkConfigResponse `json:"config"`
}

func (h *chunkWorkbenchHandler) saveChunkConfig(ctx context.Context, c *app.RequestContext) {
	datasetID, ok := parseIDParam(c)
	if !ok {
		return
	}
	var req saveChunkConfigRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(consts.StatusBadRequest, Fail(CodeInvalidParam, "请求体不是合法 JSON"))
		return
	}
	saved, err := h.documents.SaveChunkConfig(ctx, datasetID, req.Config.toModel())
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, OK(newChunkConfigResponse(saved)))
}

// ---------- 预览与入库 ----------

type previewRequest struct {
	Config chunkConfigResponse `json:"config"`
}

type cleaningRuleResultResponse struct {
	Key          string   `json:"key"`
	Label        string   `json:"label"`
	Hits         int      `json:"hits"`
	RemovedRunes int      `json:"removedRunes"`
	Note         string   `json:"note,omitempty"`
	Samples      []string `json:"samples,omitempty"`
}

type previewChunkResponse struct {
	Index        int      `json:"index"`
	Kind         string   `json:"kind"`
	TitlePath    []string `json:"titlePath"`
	Page         int      `json:"page"`
	Locator      string   `json:"locator"`
	CharCount    int      `json:"charCount"`
	OverlapRunes int      `json:"overlapRunes"`
	Content      string   `json:"content"`
	OriginalText string   `json:"originalText"`
}

type previewStatsResponse struct {
	Total         int `json:"total"`
	TotalRunes    int `json:"totalRunes"`
	AvgRunes      int `json:"avgRunes"`
	MaxChunkRunes int `json:"maxChunkRunes"`
	AtLimit       int `json:"atLimit"`
	TooShort      int `json:"tooShort"`
	Tables        int `json:"tables"`
	OverlapRunes  int `json:"overlapRunes"`
	DroppedNoise  int `json:"droppedNoise"`
}

type previewResponse struct {
	Chunks        []previewChunkResponse       `json:"chunks"`
	Total         int                          `json:"total"`
	Returned      int                          `json:"returned"`
	Cleaning      []cleaningRuleResultResponse `json:"cleaning"`
	CleaningStats struct {
		RemovedRunes int `json:"removedRunes"`
		BeforeRunes  int `json:"beforeRunes"`
		AfterRunes   int `json:"afterRunes"`
	} `json:"cleaningStats"`
	Stats         previewStatsResponse `json:"stats"`
	Hints         []string             `json:"hints"`
	ConfigVersion string               `json:"configVersion"`
	Meta          struct {
		Parser     string `json:"parser"`
		ParseMode  string `json:"parseMode"`
		PageCount  int    `json:"pageCount"`
		SourceName string `json:"sourceName"`
	} `json:"meta"`
}

func (h *chunkWorkbenchHandler) preview(ctx context.Context, c *app.RequestContext) {
	documentID, ok := parseIDParam(c)
	if !ok {
		return
	}
	var req previewRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(consts.StatusBadRequest, Fail(CodeInvalidParam, "请求体不是合法 JSON"))
		return
	}

	result, err := h.documents.PreviewChunks(ctx, documentID, req.Config.toModel())
	if err != nil {
		writeServiceError(c, err)
		return
	}

	rules := make([]cleaningRuleResultResponse, 0, len(result.Cleaning.Rules))
	for _, rule := range result.Cleaning.Rules {
		rules = append(rules, cleaningRuleResultResponse{
			Key:          rule.Key,
			Label:        rule.Label,
			Hits:         rule.Hits,
			RemovedRunes: rule.RemovedRunes,
			Note:         rule.Note,
			Samples:      rule.Samples,
		})
	}
	chunks := make([]previewChunkResponse, 0, len(result.Chunks))
	for _, chunk := range result.Chunks {
		titlePath := chunk.TitlePath
		if titlePath == nil {
			titlePath = []string{}
		}
		chunks = append(chunks, previewChunkResponse{
			Index:        chunk.Index,
			Kind:         chunk.Kind,
			TitlePath:    titlePath,
			Page:         chunk.Page,
			Locator:      chunk.Locator,
			CharCount:    chunk.CharCount,
			OverlapRunes: chunk.OverlapRunes,
			Content:      chunk.Content,
			OriginalText: chunk.OriginalText,
		})
	}

	response := previewResponse{
		Chunks:        chunks,
		Total:         result.Total,
		Returned:      len(chunks),
		Cleaning:      rules,
		ConfigVersion: result.ConfigVersion,
		Hints:         result.Hints,
		Stats: previewStatsResponse{
			Total:         result.Stats.Total,
			TotalRunes:    result.Stats.TotalRunes,
			AvgRunes:      result.Stats.AvgRunes,
			MaxChunkRunes: result.Stats.MaxChunkRunes,
			AtLimit:       result.Stats.AtLimit,
			TooShort:      result.Stats.TooShort,
			Tables:        result.Stats.Tables,
			OverlapRunes:  result.Stats.OverlapRunes,
			DroppedNoise:  result.Stats.DroppedNoise,
		},
	}
	response.CleaningStats.RemovedRunes = result.Cleaning.RemovedRunes
	response.CleaningStats.BeforeRunes = result.Cleaning.BeforeRunes
	response.CleaningStats.AfterRunes = result.Cleaning.AfterRunes
	response.Meta.Parser = result.Meta.Parser
	response.Meta.ParseMode = result.Meta.ParseMode
	response.Meta.PageCount = result.Meta.PageCount
	response.Meta.SourceName = result.Meta.SourceName
	c.JSON(consts.StatusOK, OK(response))
}

type ingestRequest struct {
	Config chunkConfigResponse `json:"config"`
	// SaveAsDefault 为 true 时把这份配置写回知识库默认，下次上传自动带出来。
	SaveAsDefault bool `json:"saveAsDefault"`
}

func (h *chunkWorkbenchHandler) ingest(ctx context.Context, c *app.RequestContext) {
	documentID, ok := parseIDParam(c)
	if !ok {
		return
	}
	var req ingestRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(consts.StatusBadRequest, Fail(CodeInvalidParam, "请求体不是合法 JSON"))
		return
	}
	if err := h.documents.IngestDocument(ctx, documentID, req.Config.toModel(), req.SaveAsDefault); err != nil {
		writeServiceError(c, err)
		return
	}
	// 202：请求已接受，入库在后台进行，客户端轮询文档状态。
	c.JSON(consts.StatusAccepted, OK(nil))
}

func (h *chunkWorkbenchHandler) rechunk(ctx context.Context, c *app.RequestContext) {
	documentID, ok := parseIDParam(c)
	if !ok {
		return
	}
	if err := h.documents.RechunkDocument(ctx, documentID); err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusAccepted, OK(nil))
}

// reparse 重新解析文档（重新跑一次解析器，覆盖解析产物）。
//
// 用途：旧流程入库的文档没有解析产物，改分段配置前需要先补一次解析；
// 解析失败（解析服务抖动、文件损坏）后也可以重试。
func (h *chunkWorkbenchHandler) reparse(ctx context.Context, c *app.RequestContext) {
	documentID, ok := parseIDParam(c)
	if !ok {
		return
	}
	if err := h.documents.ReparseDocument(ctx, documentID); err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusAccepted, OK(nil))
}

// ---------- 检索配置 ----------

type retrievalConfigResponse struct {
	TopK           int     `json:"topK"`
	CandidateTopK  int     `json:"candidateTopK"`
	ScoreThreshold float32 `json:"scoreThreshold"`
	Rerank         bool    `json:"rerank"`
	VectorWeight   float32 `json:"vectorWeight"`
	KeywordWeight  float32 `json:"keywordWeight"`
}

func newRetrievalConfigResponse(config model.RetrievalConfig) retrievalConfigResponse {
	return retrievalConfigResponse{
		TopK:           config.TopK,
		CandidateTopK:  config.CandidateTopK,
		ScoreThreshold: config.ScoreThreshold,
		Rerank:         config.Rerank,
		VectorWeight:   config.VectorWeight,
		KeywordWeight:  config.KeywordWeight,
	}
}

func (h *chunkWorkbenchHandler) getRetrievalConfig(ctx context.Context, c *app.RequestContext) {
	datasetID, ok := parseIDParam(c)
	if !ok {
		return
	}
	config, err := h.documents.GetRetrievalConfig(ctx, datasetID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, OK(newRetrievalConfigResponse(config)))
}

func (h *chunkWorkbenchHandler) saveRetrievalConfig(ctx context.Context, c *app.RequestContext) {
	datasetID, ok := parseIDParam(c)
	if !ok {
		return
	}
	var req retrievalConfigResponse
	if err := c.BindJSON(&req); err != nil {
		c.JSON(consts.StatusBadRequest, Fail(CodeInvalidParam, "请求体不是合法 JSON"))
		return
	}
	saved, err := h.documents.SaveRetrievalConfig(ctx, datasetID, model.RetrievalConfig{
		TopK:           req.TopK,
		CandidateTopK:  req.CandidateTopK,
		ScoreThreshold: req.ScoreThreshold,
		Rerank:         req.Rerank,
		VectorWeight:   req.VectorWeight,
		KeywordWeight:  req.KeywordWeight,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, OK(newRetrievalConfigResponse(saved)))
}
