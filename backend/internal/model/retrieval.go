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
// 现在只有向量召回；rerank 落地后同一片段会再产生一条 rerank 阶段的命中记录，
// 这样「召回 20 条 → 重排后留 5 条」的完整过程才可回溯。
type RetrievalStage string

const (
	RetrievalStageVector RetrievalStage = "vector" // 向量召回
	RetrievalStageRerank RetrievalStage = "rerank" // 重排（未落地）
)

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
	CreatedAt    time.Time
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
	// Score 是向量相似度（余弦，越大越相似）。
	Score float32
	// RerankScore 是重排分数，rerank 落地前为空。
	RerankScore *float32
	Stage       RetrievalStage
	CreatedAt   time.Time
}

// TableName 显式声明表名，避免依赖 GORM 的复数化推断。
func (RetrievalRun) TableName() string { return "retrieval_runs" }

// TableName 显式声明表名。
func (RetrievalHit) TableName() string { return "retrieval_hits" }
