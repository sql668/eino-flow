package repository

import (
	"context"

	"einoflow-backend/internal/model"
)

// DocumentListFilter 是文档列表的查询条件。
type DocumentListFilter struct {
	DatasetID uint64
	Status    model.DocumentStatus
	Offset    int
	Limit     int
}

// DocumentRepository 描述文档元数据的持久化能力。
type DocumentRepository interface {
	Create(ctx context.Context, doc *model.Document) error
	GetByID(ctx context.Context, id uint64) (*model.Document, error)
	List(ctx context.Context, filter DocumentListFilter) ([]*model.Document, int64, error)
	// UpdateIndexState 更新解析状态、分段数与失败原因。
	// 单独一个方法而不是整行 Update：状态流转是并发发生的，
	// 只更新这几个字段可以避免把其它并发修改覆盖掉。
	UpdateIndexState(ctx context.Context, id uint64, status model.DocumentStatus, segmentCount int, errorMessage string) error
	SoftDelete(ctx context.Context, id uint64) error
}

// SegmentListFilter 是分段列表的查询条件。
type SegmentListFilter struct {
	DocumentID uint64
	Offset     int
	Limit      int
}

// RetrievedSegment 是检索命中的分段及其所属文档的展示信息。
//
// 带上文档名是因为检索结果要展示「这段话出自哪份文档」，
// 让上层再查一次文档会变成 N+1 次查询，所以在这一层用一次 JOIN 解决。
type RetrievedSegment struct {
	Segment      *model.Segment
	DocumentName string
}

// SegmentRepository 描述文档分段的持久化能力。
type SegmentRepository interface {
	// CreateBatch 批量写入分段。逐条插入在几百段规模下会很慢，因此按批提交。
	CreateBatch(ctx context.Context, segments []*model.Segment) error
	// List 按顺序分页读取分段。
	List(ctx context.Context, filter SegmentListFilter) ([]*model.Segment, int64, error)
	// DeleteByDocument 删除某文档的全部分段，返回删除条数。重新解析前先清空旧分段。
	DeleteByDocument(ctx context.Context, documentID uint64) (int64, error)
	// AttachVectorIDs 为某文档的全部分段回填 vector_id。
	AttachVectorIDs(ctx context.Context, documentID uint64) (int64, error)
	// ListForRetrieval 按主键批量读取分段，用于把向量命中还原成可读内容。
	//
	// 两个语义必须由实现保证，不能交给调用方：
	//   - 只返回 datasetID 下的分段，避免跨知识库串数据；
	//   - 只返回「所属文档已就绪且未删除」的分段。文档删除后分段不一定立刻清理，
	//     检索若不过滤就会把已删除文档的内容答给用户。
	// 返回顺序不保证与入参一致，调用方按 SegmentID 自行索引。
	ListForRetrieval(ctx context.Context, datasetID uint64, ids []uint64) ([]RetrievedSegment, error)
}
