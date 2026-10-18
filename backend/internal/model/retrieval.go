package model

import "time"

// 检索参数的领域限制。
//
// 放在 model 而不是 config 或 handler：配置校验、请求校验、兜底默认三处都要用同一个数，
// 各写一份迟早会漂移（改了一处忘了另一处，表现为「配置允许 100，接口只给 50」）。
const (
	// DefaultRetrievalTopK 是请求未指定 top_k 时的默认召回条数。
	DefaultRetrievalTopK = 5
	// MaxCandidateTopK 是重排前召回条数的上限。
	// 召回越多，重排的算力开销与噪声也越大；20~50 是常见区间，这里给到 100 留余量。
	MaxCandidateTopK = 100
	// MaxRetrievalTopK 是 top_k 上限。
	// 检索开销随 top_k 线性增长，且超过这个量级后返回的多是噪声，
	// 限制上限比事后排查「为什么一次检索要 10 秒」便宜。
	MaxRetrievalTopK = 50
)

// RetrievalStage 表示命中发生在检索的哪个阶段。
// 召回阶段按检索方式再细分（向量 / 全文 / 混合），rerank 落地后同一片段会再产生一条 rerank 阶段的命中记录，
// 这样「召回 20 条 → 重排后留 5 条」的完整过程才可回溯。
type RetrievalStage string

const (
	RetrievalStageVector   RetrievalStage = "vector"   // 向量召回
	RetrievalStageFullText RetrievalStage = "fulltext" // 全文（BM25）召回
	RetrievalStageHybrid   RetrievalStage = "hybrid"   // 混合召回（两路融合）
	RetrievalStageRerank   RetrievalStage = "rerank"   // 重排
)

// SearchMode 是检索方式。
//
// 定义在 model 而不是 repository：它同时出现在知识库配置、接口请求
// 与向量库调用上，属于领域词汇。写错一个字符串的表现是「检索悄悄退化」而不是报错，
// 所以三种取值集中在这里，并显式校验。
type SearchMode string

const (
	// SearchModeVector 只做向量（dense）召回，语义相似优先。
	SearchModeVector SearchMode = "vector"
	// SearchModeFullText 只做全文（BM25 稀疏）召回，关键词命中优先。
	SearchModeFullText SearchMode = "fulltext"
	// SearchModeHybrid 两路并行召回后融合，语义与关键词兼顾。
	SearchModeHybrid SearchMode = "hybrid"
)

// IsValid 校验检索方式取值。
func (m SearchMode) IsValid() bool {
	switch m {
	case SearchModeVector, SearchModeFullText, SearchModeHybrid:
		return true
	default:
		return false
	}
}

// HybridRank 是混合检索两路结果的融合方式。
type HybridRank string

const (
	// HybridRankWeighted 按权重加权融合，权重可调、结果可解释。
	HybridRankWeighted HybridRank = "weighted"
	// HybridRankRRF 用倒数排名融合（RRF）：只看名次不看分数，
	// 好处是不用调权重，也不受两路分数量纲不同的影响。
	HybridRankRRF HybridRank = "rrf"
)

// IsValid 校验融合方式取值。
func (r HybridRank) IsValid() bool {
	switch r {
	case HybridRankWeighted, HybridRankRRF:
		return true
	default:
		return false
	}
}

// RetrievalRun 是一次检索请求的记录。
//
// 之所以要落库而不是只打日志：检索质量问题是「事后想复现却复现不出来」的典型场景，
// 记录不留就无法补。同时它是「检索可追溯」这个差异化亮点（见 docs/PROGRESS.md D6）的数据基础。
type RetrievalRun struct {
	ID        uint64
	DatasetID uint64
	Query     string
	// TopK 是本次请求的召回条数。
	TopK int
	// EmbeddingModel 记录本次检索用的向量模型：换模型后向量空间变了，
	// 不记录就无法判断「某天的检索结果为什么变差」。
	EmbeddingModel string
	// SearchMode 是本次使用的检索方式（vector / fulltext / hybrid）。
	// 它与 EmbeddingModel 同理：同一个问题换了检索方式，结果必然不同，
	// 不留痕就没法解释「昨天搜得到、今天搜不到」。
	SearchMode string
	// CandidateCount 是重排前的召回条数；未启用重排时等于命中数。
	// 记录它是为了还原「召回 20 条 → 重排后留 5 条」这个两阶段过程。
	CandidateCount int
	// RerankModel 是本次使用的重排模型；为空表示未做重排。
	RerankModel string
	// LatencyMS 是检索耗时（毫秒），用于观察性能退化。
	LatencyMS int
	// HitCount 是本次最终返回的命中条数。
	HitCount int
	// ErrorMessage 记录失败原因；成功时为空。
	// 失败的检索同样要留痕，否则「用户说搜不到」时无据可查。
	ErrorMessage string
	// ConfigSnapshot 是本次检索的完整参数快照（检索方式、融合、阈值、重排等）。
	//
	// 旧记录没有快照时是空对象：查询方据此显示"当时未记录"，
	// 不能用当前知识库配置反推历史，否则历史现象会被现在的设置改写。
	ConfigSnapshot RetrievalRunConfig `gorm:"serializer:json"`
	CreatedAt      time.Time
}

// RetrievalRunConfig 是一次检索的参数快照。
type RetrievalRunConfig struct {
	Mode           string  `json:"mode"`
	HybridRank     string  `json:"hybridRank,omitempty"`
	TopK           int     `json:"topK"`
	CandidateTopK  int     `json:"candidateTopK"`
	ScoreThreshold float32 `json:"scoreThreshold"`
	// FilteredByThreshold 是被相似度阈值筛掉的条数。
	// 历史页要解释"为什么只剩 1 条"，只给阈值不够，还要知道筛掉了多少。
	FilteredByThreshold int     `json:"filteredByThreshold"`
	Rerank              bool    `json:"rerank"`
	VectorWeight        float64 `json:"vectorWeight,omitempty"`
	FullTextWeight      float64 `json:"fullTextWeight,omitempty"`
	RerankModel         string  `json:"rerankModel,omitempty"`
	RerankSkipped       string  `json:"rerankSkipped,omitempty"`
}

// RetrievalHit 是一次检索中的一个命中片段。
type RetrievalHit struct {
	ID        uint64
	RunID     uint64
	DatasetID uint64
	// SegmentID / DocumentID 指向命中的分段与文档，用于跳转查看原文。
	SegmentID  uint64
	DocumentID uint64
	// Rank 是该片段在本阶段的排名，从 1 开始。
	Rank int
	// Score 是本阶段的排序分（单路是那一路的分数，混合是融合分，重排是重排分）。
	Score float32
	// VectorScore / FullTextScore 是两路各自的分数；该路未参与本次检索时为空。
	// 它们与 Score 的区别是「原始分」与「排序分」：混合检索下两者不同，
	// 分开记录才能回答「这条是靠语义还是靠关键词进来的」。
	VectorScore *float32
	// 列名是 fulltext_score（见 0010 迁移），不是 GORM 默认推断的 full_text_score；
	// 不显式指定会导致每次留痕写入都报「列不存在」，而且只有查日志才能发现。
	FullTextScore *float32 `gorm:"column:fulltext_score"`
	// RerankScore 是重排分数，未做重排时为空。
	RerankScore *float32
	Stage       RetrievalStage
	CreatedAt   time.Time
}

// TableName 显式声明表名，避免依赖 GORM 的复数化推断。
func (RetrievalRun) TableName() string { return "retrieval_runs" }

// TableName 显式声明表名。
func (RetrievalHit) TableName() string { return "retrieval_hits" }
