package model

import (
	"time"

	"gorm.io/gorm"
)

// DatasetStatus 描述知识库的处理状态。
type DatasetStatus string

const (
	DatasetStatusActive   DatasetStatus = "active"   // 可用
	DatasetStatusIndexing DatasetStatus = "indexing" // 正在索引
	DatasetStatusError    DatasetStatus = "error"    // 索引异常
)

// IsValid 校验知识库状态取值。
func (s DatasetStatus) IsValid() bool {
	switch s {
	case DatasetStatusActive, DatasetStatusIndexing, DatasetStatusError:
		return true
	default:
		return false
	}
}

// Dataset 是一个知识库。
type Dataset struct {
	ID          uint64
	Name        string
	Description string
	// EmbeddingModel 记录该知识库使用的向量模型；更换模型后必须重建索引，
	// 因此把它落库而不是只放配置里，便于后续做索引版本管理。
	EmbeddingModel string
	Status         DatasetStatus
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      gorm.DeletedAt `gorm:"index"`
}

// DocumentStatus 描述文档的向量化状态。
type DocumentStatus string

const (
	DocumentStatusPending  DocumentStatus = "pending"  // 待处理
	DocumentStatusIndexing DocumentStatus = "indexing" // 解析 / 向量化中
	DocumentStatusReady    DocumentStatus = "ready"    // 已可检索
	DocumentStatusFailed   DocumentStatus = "failed"   // 处理失败
)

// Document 是知识库里的一份原始文档。
type Document struct {
	ID           uint64
	DatasetID    uint64
	Name         string
	Source       string // 原始文件在对象存储（MinIO）中的 key
	FileSize     int64
	Status       DocumentStatus
	SegmentCount int
	ErrorMessage string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    gorm.DeletedAt `gorm:"index"`
}

// Segment 是文档切分后的一个片段。
// 片段文本存 PostgreSQL，向量存 Milvus，通过 VectorID 关联，便于删除与重建索引。
type Segment struct {
	ID         uint64
	DatasetID  uint64
	DocumentID uint64
	// ChunkIndex 是分段在文档中的顺序，从 0 开始。
	// 列名不用 position：它是 SQL 标准关键字，部分数据库需要加引号才能当列名用。
	ChunkIndex int `gorm:"column:chunk_index"`
	Content    string
	TokenCount int
	// VectorID 是 Milvus 中的向量主键；向量阶段落地前为空。
	VectorID string
	// 以下字段用于引用溯源与上下文展示。
	Page    int
	Locator string
	Kind    string
	// TitlePath 是所属标题路径，用 JSONB 存结构化数组（GORM 的 serializer 负责编解码）。
	TitlePath []string `gorm:"serializer:json"`
	CreatedAt time.Time
}

// TableName 显式声明表名：模型名 Segment 与表名 document_segments 不一致，
// 写出来避免依赖 GORM 的复数化推断（推断错会查询到不存在的表）。
func (Segment) TableName() string {
	return "document_segments"
}
