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

// MessageCitation 是助手消息引用的一条片段。
//
// 只放「展示与回溯」够用的字段：回答里的 [n] 编号、来源文档与页码、最终排序分数。
// 片段正文不冗余存一份——它就在 document_segments 里，按 SegmentID 取即可。
type MessageCitation struct {
	// Index 是提示与回答里的引用编号（从 1 开始）。
	Index        int      `json:"index"`
	SegmentID    uint64   `json:"segmentId"`
	DocumentID   uint64   `json:"documentId"`
	DocumentName string   `json:"documentName"`
	Page         int      `json:"page"`
	TitlePath    []string `json:"titlePath"`
	// Score 是最终用于排序的分数；RerankScore 单独给出，便于展示两阶段差异。
	Score       float32  `json:"score"`
	RerankScore *float32 `json:"rerankScore,omitempty"`
}

// MessageToolCall 是一条消息里记录的一次工具调用。
//
// 入参与结果都只存**摘要**（长度受限）：完整检索结果可能包含整段文档正文，
// 全量塞进消息表会让会话体积失控；完整片段仍可通过 citations 与分段表回查。
type MessageToolCall struct {
	// Round 是本次问答内的调用序号，从 1 开始；同一轮可能并行调用多个工具。
	Round int `json:"round"`
	// CallID 是模型给出的调用 ID；Ollama 等不返回 ID 的实现会由适配器补一个。
	CallID string `json:"callId"`
	Name   string `json:"name"`
	// Arguments / Result 是截断后的摘要。
	Arguments string `json:"arguments"`
	Result    string `json:"result"`
	// Failed 表示工具执行失败；Skipped 表示因达到轮次上限而未执行。
	Failed  bool `json:"failed"`
	Skipped bool `json:"skipped"`
	// DurationMS 是工具执行耗时；被跳过时为 0。
	DurationMS int `json:"durationMs"`
}

// Message 是会话里的一条消息。
type Message struct {
	ID             uint64
	ConversationID uint64
	Role           MessageRole
	Content        string
	// Citations 只在助手消息上有值：本轮回答引用了哪些片段。
	// 用 jsonb 存（GORM serializer），与消息一起读写，不需要额外查询。
	Citations []MessageCitation `gorm:"serializer:json"`
	// ToolCalls 只在助手消息上有值：本轮回答调用过哪些工具。
	// 与 citations 同样用 jsonb 存，刷新会话时可直接回看推理依据。
	ToolCalls []MessageToolCall `gorm:"serializer:json"`
	// RetrievalRunID 指向 retrieval_runs：可以回看当时的召回与重排过程。
	RetrievalRunID uint64
	// TokenCount / LatencyMS / TraceID 服务于后续的可观测能力（V2.0 全链路追踪），
	// 现在就落库，避免以后改表。
	TokenCount int
	// LatencyMS 显式指定列名：GORM 对 "MS" 这类连续大写字母的推断不保证符合预期。
	LatencyMS int `gorm:"column:latency_ms"`
	TraceID   string
	CreatedAt time.Time
}
