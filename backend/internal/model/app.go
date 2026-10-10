package model

import (
	"time"

	"gorm.io/gorm"
)

// AppMode 表示 AI 应用的形态。
type AppMode string

const (
	AppModeChat     AppMode = "chat"     // 对话应用
	AppModeRAG      AppMode = "rag"      // 知识库问答
	AppModeAgent    AppMode = "agent"    // 智能体
	AppModeWorkflow AppMode = "workflow" // 工作流
)

// IsValid 校验枚举取值，避免把非法值写进数据库。
func (m AppMode) IsValid() bool {
	switch m {
	case AppModeChat, AppModeRAG, AppModeAgent, AppModeWorkflow:
		return true
	default:
		return false
	}
}

// AppStatus 表示应用的发布状态。
type AppStatus string

const (
	AppStatusDraft     AppStatus = "draft"     // 草稿，仅创建者可调试
	AppStatusPublished AppStatus = "published" // 已发布，可对外提供 API
)

// IsValid 校验发布状态取值。
func (s AppStatus) IsValid() bool {
	switch s {
	case AppStatusDraft, AppStatusPublished:
		return true
	default:
		return false
	}
}

// App 是一个 AI 应用。
type App struct {
	ID          uint64
	Name        string
	Description string
	Mode        AppMode
	Status      AppStatus
	Icon        string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	// DeletedAt 用 gorm.DeletedAt：查询会自动过滤已软删除的行，
	// 免得每处查询都手写 deleted_at IS NULL——漏写一处就会把已删除数据查出来。
	DeletedAt gorm.DeletedAt `gorm:"index"`
}
