package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

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

// UpdateParseState 更新解析阶段的状态与产物引用。
func (r *DocumentRepository) UpdateParseState(
	ctx context.Context, id uint64, status model.DocumentStatus,
	parsedKey string, charCount int, errorMessage string,
) error {
	result := r.client.DB(ctx).
		Model(&model.Document{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":        string(status),
			"parsed_key":    parsedKey,
			"char_count":    charCount,
			"error_message": errorMessage,
			"updated_at":    time.Now(),
		})
	if result.Error != nil {
		return fmt.Errorf("更新文档解析状态失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return repository.ErrNotFound
	}
	return nil
}

// UpdateIndexState 更新入库阶段的状态、分段数与分段模式。
//
// 用 map 而不是 Updates(struct)：GORM 的 Updates(struct) 会跳过零值，
// 而 segment_count=0、chunk_mode="" 在这里都是合法值（失败的文档要清零）。
func (r *DocumentRepository) UpdateIndexState(
	ctx context.Context, id uint64, status model.DocumentStatus,
	segmentCount int, chunkMode string, errorMessage string,
) error {
	result := r.client.DB(ctx).
		Model(&model.Document{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":        string(status),
			"segment_count": segmentCount,
			"chunk_mode":    chunkMode,
			"error_message": errorMessage,
			"updated_at":    time.Now(),
		})
	if result.Error != nil {
		return fmt.Errorf("更新文档状态失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return repository.ErrNotFound
	}
	return nil
}

// SoftDelete 软删除文档，并**同步删除它的分段**（同一事务）。
//
// 分段必须一起删：文档被软删除后，检索侧虽然按「文档已就绪且未删除」过滤，
// 但残留在库里的分段会继续占空间，也让「按 chunk_id 复查」查到已删除内容。
// 向量侧由调用方（service）负责清理，跨库无法放进同一个事务。
func (r *DocumentRepository) SoftDelete(ctx context.Context, id uint64) error {
	return r.client.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := r.client.DB(txCtx).
			Where("document_id = ?", id).
			Delete(&model.Segment{}).Error; err != nil {
			return fmt.Errorf("删除文档分段失败: %w", err)
		}
		result := r.client.DB(txCtx).Where("id = ?", id).Delete(&model.Document{})
		if result.Error != nil {
			return fmt.Errorf("软删除文档失败: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return repository.ErrNotFound
		}
		return nil
	})
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

// GetByID 读取单个分段，并限制在指定知识库内。
func (r *SegmentRepository) GetByID(
	ctx context.Context, datasetID uint64, segmentID uint64,
) (*model.Segment, error) {
	var segment model.Segment
	err := r.client.DB(ctx).
		Where("id = ? AND dataset_id = ?", segmentID, datasetID).
		First(&segment).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("读取分段失败: %w", err)
	}
	return &segment, nil
}

// UpdateKeywords 覆盖单个分段的关键词。
//
// 用 Select 显式限定更新列：GORM 的 Updates(struct) 会忽略零值字段，
// 关键词是空数组时会被整列跳过，表现为「清空关键词无效」。
func (r *SegmentRepository) UpdateKeywords(
	ctx context.Context, datasetID uint64, segmentID uint64, keywords []string,
) error {
	if keywords == nil {
		keywords = []string{}
	}
	result := r.client.DB(ctx).
		Model(&model.Segment{}).
		Where("id = ? AND dataset_id = ?", segmentID, datasetID).
		Select("keywords").
		Updates(&model.Segment{Keywords: keywords})
	if result.Error != nil {
		return fmt.Errorf("更新分段关键词失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return repository.ErrNotFound
	}
	return nil
}

// DeleteByDocument 删除某文档的全部分段。
func (r *SegmentRepository) DeleteByDocument(ctx context.Context, documentID uint64) (int64, error) {
	result := r.client.DB(ctx).Where("document_id = ?", documentID).Delete(&model.Segment{})
	if result.Error != nil {
		return 0, fmt.Errorf("删除文档分段失败: %w", result.Error)
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

// ListForRetrieval 按 chunk_id 批量读取分段，并 JOIN 文档过滤掉不可检索的记录。
func (r *SegmentRepository) ListForRetrieval(
	ctx context.Context, datasetID uint64, chunkIDs []string,
) ([]repository.RetrievedSegment, error) {
	if len(chunkIDs) == 0 {
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
		Where("s.chunk_id IN ?", chunkIDs).
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
