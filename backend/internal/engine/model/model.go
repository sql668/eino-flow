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
	"fmt"

	einomodel "github.com/cloudwego/eino/components/model"
)

// 领域错误。
var (
	// ErrNotConfigured 表示所需的模型尚未配置。
	ErrNotConfigured = errors.New("模型未配置")
	// ErrDimensionMismatch 表示模型输出维度与已建索引不一致。
	// 这种情况必须显式失败：维度不匹配的向量写进索引后，检索结果会变成一堆噪声，且极难排查。
	ErrDimensionMismatch = errors.New("向量维度与索引不一致")
	// ErrChatUnsupported 表示该厂商不提供对话能力。
	ErrChatUnsupported = errors.New("该厂商不支持对话")
	// ErrEmbeddingUnsupported 表示该厂商不提供向量化能力。
	//
	// 与 ErrNotConfigured 分开：前者要换厂商（例如 DeepSeek 没有 embedding 接口），
	// 后者只要去配置页填一下。
	ErrEmbeddingUnsupported = errors.New("该厂商不支持向量化")
	// ErrRerankUnsupported 表示该厂商不提供重排能力。
	//
	// 单独一个错误而不是复用 ErrNotConfigured：两者修法不同——
	// 「没配」要去配置页填，「不支持」得换厂商（实测 Ollama 就没有 rerank 端点）。
	ErrRerankUnsupported = errors.New("该厂商不支持重排")
	// ErrToolCallingUnsupported 表示所选模型不具备工具调用能力。
	//
	// 智能体形态必须据此明确失败：退化成普通对话会让使用者以为
	// 「工具已经接上了」，实际模型从未调用过任何工具。
	ErrToolCallingUnsupported = errors.New("该模型不支持工具调用")
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

// ChatModel 是对话模型。
//
// 直接复用 Eino 的接口（Generate / Stream）而不是自建一套：
// 编排层（compose 的 Chain / Graph 节点）只认这个接口，
// 我们再包一层同名接口只会多一次无意义的类型转换。
// 厂商实现只要满足它，就能被 Eino 的图直接编排。
type ChatModel = einomodel.BaseChatModel

// ToolCallingChatModel 是带工具绑定能力的对话模型。
//
// 直接复用 Eino 的接口：智能体运行时（ReAct）只认它，
// WithTools 返回新实例而不是原地修改，保证同一模型可被多个请求安全共享。
type ToolCallingChatModel = einomodel.ToolCallingChatModel

// RequireToolCalling 把普通对话模型提升为工具调用模型。
//
// 用类型断言而不是另建一层适配：模型"是否支持工具"是实现的事实属性，
// 到这里还不能确定就应该明确报错，而不是退化成纯聊天。
func RequireToolCalling(modelName string, chat ChatModel) (ToolCallingChatModel, error) {
	if chat == nil {
		return nil, fmt.Errorf("%w: 模型 %s 没有可用实例", ErrToolCallingUnsupported, modelName)
	}
	toolCalling, ok := chat.(ToolCallingChatModel)
	if !ok {
		return nil, fmt.Errorf("%w（模型 %s），请改用支持工具调用的模型", ErrToolCallingUnsupported, modelName)
	}
	return toolCalling, nil
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
	// ChatModel 返回该作用域下生效的对话模型。
	ChatModel(ctx context.Context, scope ScopeRef) (ChatModel, error)
	// ToolCallingChatModel 返回该作用域下生效、且具备工具调用能力的对话模型。
	//
	// 单独一个入口而不是让调用方自己断言：解析与缓存只有一份，
	// 调用方（智能体运行时）不应该知道具体厂商的实例类型。
	ToolCallingChatModel(ctx context.Context, scope ScopeRef) (ToolCallingChatModel, error)
	// Reranker 返回该作用域下生效的重排模型。
	//
	// 未配置时返回 ErrNotConfigured：调用方据此**跳过重排并说明原因**，
	// 而不是把「没配重排」当成检索失败。
	Reranker(ctx context.Context, scope ScopeRef) (Reranker, error)
}
