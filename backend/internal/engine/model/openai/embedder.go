// Package openai 提供 OpenAI 兼容接口的模型实现。
//
// 一个实现覆盖一批厂商：OpenAI、DeepSeek、阿里云百炼（DashScope 兼容模式）、
// 硅基流动、vLLM / Xinference 等本地推理服务都遵循 `/v1/embeddings` 与
// `Authorization: Bearer <key>` 这套约定，差别只在 base_url 与模型名。
//
// 这也是「内置厂商 + 自填地址」这个方案的价值所在：加一家兼容厂商不用写代码。
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	enginemodel "einoflow-backend/internal/engine/model"
)

const (
	defaultBaseURL = "https://api.openai.com/v1"
	// defaultBatchSize 与 Ollama 侧保持一致：单次 16 条在实测里耗时与内存都平衡。
	defaultBatchSize = 16
	defaultTimeout   = 60 * time.Second
	maxErrorBody     = 512
	probeText        = "维度探测"
)

// Options 是构造参数。
type Options struct {
	// BaseURL 形如 https://api.openai.com/v1（含版本段）。
	BaseURL string
	Model   string
	APIKey  string
	// BatchSize <= 0 时用默认值。
	BatchSize int
	// HTTPClient 可选，便于测试注入。
	HTTPClient *http.Client
}

// Embedder 实现 enginemodel.Embedder。
type Embedder struct {
	baseURL   string
	model     string
	apiKey    string
	batchSize int
	http      *http.Client
	dimension int
}

// New 构造 Embedder。维度未知，需调用 DetectDimension 或首次 Embed 后才有值。
func New(opts Options) *Embedder {
	baseURL := strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	batchSize := opts.BatchSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
	}
	return &Embedder{
		baseURL:   baseURL,
		model:     strings.TrimSpace(opts.Model),
		apiKey:    strings.TrimSpace(opts.APIKey),
		batchSize: batchSize,
		http:      client,
	}
}

// Name 实现 enginemodel.Embedder。
func (e *Embedder) Name() string { return e.model }

// Dimension 实现 enginemodel.Embedder。尚未探测过时返回 0。
func (e *Embedder) Dimension() int { return e.dimension }

// DetectDimension 通过一次探测调用确定维度。
//
// 不靠「模型名 → 维度」的映射表：同一模型名在不同服务商下的维度可能不同
// （有的厂商提供多种维度配置），探测拿到的才是真实值。
func (e *Embedder) DetectDimension(ctx context.Context) (int, error) {
	vectors, err := e.Embed(ctx, []string{probeText})
	if err != nil {
		return 0, fmt.Errorf("探测模型 %s 维度失败: %w", e.model, err)
	}
	if len(vectors) != 1 || len(vectors[0]) == 0 {
		return 0, fmt.Errorf("模型 %s 返回了空的向量", e.model)
	}
	return len(vectors[0]), nil
}

// Embed 实现 enginemodel.Embedder：按批调用 /embeddings。
func (e *Embedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	result := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += e.batchSize {
		end := min(start+e.batchSize, len(texts))
		vectors, err := e.embedBatch(ctx, texts[start:end])
		if err != nil {
			return nil, err
		}
		result = append(result, vectors...)
	}
	return result, nil
}

// embedBatch 调用一次接口完成一批文本的向量化。
func (e *Embedder) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	payload, err := json.Marshal(embedRequest{Model: e.model, Input: texts})
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, e.baseURL+"/embeddings", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	response, err := e.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("调用 %s 失败: %w", e.baseURL, err)
	}
	defer func() { _ = response.Body.Close() }()

	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		// 把状态码与响应体带出来：401（密钥错）与 404（地址或模型名错）的修法完全不同，
		// 只回一句「调用失败」会让使用者拿到无用的报错。
		return nil, fmt.Errorf("接口返回 HTTP %d: %s", response.StatusCode, truncate(string(raw), maxErrorBody))
	}

	var parsed embedResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return nil, fmt.Errorf("接口报错: %s", parsed.Error.Message)
	}

	// 按 index 排序还原顺序：接口不保证返回顺序与入参一致，
	// 顺序错位会让「第 3 段文本」配上「第 5 段的向量」，检索结果会莫名其妙。
	vectors := make([][]float32, len(texts))
	for _, item := range parsed.Data {
		if item.Index < 0 || item.Index >= len(texts) {
			return nil, fmt.Errorf("接口返回了越界的下标 %d", item.Index)
		}
		vectors[item.Index] = item.Embedding
	}

	for i, vector := range vectors {
		if len(vector) == 0 {
			return nil, fmt.Errorf("第 %d 条文本没有返回向量", i)
		}
		if e.dimension == 0 {
			e.dimension = len(vector)
			continue
		}
		if len(vector) != e.dimension {
			return nil, fmt.Errorf("%w: 模型 %s 本次返回 %d 维，与先前的 %d 维不一致",
				enginemodel.ErrDimensionMismatch, e.model, len(vector), e.dimension)
		}
	}
	return vectors, nil
}

// embedRequest 对应 /embeddings 请求体。
type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// embedResponse 对应 /embeddings 响应体（只取用到的字段）。
type embedResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// truncate 限制错误信息长度。
func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}
