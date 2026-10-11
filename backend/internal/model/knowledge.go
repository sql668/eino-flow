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
	// ChunkConfig 是知识库级的分段与清洗配置：新建文档时用它作为默认值。
	// 存 JSONB 而不是拆成十几列：配置项还会继续长（父子分块、更多清洗规则），
	// 每次加一项都改表不划算，而且这些字段从不需要按值过滤。
	ChunkConfig *ChunkConfig `gorm:"serializer:json"`
	// RetrievalConfig 是知识库级的检索配置（TopK / 阈值 / 重排开关 / 权重）。
	// 放在知识库而不是应用上：检索范围与质量参数跟「这批文档」强相关。
	RetrievalConfig *RetrievalConfig `gorm:"serializer:json"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       gorm.DeletedAt `gorm:"index"`
}

// DocumentStatus 描述文档的向量化状态。
type DocumentStatus string

const (
	DocumentStatusPending  DocumentStatus = "pending"  // 排队中
	DocumentStatusParsing  DocumentStatus = "parsing"  // 解析中（MinerU）
	DocumentStatusDraft    DocumentStatus = "draft"    // 已解析，待用户确认分段与清洗
	DocumentStatusIndexing DocumentStatus = "indexing" // 切分 / 向量化中
	DocumentStatusReady    DocumentStatus = "ready"    // 已可检索
	DocumentStatusFailed   DocumentStatus = "failed"   // 处理失败
)

// IsSearchable 判断该状态下的文档是否参与检索。
//
// 只有 ready 参与：draft（待确认）与 parsing 阶段的文档没有可检索的向量，
// 若不显式排除，检索侧的过滤条件会漏掉新状态，导致「半成品文档被检索到」。
func (s DocumentStatus) IsSearchable() bool { return s == DocumentStatusReady }

// IsProcessing 判断文档是否还在后台处理中（用于前端决定是否轮询）。
func (s DocumentStatus) IsProcessing() bool {
	switch s {
	case DocumentStatusPending, DocumentStatusParsing, DocumentStatusIndexing:
		return true
	default:
		return false
	}
}

// Document 是知识库里的一份原始文档。
type Document struct {
	ID        uint64
	DatasetID uint64
	Name      string
	Source    string // 原始文件在对象存储（MinIO）中的 key
	// ParsedKey 是解析产物（IR）在对象存储中的 key。
	//
	// 必须保留：分段工作台要反复调参数看预览，如果每次都重新调用 MinerU 解析
	// （一份 PDF 几十秒到几分钟），「调参」这件事就没法做了。
	ParsedKey string
	FileSize  int64
	// CharCount 是解析出的正文字符数（清洗前），文档列表用它展示体量。
	CharCount int
	// ChunkMode 记录本文档最终采用的分段模式，列表里一眼看出「这份是怎么切的」。
	ChunkMode string
	// UserID 是上传人标识，随分段一起写进向量库元数据（权限过滤预留）。
	UserID       string
	Status       DocumentStatus
	SegmentCount int
	ErrorMessage string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    gorm.DeletedAt `gorm:"index"`
}

// Segment 是文档切分后的一个片段。
//
// 存储分工：
//   - 片段正文与展示/统计字段在 PostgreSQL（本表）；
//   - 向量与可过滤元数据在 Milvus；
//   - 两边靠 ChunkID 对应：Milvus 的主键就是它，检索拿它回查本表取正文。
type Segment struct {
	ID         uint64
	DatasetID  uint64
	DocumentID uint64
	// ChunkIndex 是分段在文档中的顺序，从 0 开始。
	// 列名不用 position：它是 SQL 标准关键字，部分数据库需要加引号才能当列名用。
	ChunkIndex int `gorm:"column:chunk_index"`
	// ChunkID 是业务生成的片段标识（UUID），同时是 Milvus 里的主键。
	//
	// 用它而不是本表的自增 ID 做跨库关联：向量库与业务库各自独立演进，
	// 业务侧生成 ID 后写入两边，将来换向量库、分库或重建索引都不用改主键语义。
	ChunkID string `gorm:"column:chunk_id"`
	Content string
	// CharCount 是片段字符数（按 rune 计）。切片器按长度切分时用它展示实际长度，
	// 也用于工作台的健康度提示（哪些片段贴着上限被强制切开）。
	CharCount  int
	TokenCount int
	// OverlapRunes 记录本片段开头有多少字符是与前一个片段重叠的窗口。
	// 留痕是为了排查「为什么两段内容看起来重复」——它是有意为之，不是写重了。
	OverlapRunes int
	// ContentHash 是片段正文的哈希，用于将来做增量重切（内容没变就不重算向量）。
	ContentHash string
	// ChunkConfigVersion 记录切分时用的配置指纹：换了配置后重新入库，
	// 靠它分辨「哪些片段是旧配置切的」。
	ChunkConfigVersion string
	// UserID 是上传人标识，随分段写入向量库元数据，为按人过滤（权限）留出字段。
	UserID string
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
