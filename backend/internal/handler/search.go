package handler

import (
	"context"
	"strconv"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/route"

	"einoflow-backend/internal/model"
	"einoflow-backend/internal/service"
)

// searchHandler 承载知识库检索的 HTTP 处理。
type searchHandler struct {
	searches *service.RetrievalService
}

// registerSearchRoutes 注册检索路由。
// 检索挂在知识库下：它永远发生在某个知识库的范围内，路径本身就表达了这层约束。
func registerSearchRoutes(group *route.RouterGroup, searches *service.RetrievalService) {
	h := &searchHandler{searches: searches}
	group.POST("/datasets/:id/search", h.search)
	group.GET("/datasets/:id/retrieval-runs", h.listRuns)
	group.GET("/datasets/:id/retrieval-runs/:runId", h.getRun)
	group.POST("/datasets/:id/recall-tests", h.recallTest)
}

type searchRequest struct {
	Query string `json:"query"`
	// TopK 不传（或传 0）时用配置里的默认值。
	TopK int `json:"topK"`
	// CandidateTopK 是重排前的召回条数，不传时用配置里的默认值。
	CandidateTopK int `json:"candidateTopK"`
	// Rerank 不传时按默认策略（配了重排模型就启用）；
	// 传 true / false 可直接对比「开重排 / 关重排」的结果差异。
	Rerank *bool `json:"rerank"`
}

// searchHitResponse 是单个命中片段。
//
// 溯源字段（page / locator / titlePath）与内容一起返回，是「检索可追溯」这条需求（D6）的对外形态：
// 使用者不仅能看到命中了什么，还能知道它来自哪一页、哪一个标题下。
type searchHitResponse struct {
	SegmentID    uint64 `json:"segmentId"`
	DocumentID   uint64 `json:"documentId"`
	DocumentName string `json:"documentName"`
	Rank         int    `json:"rank"`
	// Score 是决定本次排名的分数（单路检索是该路分数，混合检索是融合分）。
	Score float32 `json:"score"`
	// VectorScore / FullTextScore / RerankScore 都返回：
	// 使用者才能看出「这条是靠语义还是关键词命中」「重排把哪条提了上来」。
	VectorScore   *float32 `json:"vectorScore,omitempty"`
	FullTextScore *float32 `json:"fullTextScore,omitempty"`
	RerankScore   *float32 `json:"rerankScore,omitempty"`
	Content       string   `json:"content"`
	ChunkIndex    int      `json:"chunkIndex"`
	Page          int      `json:"page"`
	Locator       string   `json:"locator"`
	Kind          string   `json:"kind"`
	TitlePath     []string `json:"titlePath"`
}

type searchResponse struct {
	// RunID 是本次检索的留痕记录 ID，便于事后按 ID 复查这次检索。
	RunID          uint64 `json:"runId"`
	Query          string `json:"query"`
	TopK           int    `json:"topK"`
	EmbeddingModel string `json:"embeddingModel"`
	// Mode 是本次实际使用的检索方式（vector / fulltext / hybrid）。
	// 与 embeddingModel 同理：模式变了结果必然不同，界面上要能看出来。
	Mode string `json:"mode"`
	// RerankModel 为空表示本次未做重排，原因见 rerankSkipped。
	RerankModel   string `json:"rerankModel"`
	RerankSkipped string `json:"rerankSkipped,omitempty"`
	// Recalled 是重排前的召回条数，用于展示「召回 N 条 → 重排后留 M 条」。
	Recalled int `json:"recalled"`
	// Filtered 是被相似度阈值筛掉的条数，Threshold 是本次使用的阈值。
	// 返回它们是为了解释「为什么没结果」——否则使用者会以为检索坏了。
	Filtered  int                 `json:"filtered"`
	Threshold float32             `json:"threshold"`
	LatencyMS int                 `json:"latencyMs"`
	Hits      []searchHitResponse `json:"hits"`
}

func (h *searchHandler) search(ctx context.Context, c *app.RequestContext) {
	datasetID, ok := parseIDParam(c)
	if !ok {
		return
	}

	var req searchRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(consts.StatusBadRequest, Fail(CodeInvalidParam, "请求体不是合法 JSON"))
		return
	}

	result, err := h.searches.Search(ctx, service.SearchInput{
		DatasetID:     datasetID,
		Query:         req.Query,
		TopK:          req.TopK,
		CandidateTopK: req.CandidateTopK,
		Rerank:        req.Rerank,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, OK(newSearchResponse(result)))
}

// newSearchResponse 把检索结果转成对外结构；检索接口与召回测试共用。
func newSearchResponse(result *service.SearchResult) searchResponse {
	hits := make([]searchHitResponse, 0, len(result.Hits))
	for _, hit := range result.Hits {
		hits = append(hits, searchHitResponse{
			SegmentID:     hit.SegmentID,
			DocumentID:    hit.DocumentID,
			DocumentName:  hit.DocumentName,
			Rank:          hit.Rank,
			Score:         hit.Score,
			VectorScore:   hit.VectorScore,
			FullTextScore: hit.FullTextScore,
			RerankScore:   hit.RerankScore,
			Content:       hit.Content,
			ChunkIndex:    hit.ChunkIndex,
			Page:          hit.Page,
			Locator:       hit.Locator,
			Kind:          hit.Kind,
			TitlePath:     hit.TitlePath,
		})
	}

	return searchResponse{
		RunID:          result.RunID,
		Query:          result.Query,
		TopK:           result.TopK,
		EmbeddingModel: result.EmbeddingModel,
		Mode:           string(result.Mode),
		RerankModel:    result.RerankModel,
		RerankSkipped:  result.RerankSkipped,
		Recalled:       result.Recalled,
		Filtered:       result.Filtered,
		Threshold:      result.Threshold,
		LatencyMS:      result.LatencyMS,
		Hits:           hits,
	}
}

// retrievalRunResponse 是检索历史里的一条运行记录。
type retrievalRunResponse struct {
	ID             uint64                   `json:"id"`
	DatasetID      uint64                   `json:"datasetId"`
	Query          string                   `json:"query"`
	TopK           int                      `json:"topK"`
	EmbeddingModel string                   `json:"embeddingModel"`
	Mode           string                   `json:"mode"`
	CandidateCount int                      `json:"candidateCount"`
	RerankModel    string                   `json:"rerankModel"`
	RerankSkipped  string                   `json:"rerankSkipped,omitempty"`
	LatencyMS      int                      `json:"latencyMs"`
	HitCount       int                      `json:"hitCount"`
	ErrorMessage   string                   `json:"errorMessage,omitempty"`
	Config         model.RetrievalRunConfig `json:"config"`
	CreatedAt      string                   `json:"createdAt"`
	Hits           []retrievalHitDetail     `json:"hits"`
}

type retrievalHitDetail struct {
	ID            uint64   `json:"id"`
	RunID         uint64   `json:"runId"`
	SegmentID     uint64   `json:"segmentId"`
	DocumentID    uint64   `json:"documentId"`
	DocumentName  string   `json:"documentName"`
	Rank          int      `json:"rank"`
	Score         float32  `json:"score"`
	VectorScore   *float32 `json:"vectorScore,omitempty"`
	FullTextScore *float32 `json:"fullTextScore,omitempty"`
	RerankScore   *float32 `json:"rerankScore,omitempty"`
	Stage         string   `json:"stage"`
	Content       string   `json:"content"`
	ChunkIndex    int      `json:"chunkIndex"`
	Page          int      `json:"page"`
	Locator       string   `json:"locator"`
	Kind          string   `json:"kind"`
	TitlePath     []string `json:"titlePath"`
}

type retrievalRunListResponse struct {
	Items    []retrievalRunResponse `json:"items"`
	Total    int64                  `json:"total"`
	Page     int                    `json:"page"`
	PageSize int                    `json:"pageSize"`
}

func newRetrievalRunResponse(view *service.RetrievalRunView) retrievalRunResponse {
	run := view.Run
	hits := make([]retrievalHitDetail, 0, len(view.Hits))
	for _, hit := range view.Hits {
		hits = append(hits, retrievalHitDetail{
			ID:            hit.ID,
			RunID:         hit.RunID,
			SegmentID:     hit.SegmentID,
			DocumentID:    hit.DocumentID,
			DocumentName:  hit.DocumentName,
			Rank:          hit.Rank,
			Score:         hit.Score,
			VectorScore:   hit.VectorScore,
			FullTextScore: hit.FullTextScore,
			RerankScore:   hit.RerankScore,
			Stage:         string(hit.Stage),
			Content:       hit.Content,
			ChunkIndex:    hit.ChunkIndex,
			Page:          hit.Page,
			Locator:       hit.Locator,
			Kind:          hit.Kind,
			TitlePath:     hit.TitlePath,
		})
	}
	return retrievalRunResponse{
		ID:             run.ID,
		DatasetID:      run.DatasetID,
		Query:          run.Query,
		TopK:           run.TopK,
		EmbeddingModel: run.EmbeddingModel,
		Mode:           run.SearchMode,
		CandidateCount: run.CandidateCount,
		RerankModel:    run.RerankModel,
		RerankSkipped:  run.ConfigSnapshot.RerankSkipped,
		LatencyMS:      run.LatencyMS,
		HitCount:       run.HitCount,
		ErrorMessage:   run.ErrorMessage,
		Config:         run.ConfigSnapshot,
		CreatedAt:      formatTime(run.CreatedAt),
		Hits:           hits,
	}
}

func (h *searchHandler) listRuns(ctx context.Context, c *app.RequestContext) {
	datasetID, ok := parseIDParam(c)
	if !ok {
		return
	}
	from, err := parseOptionalTime(c.Query("from"))
	if err != nil {
		c.JSON(consts.StatusBadRequest, Fail(CodeInvalidParam, "from 必须是 RFC3339 时间"))
		return
	}
	to, err := parseOptionalTime(c.Query("to"))
	if err != nil {
		c.JSON(consts.StatusBadRequest, Fail(CodeInvalidParam, "to 必须是 RFC3339 时间"))
		return
	}
	page, err := parseIntQuery(c.Query("page"))
	if err != nil {
		c.JSON(consts.StatusBadRequest, Fail(CodeInvalidParam, "page 必须是整数"))
		return
	}
	pageSize, err := parseIntQuery(c.Query("pageSize"))
	if err != nil {
		c.JSON(consts.StatusBadRequest, Fail(CodeInvalidParam, "pageSize 必须是整数"))
		return
	}

	result, err := h.searches.ListRuns(ctx, service.ListRetrievalRunsInput{
		DatasetID: datasetID,
		From:      from,
		To:        to,
		Page:      page,
		PageSize:  pageSize,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	items := make([]retrievalRunResponse, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, newRetrievalRunResponse(item))
	}
	c.JSON(consts.StatusOK, OK(retrievalRunListResponse{
		Items:    items,
		Total:    result.Total,
		Page:     result.Page,
		PageSize: result.PageSize,
	}))
}

func (h *searchHandler) getRun(ctx context.Context, c *app.RequestContext) {
	datasetID, ok := parseIDParam(c)
	if !ok {
		return
	}
	runID, err := strconv.ParseUint(c.Param("runId"), 10, 64)
	if err != nil || runID == 0 {
		c.JSON(consts.StatusBadRequest, Fail(CodeInvalidParam, "runId 必须是正整数"))
		return
	}
	view, err := h.searches.GetRun(ctx, datasetID, runID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, OK(newRetrievalRunResponse(view)))
}

type recallTestRequest struct {
	Query          string   `json:"query"`
	TopK           int      `json:"topK"`
	CandidateTopK  int      `json:"candidateTopK"`
	Rerank         *bool    `json:"rerank"`
	Mode           *string  `json:"mode"`
	HybridRank     *string  `json:"hybridRank"`
	VectorWeight   *float64 `json:"vectorWeight"`
	FullTextWeight *float64 `json:"fullTextWeight"`
	ScoreThreshold *float32 `json:"scoreThreshold"`
}

func (h *searchHandler) recallTest(ctx context.Context, c *app.RequestContext) {
	datasetID, ok := parseIDParam(c)
	if !ok {
		return
	}
	var req recallTestRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(consts.StatusBadRequest, Fail(CodeInvalidParam, "请求体不是合法 JSON"))
		return
	}

	input := service.SearchInput{
		DatasetID:      datasetID,
		Query:          req.Query,
		TopK:           req.TopK,
		CandidateTopK:  req.CandidateTopK,
		Rerank:         req.Rerank,
		VectorWeight:   req.VectorWeight,
		FullTextWeight: req.FullTextWeight,
		ScoreThreshold: req.ScoreThreshold,
	}
	if req.Mode != nil {
		mode := model.SearchMode(*req.Mode)
		input.Mode = &mode
	}
	if req.HybridRank != nil {
		rank := model.HybridRank(*req.HybridRank)
		input.HybridRank = &rank
	}

	result, err := h.searches.RecallTest(ctx, input)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(consts.StatusOK, OK(newSearchResponse(result)))
}

func parseOptionalTime(raw string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func parseIntQuery(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	return strconv.Atoi(raw)
}
