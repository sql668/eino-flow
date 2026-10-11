package service

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"einoflow-backend/internal/engine/rag"
	"einoflow-backend/internal/model"
	"einoflow-backend/internal/repository"
)

// 检索入参的长度限制。
const (
	// maxQueryRunes 限制提问长度：过长的查询既没有检索意义，也会拖慢向量化。
	maxQueryRunes = 2000
	// traceErrorMessageLimit 与 retrieval_runs.error_message 列长度对齐（VARCHAR(512)）。
	traceErrorMessageLimit = 500
)

// SearchInput 是检索入参。
type SearchInput struct {
	DatasetID uint64
	Query     string
	// TopK 为 0 时用配置里的默认值。
	TopK int
	// CandidateTopK 为 0 时用配置里的默认值（重排前的召回条数）。
	CandidateTopK int
	// Rerank 为 nil 时按默认策略（配了重排模型就启用），
	// 显式传 true / false 用于对比两阶段检索的效果。
	Rerank *bool
}

// SearchResult 是检索结果。
type SearchResult struct {
	// RunID 是本次检索的留痕记录 ID，便于事后按 ID 复查这次检索。
	RunID          uint64
	Query          string
	TopK           int
	EmbeddingModel string
	// Mode 是本次实际使用的检索方式（向量 / 全文 / 混合）。
	Mode repository.SearchMode
	// RerankModel 为空表示本次未做重排；RerankSkipped 说明原因。
	RerankModel   string
	RerankSkipped string
	// Recalled 是重排前的召回条数（含被阈值筛掉的）。
	Recalled int
	// Filtered 是被相似度阈值筛掉的条数；Threshold 是本次实际使用的阈值。
	Filtered  int
	Threshold float32
	LatencyMS int
	Hits      []rag.Hit
}

// Retriever 是检索能力的抽象。
//
// 用接口而不是直接依赖 *rag.Retriever：一是便于单测打桩（真实检索要连 Ollama 与 Milvus），
// 二是重排、混合检索等新实现可以按同一契约替换进来。
type Retriever interface {
	Retrieve(ctx context.Context, req rag.RetrieveRequest) (rag.RetrieveOutput, error)
}

// RetrievalService 承载检索与检索留痕。
//
// 留痕放在 service 而不是 engine：engine 只负责「算出命中」，
// 「这次检索被谁问的、要记录什么」属于业务语义。
type RetrievalService struct {
	datasets  repository.DatasetRepository
	retriever Retriever
	traces    repository.RetrievalRepository
	defaults  RetrievalDefaults
}

// RetrievalDefaults 是检索的兜底参数（知识库没配过检索设置时使用）。
type RetrievalDefaults struct {
	TopK int
	// CandidateTopK 是重排前的召回条数兜底值。
	CandidateTopK int
}

// NewRetrievalService 构造检索服务。
func NewRetrievalService(
	datasets repository.DatasetRepository,
	retriever Retriever,
	traces repository.RetrievalRepository,
	defaults RetrievalDefaults,
) *RetrievalService {
	if defaults.TopK <= 0 {
		defaults.TopK = model.DefaultRetrievalTopK
	}
	return &RetrievalService{
		datasets:  datasets,
		retriever: retriever,
		traces:    traces,
		defaults:  defaults,
	}
}

// Search 执行一次检索，并把本次检索与命中写入留痕表。
func (s *RetrievalService) Search(ctx context.Context, input SearchInput) (*SearchResult, error) {
	if input.DatasetID == 0 {
		return nil, fmt.Errorf("%w: 缺少知识库 ID", ErrInvalidInput)
	}

	query := strings.TrimSpace(input.Query)
	if query == "" {
		return nil, fmt.Errorf("%w: 检索内容不能为空", ErrInvalidInput)
	}
	if len([]rune(query)) > maxQueryRunes {
		return nil, fmt.Errorf("%w: 检索内容过长（最多 %d 个字符）", ErrInvalidInput, maxQueryRunes)
	}

	// 先确认知识库存在：否则「知识库不存在」会表现为「搜不到东西」，
	// 使用者会去怀疑检索质量而不会想到是 ID 写错。
	// 同时取出知识库级检索配置：TopK / 阈值 / 重排开关都以它为准，
	// 请求没显式指定时就用知识库的设置（否则「知识库设置」形同虚设）。
	dataset, err := s.datasets.GetByID(ctx, input.DatasetID)
	if err != nil {
		return nil, translateError(err)
	}
	retrievalConfig := retrievalConfigOrDefault(dataset.RetrievalConfig, s.defaults)

	topK := input.TopK
	if topK <= 0 {
		topK = retrievalConfig.TopK
	}
	if topK > model.MaxRetrievalTopK {
		return nil, fmt.Errorf("%w: topK 最大为 %d", ErrInvalidInput, model.MaxRetrievalTopK)
	}
	candidateTopK := input.CandidateTopK
	if candidateTopK <= 0 {
		candidateTopK = retrievalConfig.CandidateTopK
	}
	if candidateTopK > model.MaxCandidateTopK {
		return nil, fmt.Errorf("%w: candidateTopK 最大为 %d", ErrInvalidInput, model.MaxCandidateTopK)
	}
	// 重排开关：请求显式指定优先，否则用知识库配置。
	rerank := input.Rerank
	if rerank == nil {
		enabled := retrievalConfig.Rerank
		rerank = &enabled
	}

	started := time.Now()
	output, err := s.retriever.Retrieve(ctx, rag.RetrieveRequest{
		DatasetID:     input.DatasetID,
		Query:         query,
		TopK:          topK,
		CandidateTopK: candidateTopK,
		Rerank:        rerank,
		// 检索方式与融合参数来自知识库配置：同 Dify 一样在知识库设置页选，
		// 检索时不再让调用方重复指定（否则「设置页选了全文、接口却在做向量」）。
		Mode:           retrievalConfig.SearchMode,
		HybridRank:     retrievalConfig.HybridRank,
		VectorWeight:   float64(retrievalConfig.VectorWeight),
		FullTextWeight: float64(retrievalConfig.KeywordWeight),
		// 阈值来自知识库配置：这是「问什么都给一段」的解法，配置页可改。
		ScoreThreshold: retrievalConfig.ScoreThreshold,
	})
	latency := time.Since(started)

	result := &SearchResult{
		Query:          query,
		TopK:           topK,
		EmbeddingModel: output.EmbeddingModel,
		Mode:           output.Mode,
		RerankModel:    output.RerankModel,
		RerankSkipped:  output.RerankSkipped,
		Recalled:       len(output.Candidates) + output.FilteredByThreshold,
		Filtered:       output.FilteredByThreshold,
		Threshold:      output.Threshold,
		LatencyMS:      int(latency.Milliseconds()),
		Hits:           output.Hits,
	}

	// 留痕失败不能让检索失败：用户要的是结果，记录只是副作用。
	// 但必须留下日志，否则「记录为什么是空的」会变成新的谜题。
	runID, traceErr := s.recordTrace(ctx, input.DatasetID, query, topK, latency, output, err)
	if traceErr != nil {
		log.Printf("检索留痕写入失败（知识库 %d）: %v", input.DatasetID, traceErr)
	}
	result.RunID = runID

	if err != nil {
		return nil, err
	}
	return result, nil
}

// recordTrace 写入一次检索与它的命中片段，返回检索记录 ID。
func (s *RetrievalService) recordTrace(
	ctx context.Context,
	datasetID uint64,
	query string,
	topK int,
	latency time.Duration,
	output rag.RetrieveOutput,
	retrieveErr error,
) (uint64, error) {
	// 召回条数取候选数量；没走重排时候选就等于命中，两者一致。
	recalled := len(output.Candidates)
	if recalled == 0 {
		recalled = len(output.Hits)
	}
	run := &model.RetrievalRun{
		DatasetID:      datasetID,
		Query:          query,
		TopK:           topK,
		CandidateCount: recalled,
		EmbeddingModel: output.EmbeddingModel,
		SearchMode:     string(stageForMode(output.Mode)),
		RerankModel:    output.RerankModel,
		LatencyMS:      int(latency.Milliseconds()),
		HitCount:       len(output.Hits),
	}
	if retrieveErr != nil {
		run.ErrorMessage = truncateRunes(retrieveErr.Error(), traceErrorMessageLimit)
	}
	if err := s.traces.CreateRun(ctx, run); err != nil {
		return 0, err
	}

	if len(output.Hits) == 0 && recalled == 0 {
		return run.ID, nil
	}

	// 两阶段都留痕（见 D6）：先记召回阶段的全部候选，再记重排阶段的最终结果。
	// 只记最终结果的话，「重排把哪条提上来、把哪条压下去」就无从回溯。
	hits := make([]*model.RetrievalHit, 0, len(output.Candidates)+len(output.Hits))
	for _, hit := range output.Candidates {
		hits = append(hits, &model.RetrievalHit{
			RunID:         run.ID,
			DatasetID:     datasetID,
			SegmentID:     hit.SegmentID,
			DocumentID:    hit.DocumentID,
			Rank:          hit.Rank,
			Score:         hit.Score,
			VectorScore:   hit.VectorScore,
			FullTextScore: hit.FullTextScore,
			Stage:         stageForMode(output.Mode),
		})
	}
	for _, hit := range output.Hits {
		if hit.RerankScore == nil {
			// 未做重排（被关闭或未配置）时不写 rerank 阶段：
			// 写一条 rerank_score 为空的记录会让人误以为「重排跑了却没给分」。
			continue
		}
		hits = append(hits, &model.RetrievalHit{
			RunID:         run.ID,
			DatasetID:     datasetID,
			SegmentID:     hit.SegmentID,
			DocumentID:    hit.DocumentID,
			Rank:          hit.Rank,
			Score:         hit.Score,
			VectorScore:   hit.VectorScore,
			FullTextScore: hit.FullTextScore,
			RerankScore:   hit.RerankScore,
			Stage:         model.RetrievalStageRerank,
		})
	}
	if err := s.traces.CreateHits(ctx, hits); err != nil {
		return run.ID, err
	}
	return run.ID, nil
}

// stageForMode 把检索方式映射成留痕的阶段名。
//
// 两者取值刻意一致（vector / fulltext / hybrid）：留痕要回答的是
// 「这次命中是哪条路线产生的」，用同一个词表才不会出现「模式叫 hybrid、阶段叫 fusion」的错位。
func stageForMode(mode repository.SearchMode) model.RetrievalStage {
	switch mode {
	case repository.SearchModeFullText:
		return model.RetrievalStageFullText
	case repository.SearchModeHybrid:
		return model.RetrievalStageHybrid
	default:
		return model.RetrievalStageVector
	}
}

// truncateRunes 按字符数截断，避免中文被按字节切断产生半个字。
func truncateRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}
