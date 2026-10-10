// Package model 定义模型能力的抽象与解析入口。
//
// **接口只覆盖当前真正被调用的能力。** 现在只有 Embedder，因为只有 RAG 入库与检索需要它；
// Reranker 与 Chat 会在各自的调用方（检索重排、对话）落地时再加，
// 避免现在为「将来可能用到」的调用方设计一堆用不上的参数。
//
// 实现分两类：
//   - 厂商实现放在本包下的子包（如 `ollama`）；
//   - 从配置解析出「当前该用哪个模型」放在 resolver.go。
//
// 阶段 2 会把解析来源从配置文件换成数据库，**调用方不需要改**。
package model

import (
	"context"
	"errors"
)

// 领域错误。
var (
	// ErrNotConfigured 表示所需的模型尚未配置。
	ErrNotConfigured = errors.New("模型未配置")
	// ErrDimensionMismatch 表示模型输出维度与已建索引不一致。
	// 这种情况必须显式失败：维度不匹配的向量写进索引后，检索结果会变成一堆噪声，且极难排查。
	ErrDimensionMismatch = errors.New("向量维度与索引不一致")
	// ErrRerankUnsupported 表示该厂商不提供重排能力。
	//
	// 单独一个错误而不是复用 ErrNotConfigured：两者修法不同——
	// 「没配」要去配置页填，「不支持」得换厂商（实测 Ollama 就没有 rerank 端点）。
	ErrRerankUnsupported = errors.New("该厂商不支持重排")
)

// Embedder 把文本转换为向量。
type Embedder interface {
	// Name 返回模型标识（例如 "bge-m3:latest"）。
	// 它会随切片结果一起落库：换模型意味着向量空间变了，必须能据此判断要重建索引。
	Name() string
	// Dimension 返回向量维度，用于创建与校验向量集合。
	Dimension() int
	// Embed 批量向量化。返回结果与入参一一对应，顺序一致。
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// Reranker 对候选文档按与查询的相关性重新打分。
//
// 返回值与入参**下标一一对应**（不做排序）：排序是调用方的职责。
// 这样接口只描述「打分」这一件事，调用方既能拿到分数排序，也能把分数落库留痕。
type Reranker interface {
	// Name 返回模型标识（例如 BAAI/bge-reranker-v2-m3）。
	Name() string
	// Rerank 返回与 documents 等长的相关性分数，分数越大越相关。
	Rerank(ctx context.Context, query string, documents []string) ([]float32, error)
}

// ScopeRef 指定「为谁解析模型」。
//
// 三级覆盖与 Dify 一致：知识库 > 应用 > 全局（见 docs/PROGRESS.md D3）。
// 零值表示全局默认，因此调用方只在确有覆盖需求时才填 ID。
type ScopeRef struct {
	AppID     uint64
	DatasetID uint64
}

// Resolver 提供当前生效的模型实例。
//
// 之所以是接口：解析来源要从「配置文件」演进到「数据库 + 应用级覆盖」，
// 调用方只依赖本接口，换实现时不用改。
type Resolver interface {
	// Embedder 返回该作用域下生效的向量化模型。
	//
	// 带 scope 而不是无参：知识库可以用自己的向量模型（同 Dify），
	// 而入库与检索必须用**同一个**模型，否则两边向量不在同一空间，检索会退化成噪声。
	Embedder(ctx context.Context, scope ScopeRef) (Embedder, error)
	// Reranker 返回该作用域下生效的重排模型。
	//
	// 未配置时返回 ErrNotConfigured：调用方据此**跳过重排并说明原因**，
	// 而不是把「没配重排」当成检索失败。
	Reranker(ctx context.Context, scope ScopeRef) (Reranker, error)
}
