package postgres

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"einoflow-backend/internal/model"
	"einoflow-backend/internal/repository"
)

// MessageRepository 是基于 PostgreSQL（GORM）的实现。
type MessageRepository struct {
	client *Client
}

// NewMessageRepository 构造消息仓储。
func NewMessageRepository(client *Client) *MessageRepository {
	return &MessageRepository{client: client}
}

// Create 追加一条消息。
func (r *MessageRepository) Create(ctx context.Context, message *model.Message) error {
	if err := r.client.DB(ctx).Create(message).Error; err != nil {
		return fmt.Errorf("写入消息失败: %w", err)
	}
	return nil
}

// List 按会话分页查询消息，按写入顺序正序返回（对话要按时间读）。
func (r *MessageRepository) List(ctx context.Context, filter repository.MessageListFilter) ([]*model.Message, int64, error) {
	build := func() *gorm.DB {
		return r.client.DB(ctx).Model(&model.Message{}).Where("conversation_id = ?", filter.ConversationID)
	}

	var total int64
	if err := build().Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("统计消息数量失败: %w", err)
	}
	if total == 0 {
		return []*model.Message{}, 0, nil
	}

	var rows []model.Message
	// 正序：对话必须按时间顺序读，与列表页的倒序惯例不同。
	if err := build().Order("id ASC").Limit(filter.Limit).Offset(filter.Offset).Find(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("查询消息列表失败: %w", err)
	}

	messages := make([]*model.Message, 0, len(rows))
	for i := range rows {
		messages = append(messages, &rows[i])
	}
	return messages, total, nil
}

// DeleteByConversation 删除某个会话下的全部消息，返回删除条数。
func (r *MessageRepository) DeleteByConversation(ctx context.Context, conversationID uint64) (int64, error) {
	result := r.client.DB(ctx).
		Where("conversation_id = ?", conversationID).
		Delete(&model.Message{})
	if result.Error != nil {
		return 0, fmt.Errorf("删除会话消息失败: %w", result.Error)
	}
	return result.RowsAffected, nil
}
