package rag

import (
	"context"
	"fmt"
	"strings"

	"einoflow-backend/internal/engine/model"
	"einoflow-backend/internal/repository"
)

// RetrieveRequest 是一次检索的入参。
type RetrieveRequest struct {
	DatasetID uint64
	Query     string
	// TopK <= 0 时用默认值。
	TopK int
}

// Hit 是一个命中片段，带齐前端展示引用所需的溯源信息。
//
// 内容与溯源字段一次性返回，而不是让前端拿 ID 再查一遍：
// 检索结果页要展示的就是这些字段，二次查询会让命中数与展示数出现不一致的窗口。
type Hit struct {
	SegmentID  uint64
	DocumentID uint64
	// DocumentName 是命中片段所属文档名，用于展示「出自哪份文档」。
	DocumentName string
	Content      string
	// Rank 是本次检索内的排名，从 1 开始。
	Rank int
	// Score 是向量相似度（余弦，越大越相似）。展示分数是为了让使用者能判断召回质量。
	Score      float32
	ChunkIndex int
	Page       int
	Locator    string
	Kind       string
	TitlePath  []string
}

// RetrieveOutput 是一次检索的结果。
type RetrieveOutput struct {
	Hits []Hit
	// EmbeddingModel 是本次使用的向量模型。
	// 检索记录要落库（见 docs/PROGRESS.md D6），换模型后要能解释「为什么结果变了」。
	EmbeddingModel string
}

// Retriever 执行「问题 → 向量 → 命中片段」的检索。
//
// 只做召回，不做重排与生成：重排（TEI 边车）与答案生成分别由后续阶段接入，
// 拆开的好处是每一段都能单独验证与替换。
type Retriever struct {
	models   model.Resolver
	vectors  repository.VectorStore
	segments repository.SegmentRepository
	// defaultTopK 是请求未指定 top_k 时的兜底值。
	defaultTopK int
}

// RetrieverDeps 是检索器依赖。
type RetrieverDeps struct {
	Models      model.Resolver
	Vectors     repository.VectorStore
	Segments    repository.SegmentRepository
	DefaultTopK int
}

// NewRetriever 构造检索器。
func NewRetriever(deps RetrieverDeps) *Retriever {
	return &Retriever{
		models:      deps.Models,
		vectors:     deps.Vectors,
		segments:    deps.Segments,
		defaultTopK: deps.DefaultTopK,
	}
}

// Retrieve 执行一次检索。
func (r *Retriever) Retrieve(ctx context.Context, req RetrieveRequest) (RetrieveOutput, error) {
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return RetrieveOutput{}, fmt.Errorf("检索内容不能为空")
	}
	if req.DatasetID == 0 {
		return RetrieveOutput{}, fmt.Errorf("检索必须指定知识库")
	}

	topK := req.TopK
	if topK <= 0 {
		topK = r.defaultTopK
	}

	// 用与入库同一个作用域解析模型：知识库级覆盖必须两边一致，
	// 一边用 A 模型入库、一边用 B 模型检索，向量不在同一空间，检索结果就是噪声。
	embedder, err := r.models.Embedder(ctx, model.ScopeRef{DatasetID: req.DatasetID})
	if err != nil {
		return RetrieveOutput{}, fmt.Errorf("获取向量化模型失败: %w", err)
	}

	// 查询与入库必须用同一个模型，否则两边向量不在同一空间，检索结果会退化成随机噪声。
	// 这正是把模型解析收敛到 resolver 的原因：换模型时入库与检索一起换。
	vectors, err := embedder.Embed(ctx, []string{query})
	if err != nil {
		return RetrieveOutput{}, fmt.Errorf("向量化检索内容失败: %w", err)
	}
	if len(vectors) != 1 {
		return RetrieveOutput{}, fmt.Errorf("向量化检索内容返回了 %d 条向量，期望 1 条", len(vectors))
	}

	matches, err := r.vectors.Search(ctx, req.DatasetID, vectors[0], topK)
	if err != nil {
		return RetrieveOutput{}, err
	}
	output := RetrieveOutput{EmbeddingModel: embedder.Name(), Hits: []Hit{}}
	if len(matches) == 0 {
		return output, nil
	}

	ids := make([]uint64, 0, len(matches))
	for _, match := range matches {
		ids = append(ids, match.SegmentID)
	}
	found, err := r.segments.ListForRetrieval(ctx, req.DatasetID, ids)
	if err != nil {
		return RetrieveOutput{}, err
	}

	// 向量库与业务库是两套存储，可能出现「向量在、分段不在」（文档刚被删除）的情况。
	// 这里按向量分数顺序组装，找不到的分段直接跳过，因此命中数可能少于召回数。
	byID := make(map[uint64]repository.RetrievedSegment, len(found))
	for _, item := range found {
		byID[item.Segment.ID] = item
	}

	for _, match := range matches {
		item, ok := byID[match.SegmentID]
		if !ok {
			continue
		}
		segment := item.Segment
		output.Hits = append(output.Hits, Hit{
			SegmentID:    segment.ID,
			DocumentID:   segment.DocumentID,
			DocumentName: item.DocumentName,
			Content:      segment.Content,
			Rank:         len(output.Hits) + 1,
			Score:        match.Score,
			ChunkIndex:   segment.ChunkIndex,
			Page:         segment.Page,
			Locator:      segment.Locator,
			Kind:         segment.Kind,
			TitlePath:    segment.TitlePath,
		})
	}
	return output, nil
}
