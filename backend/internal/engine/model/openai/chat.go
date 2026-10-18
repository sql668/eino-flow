package openai

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

// ChatModel 是 OpenAI 兼容接口的对话实现（DeepSeek、通义兼容模式、vLLM 等同属这一类）。
//
// 与向量化实现放在同一个包：同一个厂商的两种能力共用地址与鉴权逻辑，
// 拆成两个包反而要各自维护一份 baseURL / Bearer 头。
type ChatModel struct {
	baseURL string
	model   string
	apiKey  string
	http    *http.Client
	// tools 是 WithTools 绑定后的工具声明（已转成 OpenAI 的 tools 形态）。
	// 放在实例上而不是请求参数里：Eino 的 ToolCallingChatModel 就是"带工具的模型实例"语义，
	// 同一个基础实例可以派生出不同工具集的副本，互不影响。
	tools []chatTool
}

var _ einomodel.ToolCallingChatModel = (*ChatModel)(nil)

// ChatOptions 是构造参数。
type ChatOptions struct {
	BaseURL    string
	Model      string
	APIKey     string
	HTTPClient *http.Client
}

// NewChatModel 构造 OpenAI 兼容的对话模型。
func NewChatModel(opts ChatOptions) *ChatModel {
	baseURL := strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	return &ChatModel{
		baseURL: baseURL,
		model:   strings.TrimSpace(opts.Model),
		apiKey:  strings.TrimSpace(opts.APIKey),
		http:    client,
	}
}

// Name 返回模型标识，用于响应里的展示字段（Eino 的接口没有这个方法）。
func (c *ChatModel) Name() string { return c.model }

// WithTools 返回带工具集的模型副本。
//
// 不做原地修改：base 实例在同一进程里被多个会话共享，
// 原地绑定会让 A 请求的工具集覆盖 B 请求（Eino 的 ToolCallingChatModel 文档也明确要求不可变）。
func (c *ChatModel) WithTools(tools []*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	prepared, err := buildChatTools(tools)
	if err != nil {
		return nil, err
	}
	clone := *c
	clone.tools = prepared
	return &clone, nil
}

// Generate 实现 Eino 的 BaseChatModel。
func (c *ChatModel) Generate(
	ctx context.Context, input []*schema.Message, opts ...einomodel.Option,
) (*schema.Message, error) {
	_ = opts
	payload, err := c.buildPayload(input, false)
	if err != nil {
		return nil, err
	}
	response, err := c.do(ctx, payload, false)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()

	var parsed chatResponse
	if err := json.NewDecoder(response.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("解析对话响应失败: %w", err)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return nil, fmt.Errorf("对话接口报错: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		// 空 choices 不是"空回答"，而是协议异常：当成空回答会让上层以为模型真的没话说。
		return nil, fmt.Errorf("对话接口返回了空的 choices")
	}

	choice := parsed.Choices[0].Message
	message := schema.AssistantMessage(choice.Content, toSchemaToolCalls(choice.ToolCalls))
	message.ReasoningContent = choice.ReasoningContent
	return message, nil
}

// Stream 实现 Eino 的 BaseChatModel。
func (c *ChatModel) Stream(
	ctx context.Context, input []*schema.Message, opts ...einomodel.Option,
) (*schema.StreamReader[*schema.Message], error) {
	_ = opts
	payload, err := c.buildPayload(input, true)
	if err != nil {
		return nil, err
	}
	response, err := c.do(ctx, payload, true)
	if err != nil {
		return nil, err
	}

	reader, writer := schema.Pipe[*schema.Message](8)
	go func() {
		defer func() { _ = response.Body.Close() }()
		defer writer.Close()

		scanner := bufio.NewScanner(response.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			// SSE 规范：只认 data: 行，其余（event/id/空行）忽略。
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "[DONE]" {
				return
			}
			var parsed chatStreamChunk
			if err := json.Unmarshal([]byte(data), &parsed); err != nil {
				writer.Send(nil, fmt.Errorf("解析对话流失败: %w", err))
				return
			}
			if parsed.Error != nil && parsed.Error.Message != "" {
				writer.Send(nil, fmt.Errorf("对话接口报错: %s", parsed.Error.Message))
				return
			}
			delta := parsed.firstDelta()
			if delta.Content == "" && delta.ReasoningContent == "" && len(delta.ToolCalls) == 0 {
				// 表示结束、或只带 role 的首帧，跳过。
				continue
			}
			message := schema.AssistantMessage(delta.Content, toSchemaToolCalls(delta.ToolCalls))
			message.ReasoningContent = delta.ReasoningContent
			if closed := writer.Send(message, nil); closed {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			writer.Send(nil, fmt.Errorf("读取对话流失败: %w", err))
		}
	}()
	return reader, nil
}

// buildPayload 组装请求体。
//
// 历史里的 assistant tool_calls 与 tool 结果必须原样带上：
// ReAct 第二轮模型要靠它们知道"上一轮调了什么、拿到了什么"，
// 少带任何一个都会让模型重复调用同一个工具。
func (c *ChatModel) buildPayload(input []*schema.Message, stream bool) ([]byte, error) {
	if c.model == "" {
		return nil, fmt.Errorf("未指定对话模型名")
	}

	messages := make([]chatMessage, 0, len(input))
	for _, message := range input {
		if message == nil {
			continue
		}
		messages = append(messages, chatMessage{
			Role:       string(message.Role),
			Content:    message.Content,
			ToolCalls:  toChatToolCalls(message.ToolCalls),
			ToolCallID: message.ToolCallID,
		})
	}

	payload := chatRequest{
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

// do 发起 HTTP 请求并做状态码检查。
func (c *ChatModel) do(ctx context.Context, payload []byte, stream bool) (*http.Response, error) {
	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	if stream {
		request.Header.Set("Accept", "text/event-stream")
	}
	if c.apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("调用对话接口失败（%s）: %w", c.baseURL, err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		detail := truncate(string(body), maxErrorBody)
		if len(c.tools) > 0 && looksLikeToolUnsupported(detail) {
			// 工具相关报错单独归类：调用方据此提示"换模型"，而不是让用户去猜 400 的含义。
			return nil, fmt.Errorf("%w（模型 %s）：%s", enginemodel.ErrToolCallingUnsupported, c.model, detail)
		}
		// 带上状态码与响应体：401（密钥）与 404（模型名/地址）的修法完全不同。
		return nil, fmt.Errorf("对话接口返回 HTTP %d: %s", response.StatusCode, detail)
	}
	return response, nil
}

// looksLikeToolUnsupported 判断 4xx 响应是否在说"不支持工具调用"。
//
// 不同兼容端点的措辞不一致（tools / tool_calls / function calling），
// 但共同点都会在错误体里出现 tool 字样；只对已绑定工具时调用，避免误伤普通 400。
func looksLikeToolUnsupported(detail string) bool {
	lower := strings.ToLower(detail)
	return strings.Contains(lower, "tool") || strings.Contains(lower, "function calling")
}

// chatRequest 对应 /chat/completions 请求体（只取用到的字段）。
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
	Tools    []chatTool    `json:"tools,omitempty"`
}

// chatMessage 是 OpenAI 风格的消息结构。
type chatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

// chatTool 是请求里的工具声明。
type chatTool struct {
	Type     string           `json:"type"`
	Function chatToolFunction `json:"function"`
}

type chatToolFunction struct {
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	Parameters  *jsonschema.Schema `json:"parameters"`
}

// chatToolCall 同时用于请求与响应。
//
// Index 只在流式响应的分片里有值：Eino 靠它把同一工具调用的参数片段拼起来。
type chatToolCall struct {
	Index    *int             `json:"index,omitempty"`
	ID       string           `json:"id,omitempty"`
	Type     string           `json:"type,omitempty"`
	Function chatFunctionCall `json:"function"`
}

type chatFunctionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// buildChatTools 把 Eino 的工具声明转成 OpenAI 的 tools 形态。
//
// 在 WithTools 阶段就完成参数结构转换：转换失败属于编程/声明错误，
// 应当立刻报出来，而不是等模型已经跑起来、发请求时才失败。
func buildChatTools(tools []*schema.ToolInfo) ([]chatTool, error) {
	prepared := make([]chatTool, 0, len(tools))
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
			// 无参工具也要给一个空对象结构：部分兼容端点不接受 parameters 缺失。
			parameters = &jsonschema.Schema{Type: "object"}
		}
		prepared = append(prepared, chatTool{
			Type: "function",
			Function: chatToolFunction{
				Name:        name,
				Description: info.Desc,
				Parameters:  parameters,
			},
		})
	}
	return prepared, nil
}

// toChatToolCalls 把 Eino 的 ToolCall 转成请求结构。
func toChatToolCalls(calls []schema.ToolCall) []chatToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]chatToolCall, 0, len(calls))
	for _, call := range calls {
		callType := call.Type
		if callType == "" {
			callType = "function"
		}
		out = append(out, chatToolCall{
			Index: call.Index,
			ID:    call.ID,
			Type:  callType,
			Function: chatFunctionCall{
				Name:      call.Function.Name,
				Arguments: call.Function.Arguments,
			},
		})
	}
	return out
}

// toSchemaToolCalls 把响应结构转回 Eino 的 ToolCall。
func toSchemaToolCalls(calls []chatToolCall) []schema.ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]schema.ToolCall, 0, len(calls))
	for _, call := range calls {
		callType := call.Type
		if callType == "" {
			callType = "function"
		}
		out = append(out, schema.ToolCall{
			Index: call.Index,
			ID:    call.ID,
			Type:  callType,
			Function: schema.FunctionCall{
				Name:      call.Function.Name,
				Arguments: call.Function.Arguments,
			},
		})
	}
	return out
}

// chatResponse 是非流式响应。
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content          string         `json:"content"`
			ReasoningContent string         `json:"reasoning_content"`
			ToolCalls        []chatToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// chatStreamChunk 对应流式响应中的一帧。
//
// 注意同时兼容流式（delta）与非流式（message）：有些兼容端点即使 stream=true
// 也会直接返回完整 message，只认 delta 会得到「空回答」。
type chatStreamChunk struct {
	Choices []struct {
		Delta   chatDelta `json:"delta"`
		Message chatDelta `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type chatDelta struct {
	Content          string         `json:"content"`
	ReasoningContent string         `json:"reasoning_content"`
	ToolCalls        []chatToolCall `json:"tool_calls"`
}

// firstDelta 取这一帧的有效增量，优先 delta，其次 message。
func (c chatStreamChunk) firstDelta() chatDelta {
	if len(c.Choices) == 0 {
		return chatDelta{}
	}
	delta := c.Choices[0].Delta
	if delta.Content != "" || delta.ReasoningContent != "" || len(delta.ToolCalls) > 0 {
		return delta
	}
	return c.Choices[0].Message
}
