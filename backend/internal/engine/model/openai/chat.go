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
}

var _ einomodel.BaseChatModel = (*ChatModel)(nil)

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

// Generate 实现 Eino 的 BaseChatModel。
func (c *ChatModel) Generate(
	ctx context.Context, input []*schema.Message, opts ...einomodel.Option,
) (*schema.Message, error) {
	_ = opts
	chunks, err := c.call(ctx, input, false)
	if err != nil {
		return nil, err
	}
	defer chunks.Close()

	var builder strings.Builder
	for {
		chunk, err := chunks.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		builder.WriteString(chunk.Content)
	}
	return schema.AssistantMessage(builder.String(), nil), nil
}

// Stream 实现 Eino 的 BaseChatModel。
func (c *ChatModel) Stream(
	ctx context.Context, input []*schema.Message, opts ...einomodel.Option,
) (*schema.StreamReader[*schema.Message], error) {
	_ = opts
	return c.call(ctx, input, true)
}

// call 调用 /chat/completions。
func (c *ChatModel) call(
	ctx context.Context, input []*schema.Message, stream bool,
) (*schema.StreamReader[*schema.Message], error) {
	if c.model == "" {
		return nil, fmt.Errorf("未指定对话模型名")
	}

	messages := make([]chatMessage, 0, len(input))
	for _, message := range input {
		if message == nil {
			continue
		}
		if len(message.ToolCalls) > 0 {
			return nil, fmt.Errorf("OpenAI 兼容对话实现暂不支持工具调用（ToolCalls）")
		}
		messages = append(messages, chatMessage{
			Role:    string(message.Role),
			Content: message.Content,
		})
	}

	payload, err := json.Marshal(chatRequest{
		Model:    c.model,
		Messages: messages,
		Stream:   stream,
	})
	if err != nil {
		return nil, fmt.Errorf("序列化对话请求失败: %w", err)
	}

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
		// 带上状态码与响应体：401（密钥）与 404（模型名/地址）的修法完全不同。
		return nil, fmt.Errorf("对话接口返回 HTTP %d: %s", response.StatusCode, truncate(string(body), maxErrorBody))
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
			content := parsed.content()
			if content == "" {
				// 表示结束、或只带 role 的首帧，跳过。
				continue
			}
			if closed := writer.Send(schema.AssistantMessage(content, nil), nil); closed {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			writer.Send(nil, fmt.Errorf("读取对话流失败: %w", err))
		}
	}()
	return reader, nil
}

// chatRequest 对应 /chat/completions 请求体（只取用到的字段）。
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

// chatMessage 是 OpenAI 风格的消息结构。
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatStreamChunk 对应流式响应中的一帧。
//
// 注意同时兼容流式（delta）与非流式（message）：有些兼容端点即使 stream=true
// 也会直接返回完整 message，只认 delta 会得到「空回答」。
type chatStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// content 取这一帧的文本，优先 delta，其次 message。
func (c chatStreamChunk) content() string {
	if len(c.Choices) == 0 {
		return ""
	}
	if text := c.Choices[0].Delta.Content; text != "" {
		return text
	}
	return c.Choices[0].Message.Content
}
