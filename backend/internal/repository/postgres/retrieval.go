package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"einoflow-backend/internal/model"
	"einoflow-backend/internal/repository"
)

// RetrievalRepository 是基于 PostgreSQL（GORM）的检索留痕实现。
type RetrievalRepository struct {
	client *Client
}

// NewRetrievalRepository 构造检索留痕仓储。
func NewRetrievalRepository(client *Client) *RetrievalRepository {
	return &RetrievalRepository{client: client}
}

// CreateRun 写入检索记录，GORM 会把自增主键回填到入参对象上。
func (r *RetrievalRepository) CreateRun(ctx context.Context, run *model.RetrievalRun) error {
	if err := r.client.DB(ctx).Create(run).Error; err != nil {
		return fmt.Errorf("写入检索记录失败: %w", err)
	}
	return nil
}

// CreateHits 批量写入命中片段。
func (r *RetrievalRepository) CreateHits(ctx context.Context, hits []*model.RetrievalHit) error {
	if len(hits) == 0 {
		return nil
	}
	if err := r.client.DB(ctx).CreateInBatches(hits, segmentBatchSize).Error; err != nil {
		return fmt.Errorf("写入检索命中记录失败: %w", err)
	}
	return nil
}

// ListRuns 按知识库与时间范围分页查询检索记录。
func (r *RetrievalRepository) ListRuns(
	ctx context.Context, filter repository.RetrievalRunFilter,
) ([]*model.RetrievalRun, int64, error) {
	build := func() *gorm.DB {
		query := r.client.DB(ctx).Model(&model.RetrievalRun{}).
			Where("dataset_id = ?", filter.DatasetID)
		if filter.From != nil {
			query = query.Where("created_at >= ?", *filter.From)
		}
		if filter.To != nil {
			query = query.Where("created_at <= ?", *filter.To)
		}
		return query
	}

	var total int64
	if err := build().Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("统计检索记录失败: %w", err)
	}
	if total == 0 {
		return []*model.RetrievalRun{}, 0, nil
	}

	var rows []model.RetrievalRun
	if err := build().
		Order("created_at DESC, id DESC").
		Limit(filter.Limit).
		Offset(filter.Offset).
		Find(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("查询检索记录失败: %w", err)
	}
	runs := make([]*model.RetrievalRun, 0, len(rows))
	for i := range rows {
		runs = append(runs, &rows[i])
	}
	return runs, total, nil
}

// GetRun 取单条检索记录，并校验它属于该知识库。
func (r *RetrievalRepository) GetRun(
	ctx context.Context, datasetID, runID uint64,
) (*model.RetrievalRun, error) {
	var run model.RetrievalRun
	err := r.client.DB(ctx).
		Where("id = ? AND dataset_id = ?", runID, datasetID).
		First(&run).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("查询检索记录失败: %w", err)
	}
	return &run, nil
}

// ListHitsByRun 批量读取命中明细。
//
// 用 LEFT JOIN：分段或文档被删掉后，历史记录仍然可读（正文为空、出处缺失），
// 不能因为"原文没了"就让整条历史查询失败。
func (r *RetrievalRepository) ListHitsByRun(
	ctx context.Context, runIDs []uint64,
) ([]*repository.RetrievalHitDetail, error) {
	if len(runIDs) == 0 {
		return []*repository.RetrievalHitDetail{}, nil
	}

	type hitRow struct {
		model.RetrievalHit
		DocumentName string `gorm:"column:document_name"`
		Content      string `gorm:"column:content"`
		ChunkIndex   int    `gorm:"column:chunk_index"`
		Page         int    `gorm:"column:page"`
		Locator      string `gorm:"column:locator"`
		Kind         string `gorm:"column:kind"`
		// title_path 是 JSONB；用原始字节接住再解析，避免 Scan 不走 serializer。
		TitlePath []byte `gorm:"column:title_path"`
	}

	var rows []hitRow
	if err := r.client.DB(ctx).
		Table("retrieval_hits AS h").
		Select(`h.*,
			COALESCE(d.name, '') AS document_name,
			COALESCE(s.content, '') AS content,
			COALESCE(s.chunk_index, 0) AS chunk_index,
			COALESCE(s.page, 0) AS page,
			COALESCE(s.locator, '') AS locator,
			COALESCE(s.kind, '') AS kind,
			s.title_path`).
		Joins("LEFT JOIN document_segments s ON s.id = h.segment_id").
		Joins("LEFT JOIN documents d ON d.id = h.document_id").
		Where("h.run_id IN ?", runIDs).
		Order("h.run_id, h.stage, h.rank").
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("查询检索命中失败: %w", err)
	}

	out := make([]*repository.RetrievalHitDetail, 0, len(rows))
	for _, row := range rows {
		titlePath := make([]string, 0)
		if len(row.TitlePath) > 0 {
			// 解析失败不能拖垮整页：标题路径只是展示字段，留空即可。
			_ = json.Unmarshal(row.TitlePath, &titlePath)
		}
		out = append(out, &repository.RetrievalHitDetail{
			RetrievalHit: row.RetrievalHit,
			DocumentName: row.DocumentName,
			Content:      row.Content,
			ChunkIndex:   row.ChunkIndex,
			Page:         row.Page,
			Locator:      row.Locator,
			Kind:         row.Kind,
			TitlePath:    titlePath,
		})
	}
	return out, nil
}
