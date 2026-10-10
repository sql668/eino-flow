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

// Resolver 提供当前生效的模型实例。
//
// 之所以是接口：解析来源要从「配置文件」演进到「数据库 + 应用级覆盖」，
// 调用方只依赖本接口，换实现时不用改。
type Resolver interface {
	// Embedder 返回当前生效的向量化模型。
	Embedder(ctx context.Context) (Embedder, error)
}
