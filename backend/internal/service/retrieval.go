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
}

// SearchResult 是检索结果。
type SearchResult struct {
	// RunID 是本次检索的留痕记录 ID，便于事后按 ID 复查这次检索。
	RunID          uint64
	Query          string
	TopK           int
	EmbeddingModel string
	LatencyMS      int
	Hits           []rag.Hit
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

// RetrievalDefaults 是检索的兜底参数。
type RetrievalDefaults struct {
	TopK int
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

	topK := input.TopK
	if topK <= 0 {
		topK = s.defaults.TopK
	}
	if topK > model.MaxRetrievalTopK {
		return nil, fmt.Errorf("%w: topK 最大为 %d", ErrInvalidInput, model.MaxRetrievalTopK)
	}

	// 先确认知识库存在：否则「知识库不存在」会表现为「搜不到东西」，
	// 使用者会去怀疑检索质量而不会想到是 ID 写错。
	if _, err := s.datasets.GetByID(ctx, input.DatasetID); err != nil {
		return nil, translateError(err)
	}

	started := time.Now()
	output, err := s.retriever.Retrieve(ctx, rag.RetrieveRequest{
		DatasetID: input.DatasetID,
		Query:     query,
		TopK:      topK,
	})
	latency := time.Since(started)

	result := &SearchResult{
		Query:          query,
		TopK:           topK,
		EmbeddingModel: output.EmbeddingModel,
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
	run := &model.RetrievalRun{
		DatasetID:      datasetID,
		Query:          query,
		TopK:           topK,
		EmbeddingModel: output.EmbeddingModel,
		LatencyMS:      int(latency.Milliseconds()),
		HitCount:       len(output.Hits),
	}
	if retrieveErr != nil {
		run.ErrorMessage = truncateRunes(retrieveErr.Error(), traceErrorMessageLimit)
	}
	if err := s.traces.CreateRun(ctx, run); err != nil {
		return 0, err
	}

	if len(output.Hits) == 0 {
		return run.ID, nil
	}

	hits := make([]*model.RetrievalHit, 0, len(output.Hits))
	for _, hit := range output.Hits {
		hits = append(hits, &model.RetrievalHit{
			RunID:      run.ID,
			DatasetID:  datasetID,
			SegmentID:  hit.SegmentID,
			DocumentID: hit.DocumentID,
			Rank:       hit.Rank,
			Score:      hit.Score,
			Stage:      model.RetrievalStageVector,
		})
	}
	if err := s.traces.CreateHits(ctx, hits); err != nil {
		return run.ID, err
	}
	return run.ID, nil
}

// truncateRunes 按字符数截断，避免中文被按字节切断产生半个字。
func truncateRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}
