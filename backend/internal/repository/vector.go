package repository

import (
	"context"

	"einoflow-backend/internal/model"
)

// VectorRecord 是一条待写入向量库的记录。
//
// 向量与文本分离存放：向量与可过滤元数据在 Milvus，片段正文在 PostgreSQL，
// 两边靠 ChunkID 对应（Milvus 主键就是它，检索拿它回查 PG 取正文）。
//
// 元数据的作用不只是展示：过滤在向量库内完成（按知识库/文档/上传人），
// 不用回查业务库，这是将来做权限隔离与混合检索的基础。
type VectorRecord struct {
	// ChunkID 由业务生成（UUID），是向量库主键，也是与 PostgreSQL 分段的关联键。
	// 用它而不是业务库自增 ID：向量库与业务库各自独立演进，
	// 换向量库、分库或重建索引都不必改主键语义。
	ChunkID string
	// DatasetID / DocumentID 是过滤字段：
	// dataset_id 用于把检索限定在单个知识库内；document_id 用于按文档整体删除向量。
	DatasetID  uint64
	DocumentID uint64
	// UserID 是上传人标识：为「按人可见」这类权限过滤预留。
	UserID string
	// TitlePath 是章节路径（拼接形式），检索结果可直接展示，也可用于过滤。
	TitlePath string
	// Page 是片段起始页码，配合 TitlePath 做溯源展示。
	Page int
	// Kind 是片段类型：text / table。
	Kind string
	// Content 是片段正文。
	//
	// 存进向量库**只为一件事**：BM25 稀疏检索需要服务端掌握词频与 IDF，
	// 拿不到正文就只能自己维护统计（那是另一个需要持续维护的状态）。
	// 业务库仍是正文的权威来源，检索时也不回传这个字段（见 LoadFields）。
	Content string
	// Keywords 是片段关键词。写入时与正文拼成 BM25 的输入文本——
	// 人工调整关键词因此能真实影响全文与混合检索的召回。
	Keywords []string
	// Vector 是文本向量。
	Vector []float32
}

// VectorMatch 是一次向量检索的命中。
type VectorMatch struct {
	// ChunkID 命中片段的主键，回查 PostgreSQL 用它。
	ChunkID string
	// Score 是参与排序的最终分数：单路检索时就是该路的分数，
	// 混合检索时是两路融合后的分数。不同模式的绝对值不可横向比较
	// （余弦相似度在 0~1，BM25 是无上界的打分，融合分又是另一个量纲）。
	Score float32
	// VectorScore 是向量路分数；该路未参与本次检索时为空。
	VectorScore *float32
	// FullTextScore 是全文（BM25）路分数；该路未参与本次检索时为空。
	//
	// 两路分数都往回带，是为了让「混合检索到底是谁把这条提上来的」
	// 在检索留痕与界面上可查——只留一个融合分数等于把这个信息丢掉了。
	FullTextScore *float32
}

// 检索方式与融合方式的取值定义在 model（领域词汇），这里只做别名，
// 让向量库实现的签名短一些，语义与 model 完全一致。
type (
	SearchMode = model.SearchMode
	HybridRank = model.HybridRank
)

const (
	SearchModeVector   = model.SearchModeVector
	SearchModeFullText = model.SearchModeFullText
	SearchModeHybrid   = model.SearchModeHybrid

	HybridRankWeighted = model.HybridRankWeighted
	HybridRankRRF      = model.HybridRankRRF
)

// SearchQuery 是一次检索的查询侧入参。
//
// 把「文本 / 向量 / 融合方式」收在一个结构里，而不是给 Search 加一长串参数：
// 不同模式用到的字段不同（全文不需要向量，向量检索也不需要文本），
// 收成结构后由实现按模式取用，调用方不用记参数顺序。
type SearchQuery struct {
	// Text 是已清洗的查询文本，全文与混合检索用它（切词由向量库的分析器完成）。
	Text string
	// Vector 是查询向量，向量与混合检索用它。
	Vector []float32
	// Mode 是检索方式；空值按向量检索处理。
	Mode SearchMode
	// Rank 是混合检索的融合方式；空值按加权处理。
	Rank HybridRank
	// VectorWeight / FullTextWeight 是加权融合时两路的权重（[0,1]，和不必为 1，
	// 实现会归一化）。仅为 0 时按 0.5 / 0.5 处理。
	VectorWeight   float64
	FullTextWeight float64
}

// SearchFilter 是向量检索的过滤条件。
//
// 过滤在向量库内完成，而不是取回来再筛：先取 TopK 再筛会出现
// 「筛完只剩几条」的塌陷（明明库里有匹配的内容，却因为被筛掉而命中不足）。
type SearchFilter struct {
	DatasetID uint64
	// DocumentIDs / UserIDs 为空表示不限制。
	DocumentIDs []uint64
	UserIDs     []string
	TopK        int
}

// VectorStore 描述向量库的持久化能力。
//
// 接口刻意只包含当前真正需要的方法。新增实现（例如将来的 pgvector 或其它向量库）时，
// 只要满足这些语义即可替换，engine 层不用改。
type VectorStore interface {
	// EnsureCollection 确保集合存在且**维度与入参一致**。
	// 维度不一致必须返回 model.ErrDimensionMismatch 而不是静默重建：
	// 两个不同维度的向量混在一个集合里，检索结果会变成噪声且很难定位原因。
	EnsureCollection(ctx context.Context, dim int) error
	// Upsert 写入或覆盖一批向量（按 SegmentID 主键去重）。
	Upsert(ctx context.Context, records []VectorRecord) error
	// DeleteByDocument 删除某文档的全部向量。
	// 文档重新解析会生成新的分段与新的主键，旧向量必须先清掉，否则检索会命中已删除内容。
	DeleteByDocument(ctx context.Context, documentID uint64) (int64, error)
	// Delete 按片段主键删除向量，用于单段删除。
	Delete(ctx context.Context, chunkIDs []string) (int64, error)
	// Search 按过滤条件做检索，返回按相关性降序的命中。
	// 支持向量 / 全文 / 混合三种方式（见 SearchQuery.Mode）。
	Search(ctx context.Context, filter SearchFilter, query SearchQuery) ([]VectorMatch, error)
	// Close 释放连接。
	Close(ctx context.Context) error
}
