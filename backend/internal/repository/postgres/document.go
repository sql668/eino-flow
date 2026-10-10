package postgres

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"einoflow-backend/internal/model"
	"einoflow-backend/internal/repository"
)

// 分段批量写入的批次大小。太大容易触及单条 SQL 的长度上限，太小则往返次数多。
const segmentBatchSize = 200

// DocumentRepository 是基于 PostgreSQL（GORM）的实现。
type DocumentRepository struct {
	client *Client
}

// NewDocumentRepository 构造文档仓储。
func NewDocumentRepository(client *Client) *DocumentRepository {
	return &DocumentRepository{client: client}
}

// Create 写入文档元数据，并回填 ID 与时间戳。
func (r *DocumentRepository) Create(ctx context.Context, doc *model.Document) error {
	if err := r.client.DB(ctx).Create(doc).Error; err != nil {
		return fmt.Errorf("创建文档记录失败: %w", err)
	}
	return nil
}

// GetByID 按主键查询；不存在或已软删除时返回 repository.ErrNotFound。
func (r *DocumentRepository) GetByID(ctx context.Context, id uint64) (*model.Document, error) {
	var doc model.Document
	if err := r.client.DB(ctx).Where("id = ?", id).First(&doc).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("查询文档失败: %w", err)
	}
	return &doc, nil
}

// List 按条件分页查询文档。
func (r *DocumentRepository) List(ctx context.Context, filter repository.DocumentListFilter) ([]*model.Document, int64, error) {
	build := func() *gorm.DB {
		query := r.client.DB(ctx).Model(&model.Document{})
		if filter.DatasetID != 0 {
			query = query.Where("dataset_id = ?", filter.DatasetID)
		}
		if filter.Status != "" {
			query = query.Where("status = ?", string(filter.Status))
		}
		return query
	}

	var total int64
	if err := build().Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("统计文档数量失败: %w", err)
	}
	if total == 0 {
		return []*model.Document{}, 0, nil
	}

	var rows []model.Document
	if err := build().Order("id DESC").Limit(filter.Limit).Offset(filter.Offset).Find(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("查询文档列表失败: %w", err)
	}

	docs := make([]*model.Document, 0, len(rows))
	for i := range rows {
		docs = append(docs, &rows[i])
	}
	return docs, total, nil
}

// UpdateIndexState 更新解析状态、分段数与失败原因。
func (r *DocumentRepository) UpdateIndexState(
	ctx context.Context, id uint64, status model.DocumentStatus, segmentCount int, errorMessage string,
) error {
	result := r.client.DB(ctx).
		Model(&model.Document{}).
		Where("id = ?", id).
		Select("status", "segment_count", "error_message").
		Updates(&model.Document{
			Status:       status,
			SegmentCount: segmentCount,
			ErrorMessage: errorMessage,
		})
	if result.Error != nil {
		return fmt.Errorf("更新文档状态失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return repository.ErrNotFound
	}
	return nil
}

// SoftDelete 软删除文档。
func (r *DocumentRepository) SoftDelete(ctx context.Context, id uint64) error {
	result := r.client.DB(ctx).Where("id = ?", id).Delete(&model.Document{})
	if result.Error != nil {
		return fmt.Errorf("删除文档失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return repository.ErrNotFound
	}
	return nil
}

// SegmentRepository 是基于 PostgreSQL（GORM）的实现。
type SegmentRepository struct {
	client *Client
}

// NewSegmentRepository 构造分段仓储。
func NewSegmentRepository(client *Client) *SegmentRepository {
	return &SegmentRepository{client: client}
}

// CreateBatch 批量写入分段。
func (r *SegmentRepository) CreateBatch(ctx context.Context, segments []*model.Segment) error {
	if len(segments) == 0 {
		return nil
	}
	if err := r.client.DB(ctx).CreateInBatches(segments, segmentBatchSize).Error; err != nil {
		return fmt.Errorf("写入文档分段失败: %w", err)
	}
	return nil
}

// List 按 chunk_index 顺序分页读取分段。
func (r *SegmentRepository) List(ctx context.Context, filter repository.SegmentListFilter) ([]*model.Segment, int64, error) {
	build := func() *gorm.DB {
		return r.client.DB(ctx).Model(&model.Segment{}).Where("document_id = ?", filter.DocumentID)
	}

	var total int64
	if err := build().Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("统计分段数量失败: %w", err)
	}
	if total == 0 {
		return []*model.Segment{}, 0, nil
	}

	var rows []model.Segment
	if err := build().Order("chunk_index ASC").Limit(filter.Limit).Offset(filter.Offset).Find(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("查询分段列表失败: %w", err)
	}

	segments := make([]*model.Segment, 0, len(rows))
	for i := range rows {
		segments = append(segments, &rows[i])
	}
	return segments, total, nil
}

// DeleteByDocument 删除某文档的全部分段。
func (r *SegmentRepository) DeleteByDocument(ctx context.Context, documentID uint64) (int64, error) {
	result := r.client.DB(ctx).Where("document_id = ?", documentID).Delete(&model.Segment{})
	if result.Error != nil {
		return 0, fmt.Errorf("删除文档分段失败: %w", result.Error)
	}
	return result.RowsAffected, nil
}

// AttachVectorIDs 为某文档的全部分段回填 vector_id。
//
// 向量主键就是分段主键，所以这里用一条 UPDATE 把 id 转成文本写回，
// 而不是逐段回写：几百段的文档逐条更新会产生几百次往返。
func (r *SegmentRepository) AttachVectorIDs(ctx context.Context, documentID uint64) (int64, error) {
	result := r.client.DB(ctx).
		Model(&model.Segment{}).
		Where("document_id = ?", documentID).
		Update("vector_id", gorm.Expr("id::text"))
	if result.Error != nil {
		return 0, fmt.Errorf("回填分段向量标识失败: %w", result.Error)
	}
	return result.RowsAffected, nil
}

// retrievedSegmentRow 是检索关联查询的扫描结构。
//
// Segment 用匿名嵌入让 GORM 把它的字段平铺到本结构上，省去逐字段赋值。
type retrievedSegmentRow struct {
	model.Segment
	DocumentName string `gorm:"column:document_name"`
}

// ListForRetrieval 按主键批量读取分段，并 JOIN 文档过滤掉不可检索的记录。
func (r *SegmentRepository) ListForRetrieval(
	ctx context.Context, datasetID uint64, ids []uint64,
) ([]repository.RetrievedSegment, error) {
	if len(ids) == 0 {
		return []repository.RetrievedSegment{}, nil
	}

	var rows []retrievedSegmentRow
	// 只保留「文档未删除且已就绪」的分段：文档删除时分段不保证即时清理，
	// 不过滤就会把已删除文档的内容当作答案素材返回。
	if err := r.client.DB(ctx).
		Table("document_segments AS s").
		Select("s.*, d.name AS document_name").
		Joins("JOIN documents AS d ON d.id = s.document_id").
		Where("s.dataset_id = ?", datasetID).
		Where("s.id IN ?", ids).
		Where("d.deleted_at IS NULL AND d.status = ?", string(model.DocumentStatusReady)).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("读取检索命中分段失败: %w", err)
	}

	items := make([]repository.RetrievedSegment, 0, len(rows))
	for i := range rows {
		items = append(items, repository.RetrievedSegment{
			Segment:      &rows[i].Segment,
			DocumentName: rows[i].DocumentName,
		})
	}
	return items, nil
}
