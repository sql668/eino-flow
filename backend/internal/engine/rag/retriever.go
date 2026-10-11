package rag

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"einoflow-backend/internal/engine/model"
	"einoflow-backend/internal/repository"
)

// RetrieveRequest 是一次检索的入参。
type RetrieveRequest struct {
	DatasetID uint64
	Query     string
	// TopK 是最终返回条数，<=0 时用默认值。
	TopK int
	// CandidateTopK 是重排前的向量召回条数，<=0 时用默认值。
	// 必须 >= TopK：召回比最终结果还少，重排就没有可挑的余地。
	CandidateTopK int
	// Rerank 为 nil 表示按默认策略（配了重排模型就用），
	// true / false 用于显式开关——页面上的「开/关重排对比」就靠它。
	Rerank *bool
	// ScoreThreshold 是向量相似度阈值：低于它的候选直接丢弃（0 表示不启用）。
	//
	// 阈值作用在**向量分数**上而不是重排分数：两者的量纲不同
	// （向量是余弦相似度，重排是归一化后的相关性），配置里给的 0.5 这类值
	// 按语义就是针对召回阶段的；先筛再重排也顺带省掉无关候选的重排开销。
	//
	// 生效范围：向量检索（在 Go 侧筛，能统计筛掉几条）与混合检索
	// （下推到向量路做 range 过滤——融合接口不回传单路分数，只能在服务端卡）；
	// 全文检索下没有向量参与，这个阈值对它没有意义。
	ScoreThreshold float32
	// Mode 是检索方式（向量 / 全文 / 混合）；空值按向量检索处理。
	Mode repository.SearchMode
	// HybridRank 是混合检索两路结果的融合方式（加权 / RRF）。
	HybridRank repository.HybridRank
	// VectorWeight / FullTextWeight 是加权融合的两路权重。
	VectorWeight   float64
	FullTextWeight float64
}

// Hit 是一个命中片段，带齐前端展示引用所需的溯源信息。
//
// 内容与溯源字段一次性返回，而不是让前端拿 ID 再查一遍：
// 检索结果页要展示的就是这些字段，二次查询会让命中数与展示数出现不一致的窗口。
type Hit struct {
	// ChunkID 是片段主键（向量库主键）：引用留痕与「按片段复查」都靠它。
	ChunkID    string
	SegmentID  uint64
	DocumentID uint64
	// DocumentName 是命中片段所属文档名，用于展示「出自哪份文档」。
	DocumentName string
	Content      string
	// Rank 是本次检索内的排名，从 1 开始。
	Rank int
	// Score 是决定本次召回的分数：单路检索时是该路的分数，
	// 混合检索时是两路融合后的分数。它只用于说明「为什么排在第 N 位」。
	Score float32
	// VectorScore / FullTextScore 是两路各自的分数；该路未参与或服务端未回传时为空。
	//
	// 多个分数都返回而不是只返回最终分：使用者才能判断
	// 「这条是语义命中还是关键词命中」「重排把哪一条提上来了」——
	// 这也是「检索可追溯」要展示的内容。
	VectorScore   *float32
	FullTextScore *float32
	// RerankScore 是重排分数；未做重排时为空。
	RerankScore *float32
	ChunkIndex  int
	Page        int
	Locator     string
	Kind        string
	TitlePath   []string
}

// RetrieveOutput 是一次检索的结果。
type RetrieveOutput struct {
	Hits []Hit
	// Candidates 是召回阶段的全部候选（按向量分数排序），
	// 用于把「召回 → 重排」的完整过程落库留痕（见 D6）。
	Candidates []Hit
	// EmbeddingModel 是本次使用的向量模型。
	// 检索记录要落库（见 docs/PROGRESS.md D6），换模型后要能解释「为什么结果变了」。
	EmbeddingModel string
	// Mode 是本次实际使用的检索方式（留痕要记，否则「同问题不同结果」无从解释）。
	Mode repository.SearchMode
	// RerankModel 是本次使用的重排模型；未做重排时为空。
	RerankModel string
	// RerankSkipped 记录「为什么没做重排」（未配置 / 被显式关闭）。
	// 不记录的话，「结果没变」会被误判成「重排没效果」。
	RerankSkipped string
	// FilteredByThreshold 是被相似度阈值筛掉的候选数。
	// 返回它是为了解释「为什么没结果」——否则使用者会以为检索坏了。
	FilteredByThreshold int
	// Threshold 是本次实际使用的阈值。
	Threshold float32
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
	// defaultCandidateTopK 是请求未指定召回条数时的兜底值。
	defaultCandidateTopK int
	// rerankEnabled 是默认策略：配了重排模型是否直接使用。
	rerankEnabled bool
	// rerankStrict 决定重排失败时是报错还是降级。
	rerankStrict bool
}

// RetrieverDeps 是检索器依赖。
type RetrieverDeps struct {
	Models               model.Resolver
	Vectors              repository.VectorStore
	Segments             repository.SegmentRepository
	DefaultTopK          int
	DefaultCandidateTopK int
	// RerankEnabled 为 true 时，未显式指定就默认执行重排。
	RerankEnabled bool
	// RerankStrict 为 true 时，重排调用失败直接让检索失败；
	// 为 false 时降级为向量顺序，并在结果里写明原因（见 RerankSkipped）。
	RerankStrict bool
}

// NewRetriever 构造检索器。
func NewRetriever(deps RetrieverDeps) *Retriever {
	return &Retriever{
		models:               deps.Models,
		vectors:              deps.Vectors,
		segments:             deps.Segments,
		defaultTopK:          deps.DefaultTopK,
		defaultCandidateTopK: deps.DefaultCandidateTopK,
		rerankEnabled:        deps.RerankEnabled,
		rerankStrict:         deps.RerankStrict,
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
	candidateTopK := req.CandidateTopK
	if candidateTopK <= 0 {
		candidateTopK = r.defaultCandidateTopK
	}
	if candidateTopK < topK {
		// 召回少于最终条数时按最终条数召回：宁可多召回一次，也不要返回不足条数的结果。
		candidateTopK = topK
	}

	mode := req.Mode
	if mode == "" {
		mode = repository.SearchModeVector
	}
	if !mode.IsValid() {
		return RetrieveOutput{}, fmt.Errorf("不支持的检索方式 %q（可选：vector / fulltext / hybrid）", mode)
	}

	// 全文检索不需要查询向量。这里刻意跳过向量化：
	// 一是省一次模型调用（本机 8B 之外的 embedding 也有百毫秒级开销），
	// 二是「没配 embedding 也能全文检索」在部署早期是有意义的。
	searchQuery := repository.SearchQuery{
		Text:           query,
		Mode:           mode,
		Rank:           req.HybridRank,
		VectorWeight:   req.VectorWeight,
		FullTextWeight: req.FullTextWeight,
	}
	output := RetrieveOutput{
		Mode:           mode,
		Hits:           []Hit{},
		EmbeddingModel: "",
	}
	if mode != repository.SearchModeFullText {
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
		searchQuery.Vector = vectors[0]
		output.EmbeddingModel = embedder.Name()
	}

	matches, err := r.vectors.Search(ctx, repository.SearchFilter{
		DatasetID: req.DatasetID,
		TopK:      candidateTopK,
	}, searchQuery)
	if err != nil {
		return RetrieveOutput{}, err
	}
	output.Candidates = []Hit{}
	if len(matches) == 0 {
		return output, nil
	}

	// 向量库返回的是 chunk_id（业务生成的主键），按它回查 PostgreSQL 取正文。
	ids := make([]string, 0, len(matches))
	for _, match := range matches {
		ids = append(ids, match.ChunkID)
	}
	found, err := r.segments.ListForRetrieval(ctx, req.DatasetID, ids)
	if err != nil {
		return RetrieveOutput{}, err
	}

	// 向量库与业务库是两套存储，可能出现「向量在、分段不在」（文档刚被删除）的情况。
	// 这里按向量分数顺序组装，找不到的分段直接跳过，因此命中数可能少于召回数。
	byID := make(map[string]repository.RetrievedSegment, len(found))
	for _, item := range found {
		byID[item.Segment.ChunkID] = item
	}

	for _, match := range matches {
		item, ok := byID[match.ChunkID]
		if !ok {
			continue
		}
		segment := item.Segment
		output.Candidates = append(output.Candidates, Hit{
			ChunkID:       segment.ChunkID,
			SegmentID:     segment.ID,
			DocumentID:    segment.DocumentID,
			DocumentName:  item.DocumentName,
			Content:       segment.Content,
			Rank:          len(output.Candidates) + 1,
			Score:         match.Score,
			VectorScore:   match.VectorScore,
			FullTextScore: match.FullTextScore,
			ChunkIndex:    segment.ChunkIndex,
			Page:          segment.Page,
			Locator:       segment.Locator,
			Kind:          segment.Kind,
			TitlePath:     segment.TitlePath,
		})
	}
	if len(output.Candidates) == 0 {
		return output, nil
	}

	// 相似度阈值过滤：作用在**向量分**上，向量检索与混合检索都生效
	// （全文检索没有向量分，见 RetrieveRequest.ScoreThreshold）。
	//
	// 混合检索下对「同时被向量路召回」的候选按向量分筛；
	// 只有关键词命中的候选没有向量分（不在 ANN 的 TopK 里），
	// 它是靠关键词进来的，不该被语义阈值误杀，因此保留。
	if req.ScoreThreshold > 0 && (mode == repository.SearchModeVector || mode == repository.SearchModeHybrid) {
		kept := output.Candidates[:0:0]
		for _, hit := range output.Candidates {
			if hit.VectorScore == nil || *hit.VectorScore >= req.ScoreThreshold {
				kept = append(kept, hit)
			}
		}
		output.FilteredByThreshold = len(output.Candidates) - len(kept)
		output.Threshold = req.ScoreThreshold
		// 重新编号，保证 Rank 连续
		for i := range kept {
			kept[i].Rank = i + 1
		}
		output.Candidates = kept
		if len(output.Candidates) == 0 {
			return output, nil
		}
	}

	// 第二阶段：重排。失败与「未配置」是两回事，必须分开处理。
	shouldRerank := r.rerankEnabled
	if req.Rerank != nil {
		shouldRerank = *req.Rerank
	}
	if !shouldRerank {
		output.RerankSkipped = "重排已被关闭"
		output.Hits = topHits(output.Candidates, topK)
		return output, nil
	}

	reranker, err := r.models.Reranker(ctx, model.ScopeRef{DatasetID: req.DatasetID})
	if err != nil {
		// 「没配重排」不是检索失败：跳过并用向量顺序返回，同时说明原因。
		// 其它错误（比如绑定了但凭证解不开）必须暴露——那属于配置坏了。
		if errors.Is(err, model.ErrNotConfigured) {
			output.RerankSkipped = "未配置重排模型"
			output.Hits = topHits(output.Candidates, topK)
			return output, nil
		}
		return RetrieveOutput{}, err
	}

	texts := make([]string, 0, len(output.Candidates))
	for _, hit := range output.Candidates {
		texts = append(texts, hit.Content)
	}
	scores, err := reranker.Rerank(ctx, query, texts)
	if err != nil {
		if r.rerankStrict {
			// strict 档：使用者的诉求是「宁可失败也要精排质量」，直接报错。
			return RetrieveOutput{}, fmt.Errorf("重排失败: %w", err)
		}
		// auto 档：重排是可选组件，它挂了不该把检索一起拖下水，但降级必须**可见**——
		// 响应里写明原因（rerankSkipped），日志里留下错误，不能静默当成「重排没效果」。
		output.RerankSkipped = fmt.Sprintf("重排服务不可用，已按向量顺序返回：%v", err)
		output.Hits = topHits(output.Candidates, topK)
		return output, nil
	}
	if len(scores) != len(output.Candidates) {
		return RetrieveOutput{}, fmt.Errorf("重排返回的分数数量（%d）与候选数（%d）不一致",
			len(scores), len(output.Candidates))
	}

	reranked := make([]Hit, 0, len(output.Candidates))
	for i, hit := range output.Candidates {
		score := scores[i]
		hit.RerankScore = &score
		reranked = append(reranked, hit)
	}
	// 按重排分数降序；分数相同时保持向量顺序，保证结果稳定可复现。
	sort.SliceStable(reranked, func(i, j int) bool {
		return derefFloat(reranked[i].RerankScore) > derefFloat(reranked[j].RerankScore)
	})

	output.RerankModel = reranker.Name()
	output.Hits = topHits(reranked, topK)
	return output, nil
}

// topHits 截取前 n 条并重排 Rank（Rank 必须是连续的 1..n）。
func topHits(hits []Hit, n int) []Hit {
	if n > len(hits) {
		n = len(hits)
	}
	result := make([]Hit, 0, n)
	for i := 0; i < n; i++ {
		hit := hits[i]
		hit.Rank = i + 1
		result = append(result, hit)
	}
	return result
}

// derefFloat 读取可能为空的分数，空值按 0 处理（排序用）。
func derefFloat(value *float32) float32 {
	if value == nil {
		return 0
	}
	return *value
}
