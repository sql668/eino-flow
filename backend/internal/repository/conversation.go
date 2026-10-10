package repository

import (
	"context"

	"einoflow-backend/internal/model"
)

// ConversationListFilter 是会话列表的查询条件。
type ConversationListFilter struct {
	AppID  uint64 // 0 表示不过滤
	Offset int
	Limit  int
}

// ConversationRepository 描述会话的持久化能力。
type ConversationRepository interface {
	Create(ctx context.Context, conversation *model.Conversation) error
	GetByID(ctx context.Context, id uint64) (*model.Conversation, error)
	List(ctx context.Context, filter ConversationListFilter) ([]*model.Conversation, int64, error)
	Update(ctx context.Context, conversation *model.Conversation) error
	// Delete 硬删除会话。会话消息由调用方在同一个事务里一并删除。
	//
	// 这里刻意不做软删除：会话属于用户可随时清理的数据，长期保留只会带来隐私与体积负担。
	Delete(ctx context.Context, id uint64) error
}

// MessageListFilter 是消息列表的查询条件。
type MessageListFilter struct {
	ConversationID uint64
	Offset         int
	Limit          int
}

// MessageRepository 描述会话消息的持久化能力。
type MessageRepository interface {
	Create(ctx context.Context, message *model.Message) error
	List(ctx context.Context, filter MessageListFilter) ([]*model.Message, int64, error)
	// ListRecent 取最近 limit 条消息，按时间**正序**返回。
	//
	// 单独一个方法而不是用 List + Offset：倒序取最近 N 条要先知道总数，
	// 两次查询既慢，又会在并发写入时取错区间。
	ListRecent(ctx context.Context, conversationID uint64, limit int) ([]*model.Message, error)
	// DeleteByConversation 删除某个会话下的全部消息，返回删除条数。
	DeleteByConversation(ctx context.Context, conversationID uint64) (int64, error)
}
