package repository

import (
	"context"
	"time"

	"einoflow-backend/internal/model"
)

// RetrievalRepository 描述检索留痕的持久化能力。
//
// 写入是检索链路的副产品，必须在检索发生时立即完成；
// 读取供检索历史页使用，只读、不触发任何模型调用。
type RetrievalRepository interface {
	// CreateRun 写入一次检索记录，并回填 ID（命中记录要用它做关联）。
	CreateRun(ctx context.Context, run *model.RetrievalRun) error
	// CreateHits 批量写入命中片段。
	CreateHits(ctx context.Context, hits []*model.RetrievalHit) error
	// ListRuns 按知识库与时间范围分页查询检索记录（时间倒序）。
	ListRuns(ctx context.Context, filter RetrievalRunFilter) ([]*model.RetrievalRun, int64, error)
	// GetRun 按知识库取单条检索记录；不存在时返回 ErrNotFound。
	GetRun(ctx context.Context, datasetID, runID uint64) (*model.RetrievalRun, error)
	// ListHitsByRun 批量取多次检索的命中明细（含分段正文与出处）。
	ListHitsByRun(ctx context.Context, runIDs []uint64) ([]*RetrievalHitDetail, error)
}

// RetrievalRunFilter 是检索历史的查询条件。
type RetrievalRunFilter struct {
	DatasetID uint64
	// From / To 为 nil 表示不限；时间范围由调用方按展示时区换算后传入。
	From   *time.Time
	To     *time.Time
	Offset int
	Limit  int
}

// RetrievalHitDetail 是检索命中 + 分段正文与出处（只读投影）。
//
// 检索留痕表只存 ID 与分数；历史页要展示"命中了什么、出自哪一页"，
// 因此读取时关联分段与文档表，而不是把正文再冗余存一份。
type RetrievalHitDetail struct {
	model.RetrievalHit
	DocumentName string
	Content      string
	ChunkIndex   int
	Page         int
	Locator      string
	Kind         string
	TitlePath    []string
}
