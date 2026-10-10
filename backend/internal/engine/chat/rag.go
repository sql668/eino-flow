// Package chat 用 Eino 编排问答链路。
//
// 链路形状（V0.1 的 RAG 问答）：
//
//	检索（Lambda 节点）→ 组装提示词（含引用编号）→ 对话模型（Eino ChatModel 节点）
//
// 为什么检索也放进 Eino 图里，而不是在 service 里先查再拼提示：
// 「检索 → 提示 → 生成」就是这条业务链路的全部步骤，放进同一个 Runnable 之后，
// 将来要加改写、多路召回、工具调用，都是往图里加节点，而不是在 service 里堆 if-else。
// 留痕与引用展示仍然由 service 负责（检索能力通过接口注入，见 Retriever）。
package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// Context 是一条被引用的片段。
//
// 带上文档名与页码，是因为「可溯源」是这个平台的卖点：使用者要能一眼看到
// 这句话出自哪份文档的哪一页，而不是只看到一段文本。
type Context struct {
	Index        int
	SegmentID    uint64
	DocumentID   uint64
	DocumentName string
	Page         int
	TitlePath    []string
	Kind         string
	Content      string
	// Score 是最终排序分数；RerankScore 非空表示经过重排。
	Score       float32
	RerankScore *float32
}

// Scope 指定检索范围。
type Scope struct {
	AppID     uint64
	DatasetID uint64
	TopK      int
}

// Retriever 是检索能力。
//
// 定义在这里而不是直接依赖 rag.Retriever：service 需要在检索时顺带写留痕、
// 决定是否重排，把它收敛成一个窄接口，图的节点就不必知道这些细节。
type Retriever interface {
	// Retrieve 返回与问题相关的片段（已按相关性排序）。
	Retrieve(ctx context.Context, scope Scope, query string) ([]Context, error)
}

// Request 是一次问答请求。
type Request struct {
	// Question 是本轮问题。
	Question string
	// Scope 指定检索范围（DatasetID 为 0 表示不检索，即纯对话模式）。
	Scope Scope
	// History 是历史消息（不含本轮），用于多轮对话。
	History []*schema.Message
	// Citations 是**输出参数**：检索节点回填本次引用的片段。
	//
	// 用指针而不是返回值，是因为输出必须是模型消息（Eino 的流式只对最后一个节点的输出生效）。
	// 每次调用传入独立的 Request，因此不存在并发写同一字段的问题。
	Citations *[]Context
}

// Deps 是链路依赖。
type Deps struct {
	// Retriever 为 nil 表示纯对话（不检索）。
	Retriever Retriever
	// ChatModel 是 Eino 的对话模型；必须是已解析好的实例。
	ChatModel model.BaseChatModel
	// SystemPrompt 可选：留空使用内置的默认提示词。
	SystemPrompt string
}

// RAGChain 是编译好的问答链路。
type RAGChain struct {
	runnable compose.Runnable[*Request, *schema.Message]
}

// 默认系统提示词。
//
// 三条要求都是为了「可核对」：只用资料回答、资料不足要说不确定、引用要带编号。
// 不写「请勿编造」这种泛泛的话——约束要能落到可检查的行为上。
const defaultSystemPrompt = `你是 EinoFlow 的知识库问答助手。请遵守以下要求：
1. 只依据「参考资料」回答；资料里没有的内容，明确说明「资料中没有相关内容」，不要凭常识补全。
2. 回答中引用资料时，在对应句子末尾标注来源编号，例如 [1]、[2]。
3. 用简洁的书面中文回答，先给结论，再给必要说明。`

// NewRAGChain 编译问答链路。
//
// 编译放在构造时而不是每次请求：Eino 的 Compile 会做类型校验与拓扑检查，
// 把它放在启动/配置变更时做一次，请求路径上只剩实际执行。
func NewRAGChain(ctx context.Context, deps Deps) (*RAGChain, error) {
	if deps.ChatModel == nil {
		return nil, fmt.Errorf("缺少对话模型")
	}
	systemPrompt := strings.TrimSpace(deps.SystemPrompt)
	if systemPrompt == "" {
		systemPrompt = defaultSystemPrompt
	}

	chain := compose.NewChain[*Request, *schema.Message]()
	chain.AppendLambda(compose.InvokableLambda(
		func(ctx context.Context, req *Request) ([]*schema.Message, error) {
			return buildMessages(ctx, deps, systemPrompt, req)
		},
	))
	chain.AppendChatModel(deps.ChatModel)

	runnable, err := chain.Compile(ctx)
	if err != nil {
		return nil, fmt.Errorf("编译问答链路失败: %w", err)
	}
	return &RAGChain{runnable: runnable}, nil
}

// Stream 执行链路并返回模型输出的流。
func (c *RAGChain) Stream(
	ctx context.Context, req *Request,
) (*schema.StreamReader[*schema.Message], error) {
	return c.runnable.Stream(ctx, req)
}

// Invoke 执行链路并一次性拿到完整回答（用于不需要流式的场景与测试）。
func (c *RAGChain) Invoke(ctx context.Context, req *Request) (*schema.Message, error) {
	return c.runnable.Invoke(ctx, req)
}

// buildMessages 是图里的检索 + 组装节点。
func buildMessages(
	ctx context.Context, deps Deps, systemPrompt string, req *Request,
) ([]*schema.Message, error) {
	if req == nil {
		return nil, fmt.Errorf("请求为空")
	}
	question := strings.TrimSpace(req.Question)
	if question == "" {
		return nil, fmt.Errorf("问题不能为空")
	}

	messages := make([]*schema.Message, 0, len(req.History)+2)
	messages = append(messages, schema.SystemMessage(systemPrompt))

	// 检索：只有指定了知识库才做（纯对话模式不带 DatasetID）。
	if deps.Retriever != nil && req.Scope.DatasetID != 0 {
		contexts, err := deps.Retriever.Retrieve(ctx, req.Scope, question)
		if err != nil {
			return nil, err
		}
		if req.Citations != nil {
			*req.Citations = contexts
		}
		messages = append(messages, schema.SystemMessage(formatContexts(contexts)))
	}

	// 历史消息放在资料之后、本轮问题之前：模型能看到「之前聊了什么」，
	// 但资料始终紧贴问题，避免被长历史冲淡。
	messages = append(messages, req.History...)
	messages = append(messages, schema.UserMessage(question))
	return messages, nil
}

// formatContexts 把片段拼成提示词里的「参考资料」块。
//
// 编号从 1 开始，与回答里的 [n] 对应；来源信息（文档名 · 页码 · 标题路径）
// 一起给模型，是为了让它能自然地写出「据《xx》第 3 页」这类可核对的表述。
func formatContexts(contexts []Context) string {
	if len(contexts) == 0 {
		return "参考资料：（本次检索没有命中任何片段，请直接说明资料中没有相关内容）"
	}
	var builder strings.Builder
	builder.WriteString("参考资料：\n")
	for i, item := range contexts {
		builder.WriteString(fmt.Sprintf("[%d] 来源：%s", i+1, describeSource(item)))
		builder.WriteString("\n")
		builder.WriteString(strings.TrimSpace(item.Content))
		builder.WriteString("\n\n")
	}
	return builder.String()
}

// describeSource 把溯源字段拼成一行人类可读的来源描述。
func describeSource(item Context) string {
	parts := make([]string, 0, 3)
	if item.DocumentName != "" {
		parts = append(parts, item.DocumentName)
	}
	if item.Page > 0 {
		parts = append(parts, fmt.Sprintf("第 %d 页", item.Page))
	}
	if len(item.TitlePath) > 0 {
		parts = append(parts, strings.Join(item.TitlePath, " / "))
	}
	if item.Kind == "table" {
		parts = append(parts, "表格")
	}
	if len(parts) == 0 {
		return "未知来源"
	}
	return strings.Join(parts, " · ")
}
