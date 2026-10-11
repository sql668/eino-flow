package handler

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/route"

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
	// VectorScore 与 RerankScore 都返回：使用者才能看出重排把哪条提了上来。
	VectorScore float32  `json:"vectorScore"`
	RerankScore *float32 `json:"rerankScore,omitempty"`
	Content     string   `json:"content"`
	ChunkIndex  int      `json:"chunkIndex"`
	Page        int      `json:"page"`
	Locator     string   `json:"locator"`
	Kind        string   `json:"kind"`
	TitlePath   []string `json:"titlePath"`
}

type searchResponse struct {
	// RunID 是本次检索的留痕记录 ID，便于事后按 ID 复查这次检索。
	RunID          uint64 `json:"runId"`
	Query          string `json:"query"`
	TopK           int    `json:"topK"`
	EmbeddingModel string `json:"embeddingModel"`
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

	hits := make([]searchHitResponse, 0, len(result.Hits))
	for _, hit := range result.Hits {
		hits = append(hits, searchHitResponse{
			SegmentID:    hit.SegmentID,
			DocumentID:   hit.DocumentID,
			DocumentName: hit.DocumentName,
			Rank:         hit.Rank,
			VectorScore:  hit.VectorScore,
			RerankScore:  hit.RerankScore,
			Content:      hit.Content,
			ChunkIndex:   hit.ChunkIndex,
			Page:         hit.Page,
			Locator:      hit.Locator,
			Kind:         hit.Kind,
			TitlePath:    hit.TitlePath,
		})
	}

	c.JSON(consts.StatusOK, OK(searchResponse{
		RunID:          result.RunID,
		Query:          result.Query,
		TopK:           result.TopK,
		EmbeddingModel: result.EmbeddingModel,
		RerankModel:    result.RerankModel,
		RerankSkipped:  result.RerankSkipped,
		Recalled:       result.Recalled,
		Filtered:       result.Filtered,
		Threshold:      result.Threshold,
		LatencyMS:      result.LatencyMS,
		Hits:           hits,
	}))
}
