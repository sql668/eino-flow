package model

import "time"

// MessageRole 是消息的角色。
type MessageRole string

const (
	MessageRoleSystem    MessageRole = "system"
	MessageRoleUser      MessageRole = "user"
	MessageRoleAssistant MessageRole = "assistant"
	MessageRoleTool      MessageRole = "tool"
)

// IsValid 校验消息角色取值。
func (r MessageRole) IsValid() bool {
	switch r {
	case MessageRoleSystem, MessageRoleUser, MessageRoleAssistant, MessageRoleTool:
		return true
	default:
		return false
	}
}

// Conversation 是一次会话。
type Conversation struct {
	ID        uint64
	AppID     uint64
	Title     string
	UserID    string // V0.1 暂无多用户体系，先预留字段
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Message 是会话里的一条消息。
type Message struct {
	ID             uint64
	ConversationID uint64
	Role           MessageRole
	Content        string
	// TokenCount / LatencyMS / TraceID 服务于后续的可观测能力（V2.0 全链路追踪），
	// 现在就落库，避免以后改表。
	TokenCount int
	// LatencyMS 显式指定列名：GORM 对 "MS" 这类连续大写字母的推断不保证符合预期。
	LatencyMS int `gorm:"column:latency_ms"`
	TraceID   string
	CreatedAt time.Time
}
