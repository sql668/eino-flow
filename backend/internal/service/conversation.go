package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"einoflow-backend/internal/model"
	"einoflow-backend/internal/repository"
)

const maxConversationTitleRunes = 255

// CreateConversationInput 是创建会话的入参。
type CreateConversationInput struct {
	AppID uint64
	Title string
}

// UpdateConversationInput 是更新会话的入参；指针字段用于区分「未传」与「传空」。
type UpdateConversationInput struct {
	Title *string
}

// ListConversationsInput 是会话列表查询入参。
type ListConversationsInput struct {
	AppID    uint64
	Page     int
	PageSize int
}

// ConversationListResult 是会话列表查询结果。
type ConversationListResult struct {
	Items    []*model.Conversation
	Total    int64
	Page     int
	PageSize int
}

// AppendMessageInput 是追加消息的入参。
type AppendMessageInput struct {
	ConversationID uint64
	Role           model.MessageRole
	Content        string
	TokenCount     int
	LatencyMS      int
	TraceID        string
}

// ListMessagesInput 是消息列表查询入参。
type ListMessagesInput struct {
	ConversationID uint64
	Page           int
	PageSize       int
}

// MessageListResult 是消息列表查询结果。
type MessageListResult struct {
	Items    []*model.Message
	Total    int64
	Page     int
	PageSize int
}

// ConversationService 承载会话与消息的业务规则。
type ConversationService struct {
	conversations repository.ConversationRepository
	messages      repository.MessageRepository
	apps          repository.AppRepository
	tx            repository.TxManager
}

// NewConversationService 构造会话服务。
func NewConversationService(
	conversations repository.ConversationRepository,
	messages repository.MessageRepository,
	apps repository.AppRepository,
	tx repository.TxManager,
) *ConversationService {
	return &ConversationService{conversations: conversations, messages: messages, apps: apps, tx: tx}
}

// Create 创建会话。
func (s *ConversationService) Create(ctx context.Context, in CreateConversationInput) (*model.Conversation, error) {
	if in.AppID == 0 {
		return nil, fmt.Errorf("%w: appId 不能为空", ErrInvalidInput)
	}
	// 数据表刻意没有外键，所以在这里显式校验应用存在，避免产生孤儿会话。
	if _, err := s.apps.GetByID(ctx, in.AppID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("%w: 应用 %d 不存在", ErrInvalidInput, in.AppID)
		}
		return nil, err
	}

	title := strings.TrimSpace(in.Title)
	if title == "" {
		// 未指定标题时给个默认值，避免列表里出现空白项。
		title = "新会话"
	}
	if len([]rune(title)) > maxConversationTitleRunes {
		return nil, fmt.Errorf("%w: 会话标题不能超过 %d 个字符", ErrInvalidInput, maxConversationTitleRunes)
	}

	conversation := &model.Conversation{AppID: in.AppID, Title: title}
	if err := s.conversations.Create(ctx, conversation); err != nil {
		return nil, err
	}
	return conversation, nil
}

// Get 查询单个会话。
func (s *ConversationService) Get(ctx context.Context, id uint64) (*model.Conversation, error) {
	conversation, err := s.conversations.GetByID(ctx, id)
	if err != nil {
		return nil, translateError(err)
	}
	return conversation, nil
}

// List 分页查询会话。
func (s *ConversationService) List(ctx context.Context, in ListConversationsInput) (*ConversationListResult, error) {
	page, pageSize := normalizePaging(in.Page, in.PageSize)

	conversations, total, err := s.conversations.List(ctx, repository.ConversationListFilter{
		AppID:  in.AppID,
		Offset: (page - 1) * pageSize,
		Limit:  pageSize,
	})
	if err != nil {
		return nil, err
	}
	return &ConversationListResult{Items: conversations, Total: total, Page: page, PageSize: pageSize}, nil
}

// Update 更新会话标题。
func (s *ConversationService) Update(ctx context.Context, id uint64, in UpdateConversationInput) (*model.Conversation, error) {
	conversation, err := s.conversations.GetByID(ctx, id)
	if err != nil {
		return nil, translateError(err)
	}

	if in.Title != nil {
		title := strings.TrimSpace(*in.Title)
		if title == "" {
			return nil, fmt.Errorf("%w: 会话标题不能为空", ErrInvalidInput)
		}
		if len([]rune(title)) > maxConversationTitleRunes {
			return nil, fmt.Errorf("%w: 会话标题不能超过 %d 个字符", ErrInvalidInput, maxConversationTitleRunes)
		}
		conversation.Title = title
	}

	if err := s.conversations.Update(ctx, conversation); err != nil {
		return nil, translateError(err)
	}
	return conversation, nil
}

// Delete 删除会话及其全部消息。
//
// 两步必须原子完成：只删会话会留下孤儿消息，只删消息会留下空会话。
// 事务边界放在 service 层，仓储只管执行（见 backend/AGENTS.md）。
func (s *ConversationService) Delete(ctx context.Context, id uint64) error {
	// 先确认存在，避免删除不存在的会话时静默返回成功。
	if _, err := s.conversations.GetByID(ctx, id); err != nil {
		return translateError(err)
	}

	return s.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		if _, err := s.messages.DeleteByConversation(txCtx, id); err != nil {
			return err
		}
		return s.conversations.Delete(txCtx, id)
	})
}

// AppendMessage 向会话追加一条消息。
func (s *ConversationService) AppendMessage(ctx context.Context, in AppendMessageInput) (*model.Message, error) {
	if !in.Role.IsValid() {
		return nil, fmt.Errorf("%w: 不支持的消息角色 %q", ErrInvalidInput, in.Role)
	}
	content := strings.TrimSpace(in.Content)
	if content == "" {
		return nil, fmt.Errorf("%w: 消息内容不能为空", ErrInvalidInput)
	}
	// 校验会话存在，避免写入孤儿消息。
	if _, err := s.conversations.GetByID(ctx, in.ConversationID); err != nil {
		return nil, translateError(err)
	}

	message := &model.Message{
		ConversationID: in.ConversationID,
		Role:           in.Role,
		Content:        content,
		TokenCount:     in.TokenCount,
		LatencyMS:      in.LatencyMS,
		TraceID:        in.TraceID,
	}
	if err := s.messages.Create(ctx, message); err != nil {
		return nil, err
	}
	return message, nil
}

// ListMessages 分页拉取会话消息（正序，便于直接渲染对话）。
func (s *ConversationService) ListMessages(ctx context.Context, in ListMessagesInput) (*MessageListResult, error) {
	// 会话不存在时直接 404，而不是返回空列表——让调用方能区分「没有消息」和「会话不存在」。
	if _, err := s.conversations.GetByID(ctx, in.ConversationID); err != nil {
		return nil, translateError(err)
	}

	page, pageSize := normalizePaging(in.Page, in.PageSize)
	messages, total, err := s.messages.List(ctx, repository.MessageListFilter{
		ConversationID: in.ConversationID,
		Offset:         (page - 1) * pageSize,
		Limit:          pageSize,
	})
	if err != nil {
		return nil, err
	}
	return &MessageListResult{Items: messages, Total: total, Page: page, PageSize: pageSize}, nil
}
