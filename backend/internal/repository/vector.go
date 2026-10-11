package repository

import "context"

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
	// Vector 是文本向量。
	Vector []float32
}

// VectorMatch 是一次向量检索的命中。
type VectorMatch struct {
	// ChunkID 命中片段的主键，回查 PostgreSQL 用它。
	ChunkID string
	// Score 是相似度分数（余弦距离下越大越相似）。
	Score float32
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
	// Search 按过滤条件做向量相似度检索，返回按相似度降序的命中。
	Search(ctx context.Context, filter SearchFilter, vector []float32) ([]VectorMatch, error)
	// Close 释放连接。
	Close(ctx context.Context) error
}
