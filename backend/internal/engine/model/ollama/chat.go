package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"

	enginemodel "einoflow-backend/internal/engine/model"
)

// ChatModel 是 Ollama 的对话模型实现，直接满足 Eino 的 ToolCallingChatModel 接口，
// 因此可以被 Eino 的 Chain / Graph / ReAct 直接编排。
//
// 为什么只用 /api/chat 一个端点：Ollama 的流式与非流式是同一个端点，
// 靠 `stream` 字段切换，生成逻辑可以共用；分成两个实现只会多一处要同步改的解析代码。
type ChatModel struct {
	baseURL string
	model   string
	token   string
	http    *http.Client
	// tools 是 WithTools 绑定后的工具声明。
	tools []ollamaTool
}

// 编译期确认接口实现完整：Eino 的 ReAct 只认 ToolCallingChatModel，
// 实现漂移会在编译期暴露。
var _ einomodel.ToolCallingChatModel = (*ChatModel)(nil)

// ChatOptions 是构造参数。
type ChatOptions struct {
	BaseURL string
	Model   string
	Token   string
	// HTTPClient 可选，便于测试注入。
	HTTPClient *http.Client
}

// NewChatModel 构造 Ollama 对话模型。
//
// 与向量化不同，这里**不做启动期探测**：对话模型只有在真正提问时才知道能不能用，
// 提前发一次请求既慢又会消耗算力；连通性问题交给配置页的「测试连接」。
func NewChatModel(opts ChatOptions) *ChatModel {
	baseURL := strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	client := opts.HTTPClient
	if client == nil {
		// 对话比向量化慢得多（尤其是本地大模型），超时给得宽松。
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	return &ChatModel{
		baseURL: baseURL,
		model:   strings.TrimSpace(opts.Model),
		token:   strings.TrimSpace(opts.Token),
		http:    client,
	}
}

// Name 返回模型标识，用于响应里的展示字段（Eino 的接口没有这个方法）。
func (c *ChatModel) Name() string { return c.model }

// WithTools 返回带工具集的模型副本（不可变，理由同 OpenAI 兼容实现）。
func (c *ChatModel) WithTools(tools []*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	prepared, err := buildOllamaTools(tools)
	if err != nil {
		return nil, err
	}
	clone := *c
	clone.tools = prepared
	return &clone, nil
}

// Generate 实现 Eino 的 BaseChatModel：一次拿到完整回答。
func (c *ChatModel) Generate(
	ctx context.Context, input []*schema.Message, opts ...einomodel.Option,
) (*schema.Message, error) {
	_ = opts // 生成参数（温度等）尚未开放配置，见台账技术债
	payload, err := c.buildPayload(input, false)
	if err != nil {
		return nil, err
	}
	response, err := c.do(ctx, payload)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()

	var chunk ollamaChatResponse
	if err := json.NewDecoder(response.Body).Decode(&chunk); err != nil {
		return nil, fmt.Errorf("解析 Ollama 对话响应失败: %w", err)
	}
	if chunk.Error != "" {
		return nil, fmt.Errorf("Ollama 报错: %s", chunk.Error)
	}
	return toSchemaMessage(chunk.Message)
}

// Stream 实现 Eino 的 BaseChatModel：逐块产出回答。
//
// 流式用 goroutine + schema.Pipe 边读边发：Ollama 是按行返回 NDJSON 的，
// 等整段读完再发就不是流式了（首字延迟等于整段生成时间）。
func (c *ChatModel) Stream(
	ctx context.Context, input []*schema.Message, opts ...einomodel.Option,
) (*schema.StreamReader[*schema.Message], error) {
	_ = opts
	payload, err := c.buildPayload(input, true)
	if err != nil {
		return nil, err
	}
	response, err := c.do(ctx, payload)
	if err != nil {
		return nil, err
	}

	reader, writer := schema.Pipe[*schema.Message](8)
	go func() {
		defer func() { _ = response.Body.Close() }()
		defer writer.Close()

		scanner := bufio.NewScanner(response.Body)
		// 单行可能很长（长回答），把缓冲区放大以免被默认 64KB 截断。
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var chunk ollamaChatResponse
			if err := json.Unmarshal([]byte(line), &chunk); err != nil {
				writer.Send(nil, fmt.Errorf("解析 Ollama 对话响应失败: %w", err))
				return
			}
			if chunk.Error != "" {
				writer.Send(nil, fmt.Errorf("Ollama 报错: %s", chunk.Error))
				return
			}

			message, err := toSchemaMessage(chunk.Message)
			if err != nil {
				writer.Send(nil, err)
				return
			}
			// 思考内容（qwen3 这类推理模型会先产出 thinking）与正式回答分开送出：
			// 它们的用途不同——思考过程只用于「让用户看到还在干活」，
			// 正式回答才进正文与落库（见 service/chat.go）。
			if message.Content != "" || message.ReasoningContent != "" || len(message.ToolCalls) > 0 {
				if closed := writer.Send(message, nil); closed {
					// 下游已经不再接收（客户端断开），停止读取，避免白烧算力。
					return
				}
			}
			if chunk.Done {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			writer.Send(nil, fmt.Errorf("读取 Ollama 对话流失败: %w", err))
		}
	}()
	return reader, nil
}

// buildPayload 组装 /api/chat 请求体。
func (c *ChatModel) buildPayload(input []*schema.Message, stream bool) ([]byte, error) {
	if c.model == "" {
		return nil, fmt.Errorf("未指定对话模型名")
	}

	messages := make([]ollamaChatMessage, 0, len(input))
	for _, message := range input {
		if message == nil {
			continue
		}
		toolCalls, err := toOllamaToolCalls(message.ToolCalls)
		if err != nil {
			return nil, err
		}
		messages = append(messages, ollamaChatMessage{
			Role:      string(message.Role),
			Content:   message.Content,
			Thinking:  message.ReasoningContent,
			ToolCalls: toolCalls,
			// Ollama 的 tool 消息靠 tool_name 关联到调用，不像 OpenAI 用 tool_call_id。
			ToolName: message.ToolName,
		})
	}

	payload := ollamaChatRequest{
		Model:    c.model,
		Messages: messages,
		Stream:   stream,
		Tools:    c.tools,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("序列化对话请求失败: %w", err)
	}
	return raw, nil
}

// do 发起请求并做状态码检查。
func (c *ChatModel) do(ctx context.Context, payload []byte) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}

	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("调用 Ollama 对话失败（%s）: %w", c.baseURL, err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		detail := truncate(string(body), maxErrorBody)
		if len(c.tools) > 0 && looksLikeToolUnsupported(detail) {
			return nil, fmt.Errorf("%w（模型 %s）：%s", enginemodel.ErrToolCallingUnsupported, c.model, detail)
		}
		return nil, fmt.Errorf("Ollama 返回 HTTP %d: %s", response.StatusCode, detail)
	}
	return response, nil
}

// looksLikeToolUnsupported 判断响应是否在说"不支持工具调用"。
func looksLikeToolUnsupported(detail string) bool {
	lower := strings.ToLower(detail)
	return strings.Contains(lower, "tool") || strings.Contains(lower, "function calling")
}

// buildOllamaTools 把 Eino 的工具声明转成 Ollama 的 tools 形态。
func buildOllamaTools(tools []*schema.ToolInfo) ([]ollamaTool, error) {
	prepared := make([]ollamaTool, 0, len(tools))
	seen := make(map[string]struct{}, len(tools))
	for _, info := range tools {
		if info == nil {
			return nil, fmt.Errorf("绑定工具失败: 工具声明为空")
		}
		name := strings.TrimSpace(info.Name)
		if name == "" {
			return nil, fmt.Errorf("绑定工具失败: 工具名不能为空")
		}
		if _, duplicated := seen[name]; duplicated {
			return nil, fmt.Errorf("绑定工具失败: 工具名 %q 重复", name)
		}
		seen[name] = struct{}{}

		parameters, err := info.ParamsOneOf.ToJSONSchema()
		if err != nil {
			return nil, fmt.Errorf("绑定工具 %s 失败: 参数结构无法转换: %w", name, err)
		}
		if parameters == nil {
			parameters = &jsonschema.Schema{Type: "object"}
		}
		prepared = append(prepared, ollamaTool{
			Type: "function",
			Function: ollamaToolFunction{
				Name:        name,
				Description: info.Desc,
				Parameters:  parameters,
			},
		})
	}
	return prepared, nil
}

// toOllamaToolCalls 把历史里的工具调用转成 Ollama 结构。
//
// Ollama 的 arguments 是 **JSON 对象**（不是字符串），所以要把 Eino 的字符串
// 解析成 RawMessage 再嵌入；直接原样传字符串会被 Ollama 当成非法参数。
func toOllamaToolCalls(calls []schema.ToolCall) ([]ollamaToolCall, error) {
	if len(calls) == 0 {
		return nil, nil
	}
	out := make([]ollamaToolCall, 0, len(calls))
	for _, call := range calls {
		arguments := strings.TrimSpace(call.Function.Arguments)
		if arguments == "" {
			arguments = "{}"
		}
		if !json.Valid([]byte(arguments)) {
			return nil, fmt.Errorf("工具 %s 的参数不是合法 JSON: %s", call.Function.Name, arguments)
		}
		out = append(out, ollamaToolCall{
			ID: call.ID,
			Function: ollamaFunctionCall{
				Name:      call.Function.Name,
				Arguments: json.RawMessage(arguments),
			},
		})
	}
	return out, nil
}

// toSchemaMessage 把 Ollama 的一条响应转成 Eino 消息。
func toSchemaMessage(message ollamaChatMessage) (*schema.Message, error) {
	toolCalls, err := toSchemaToolCalls(message.ToolCalls)
	if err != nil {
		return nil, err
	}
	result := schema.AssistantMessage(message.Content, toolCalls)
	result.ReasoningContent = message.Thinking
	return result, nil
}

// toSchemaToolCalls 把 Ollama 的工具调用转成 Eino 结构。
//
// Ollama 不保证返回调用 ID。没有 ID 时按序号补一个：Eino 的工具节点
// 用 ID 关联 assistant 的 tool_calls 与 tool 结果，空 ID 会让后续轮次对不上。
func toSchemaToolCalls(calls []ollamaToolCall) ([]schema.ToolCall, error) {
	if len(calls) == 0 {
		return nil, nil
	}
	out := make([]schema.ToolCall, 0, len(calls))
	for i, call := range calls {
		arguments := "{}"
		if len(call.Function.Arguments) > 0 && string(call.Function.Arguments) != "null" {
			var compact bytes.Buffer
			if err := json.Compact(&compact, call.Function.Arguments); err != nil {
				return nil, fmt.Errorf("解析工具 %s 的参数失败: %w", call.Function.Name, err)
			}
			arguments = compact.String()
		}
		id := call.ID
		if id == "" {
			id = fmt.Sprintf("call_%d", i)
		}
		index := i
		out = append(out, schema.ToolCall{
			Index: &index,
			ID:    id,
			Type:  "function",
			Function: schema.FunctionCall{
				Name:      call.Function.Name,
				Arguments: arguments,
			},
		})
	}
	return out, nil
}

// ollamaChatRequest 对应 /api/chat 请求体（只取用到的字段）。
type ollamaChatRequest struct {
	Model    string              `json:"model"`
	Messages []ollamaChatMessage `json:"messages"`
	Stream   bool                `json:"stream"`
	Tools    []ollamaTool        `json:"tools,omitempty"`
}

type ollamaTool struct {
	Type     string             `json:"type"`
	Function ollamaToolFunction `json:"function"`
}

type ollamaToolFunction struct {
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	Parameters  *jsonschema.Schema `json:"parameters"`
}

// ollamaChatMessage 是 Ollama 的消息结构。
type ollamaChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	// Thinking 是推理模型的思考过程（qwen3 等）。没有该字段的模型解析出来是空串。
	Thinking  string           `json:"thinking,omitempty"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
	// ToolName 只在 role=tool 时出现，用于关联是哪次调用。
	ToolName string `json:"tool_name,omitempty"`
}

type ollamaToolCall struct {
	ID       string             `json:"id,omitempty"`
	Function ollamaFunctionCall `json:"function"`
}

type ollamaFunctionCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// ollamaChatResponse 对应 /api/chat 的一行响应（流式时每行一个）。
type ollamaChatResponse struct {
	Message ollamaChatMessage `json:"message"`
	Done    bool              `json:"done"`
	Error   string            `json:"error"`
}
