package postgres

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"einoflow-backend/internal/model"
	"einoflow-backend/internal/repository"
)

// ConversationRepository 是基于 PostgreSQL（GORM）的实现。
type ConversationRepository struct {
	client *Client
}

// NewConversationRepository 构造会话仓储。
func NewConversationRepository(client *Client) *ConversationRepository {
	return &ConversationRepository{client: client}
}

// Create 写入新会话。
func (r *ConversationRepository) Create(ctx context.Context, conversation *model.Conversation) error {
	if err := r.client.DB(ctx).Create(conversation).Error; err != nil {
		return fmt.Errorf("创建会话失败: %w", err)
	}
	return nil
}

// GetByID 按主键查询；不存在时返回 repository.ErrNotFound。
func (r *ConversationRepository) GetByID(ctx context.Context, id uint64) (*model.Conversation, error) {
	var conversation model.Conversation
	if err := r.client.DB(ctx).Where("id = ?", id).First(&conversation).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("查询会话失败: %w", err)
	}
	return &conversation, nil
}

// List 按条件分页查询会话。
func (r *ConversationRepository) List(ctx context.Context, filter repository.ConversationListFilter) ([]*model.Conversation, int64, error) {
	build := func() *gorm.DB {
		query := r.client.DB(ctx).Model(&model.Conversation{})
		if filter.AppID != 0 {
			query = query.Where("app_id = ?", filter.AppID)
		}
		return query
	}

	var total int64
	if err := build().Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("统计会话数量失败: %w", err)
	}
	if total == 0 {
		return []*model.Conversation{}, 0, nil
	}

	var rows []model.Conversation
	if err := build().Order("id DESC").Limit(filter.Limit).Offset(filter.Offset).Find(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("查询会话列表失败: %w", err)
	}

	conversations := make([]*model.Conversation, 0, len(rows))
	for i := range rows {
		conversations = append(conversations, &rows[i])
	}
	return conversations, total, nil
}

// Update 更新会话（目前只改标题）。
func (r *ConversationRepository) Update(ctx context.Context, conversation *model.Conversation) error {
	result := r.client.DB(ctx).
		Model(&model.Conversation{}).
		Where("id = ?", conversation.ID).
		Select("title").
		Updates(conversation)
	if result.Error != nil {
		return fmt.Errorf("更新会话失败: %w", result.Error)
	}

	updated, err := r.GetByID(ctx, conversation.ID)
	if err != nil {
		return err
	}
	*conversation = *updated
	return nil
}

// Delete 硬删除会话。
// Conversation 模型没有 DeletedAt 字段，GORM 会执行真正的 DELETE。
// 会话消息由 service 在同一个事务里删除。
func (r *ConversationRepository) Delete(ctx context.Context, id uint64) error {
	result := r.client.DB(ctx).Where("id = ?", id).Delete(&model.Conversation{})
	if result.Error != nil {
		return fmt.Errorf("删除会话失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return repository.ErrNotFound
	}
	return nil
}
