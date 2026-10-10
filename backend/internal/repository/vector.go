package repository

import "context"

// VectorRecord 是一条待写入向量库的记录。
//
// 向量与文本分离存放：向量在 Milvus（只做相似度计算），文本与溯源信息在 PostgreSQL。
// 这样切片的元数据改动不需要重建向量，重建向量也不必重写文本。
type VectorRecord struct {
	// SegmentID 同时是向量库里的主键，也是与 PostgreSQL 分段的关联键。
	// 直接用分段主键而不是另生成 UUID：省一次映射，也不会出现「两边对不上」。
	SegmentID uint64
	// DatasetID / DocumentID 是过滤字段：
	// dataset_id 用于把检索限定在单个知识库内；document_id 用于按文档整体删除向量。
	DatasetID  uint64
	DocumentID uint64
	Vector     []float32
}

// VectorMatch 是一次向量检索的命中。
type VectorMatch struct {
	SegmentID uint64
	// Score 是相似度分数（余弦距离下越大越相似）。
	Score float32
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
	// Delete 按分段主键删除向量，用于单段删除。
	Delete(ctx context.Context, segmentIDs []uint64) (int64, error)
	// Search 在指定知识库内做向量相似度检索，返回按相似度降序的命中。
	Search(ctx context.Context, datasetID uint64, vector []float32, topK int) ([]VectorMatch, error)
	// Close 释放连接。
	Close(ctx context.Context) error
}
