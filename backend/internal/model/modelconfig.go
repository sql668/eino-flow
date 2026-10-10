package model

import "time"

// ModelType 是模型的能力类型。
//
// 用「能力」而不是「模型名」区分：同一个模型名在不同厂商下能力不同
// （比如 bge-m3 在 Ollama 下是 embedding），配置里必须显式声明它被当作什么用。
type ModelType string

const (
	ModelTypeLLM       ModelType = "llm"       // 对话 / 生成
	ModelTypeEmbedding ModelType = "embedding" // 向量化
	ModelTypeRerank    ModelType = "rerank"    // 重排
)

// IsValid 校验模型类型取值。
func (t ModelType) IsValid() bool {
	switch t {
	case ModelTypeLLM, ModelTypeEmbedding, ModelTypeRerank:
		return true
	default:
		return false
	}
}

// ModelScope 是模型绑定的作用域。
//
// 三级覆盖关系与 Dify 一致：知识库 > 应用 > 全局。
// 之所以要支持覆盖：不同应用的质量 / 成本诉求不同（演示用大模型、批量任务用小模型），
// 而凭证通常只有一份。
type ModelScope string

const (
	ModelScopeGlobal  ModelScope = "global"  // 全局默认
	ModelScopeApp     ModelScope = "app"     // 应用级覆盖
	ModelScopeDataset ModelScope = "dataset" // 知识库级覆盖
)

// IsValid 校验作用域取值。
func (s ModelScope) IsValid() bool {
	switch s {
	case ModelScopeGlobal, ModelScopeApp, ModelScopeDataset:
		return true
	default:
		return false
	}
}

// ModelProvider 是某个内置厂商在本地的配置（地址 + 凭证）。
//
// 表名与结构体名不一致（providers vs model_providers），因此显式声明 TableName。
type ModelProvider struct {
	ID          uint64
	Provider    string // 内置厂商标识，取值见 engine/model/providers 注册表
	DisplayName string
	BaseURL     string
	// Credentials 是**密文**。
	// 命名刻意不叫 APIKey：将来一个厂商要多个字段（key + secret + project）时不必改表。
	Credentials string
	Enabled     bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// TableName 显式声明表名。
func (ModelProvider) TableName() string { return "model_providers" }

// ModelInstance 是一个可用的模型。
type ModelInstance struct {
	ID          uint64
	ProviderID  uint64
	ModelName   string
	ModelType   ModelType
	DisplayName string
	Enabled     bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// TableName 显式声明表名。
func (ModelInstance) TableName() string { return "model_instances" }

// ModelBinding 把「某个作用域下的某类能力」指向一个具体模型。
type ModelBinding struct {
	ID    uint64
	Scope ModelScope
	// AppID / DatasetID 按 scope 取值，未使用的一方为 0。
	// 用 0 而不是 NULL：唯一索引里 NULL 不参与比较，会让「同一作用域只能绑一个」失效。
	AppID           uint64
	DatasetID       uint64
	ModelType       ModelType
	ModelInstanceID uint64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// TableName 显式声明表名。
func (ModelBinding) TableName() string { return "model_bindings" }
