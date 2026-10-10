package repository

import (
	"context"

	"einoflow-backend/internal/model"
)

// RetrievalRepository 描述检索留痕的持久化能力。
//
// 只包含写入能力：留痕是检索链路的副产品，必须在检索发生时立即写，
// 读取（检索历史页面）按阶段 4 再补，写入侧先落地才能保证「记录不留则无法补」这条约束不被违反。
type RetrievalRepository interface {
	// CreateRun 写入一次检索记录，并回填 ID（命中记录要用它做关联）。
	CreateRun(ctx context.Context, run *model.RetrievalRun) error
	// CreateHits 批量写入命中片段。
	CreateHits(ctx context.Context, hits []*model.RetrievalHit) error
}
