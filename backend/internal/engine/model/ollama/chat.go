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
)

// ChatModel 是 Ollama 的对话模型实现，直接满足 Eino 的 BaseChatModel 接口，
// 因此可以被 Eino 的 Chain / Graph 直接编排。
//
// 为什么只用 /api/chat 一个端点：Ollama 的流式与非流式是同一个端点，
// 靠 `stream` 字段切换，生成逻辑可以共用；分成两个实现只会多一处要同步改的解析代码。
type ChatModel struct {
	baseURL string
	model   string
	token   string
	http    *http.Client
}

// 编译期确认接口实现完整：Eino 的编排层只认这个接口，实现漂移会在编译期暴露。
var _ einomodel.BaseChatModel = (*ChatModel)(nil)

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

// Generate 实现 Eino 的 BaseChatModel：一次拿到完整回答。
func (c *ChatModel) Generate(
	ctx context.Context, input []*schema.Message, opts ...einomodel.Option,
) (*schema.Message, error) {
	_ = opts // 生成参数（温度等）尚未开放配置，见台账技术债
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

// Stream 实现 Eino 的 BaseChatModel：逐块产出回答。
func (c *ChatModel) Stream(
	ctx context.Context, input []*schema.Message, opts ...einomodel.Option,
) (*schema.StreamReader[*schema.Message], error) {
	_ = opts
	return c.call(ctx, input, true)
}

// call 调用 /api/chat。
//
// 流式用 goroutine + schema.Pipe 边读边发：Ollama 是按行返回 NDJSON 的，
// 等整段读完再发就不是流式了（首字延迟等于整段生成时间）。
func (c *ChatModel) call(
	ctx context.Context, input []*schema.Message, stream bool,
) (*schema.StreamReader[*schema.Message], error) {
	if c.model == "" {
		return nil, fmt.Errorf("未指定对话模型名")
	}

	messages := make([]ollamaChatMessage, 0, len(input))
	for _, message := range input {
		if message == nil {
			continue
		}
		if len(message.ToolCalls) > 0 {
			// 明确拒绝而不是忽略：悄悄丢掉工具调用会让模型「看不见」自己刚才的调用，
			// 表现为答非所问，排查起来很费劲。
			return nil, fmt.Errorf("Ollama 对话实现暂不支持工具调用（ToolCalls）")
		}
		messages = append(messages, ollamaChatMessage{
			Role:    string(message.Role),
			Content: message.Content,
		})
	}

	payload, err := json.Marshal(ollamaChatRequest{
		Model:    c.model,
		Messages: messages,
		Stream:   stream,
	})
	if err != nil {
		return nil, fmt.Errorf("序列化对话请求失败: %w", err)
	}

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
		return nil, fmt.Errorf("Ollama 返回 HTTP %d: %s", response.StatusCode, truncate(string(body), maxErrorBody))
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
			if chunk.Message.Content != "" {
				if closed := writer.Send(
					schema.AssistantMessage(chunk.Message.Content, nil), nil); closed {
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

// ollamaChatRequest 对应 /api/chat 请求体（只取用到的字段）。
type ollamaChatRequest struct {
	Model    string              `json:"model"`
	Messages []ollamaChatMessage `json:"messages"`
	Stream   bool                `json:"stream"`
}

// ollamaChatMessage 是 Ollama 的消息结构。
type ollamaChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ollamaChatResponse 对应 /api/chat 的一行响应（流式时每行一个）。
type ollamaChatResponse struct {
	Message ollamaChatMessage `json:"message"`
	Done    bool              `json:"done"`
	Error   string            `json:"error"`
}
