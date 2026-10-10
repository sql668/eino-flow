// Package ollama 提供 Ollama 的模型实现。
//
// 已实测（Ollama 0.40.1）：
//   - `POST /api/embed` 支持批量输入，传 N 条文本返回 N 条向量，这是入库性能的关键；
//   - 旧接口 `/api/embeddings` 只收单条 prompt，不再使用；
//   - bge-m3 输出 1024 维、nomic-embed-text 输出 768 维。
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"einoflow-backend/internal/engine/model"
)

const (
	defaultBaseURL = "http://127.0.0.1:11434"
	// defaultBatchSize 是单次请求的文本条数上限。
	// 太大容易让服务端内存尖峰，太小则往返次数多；16 在实测里比较平衡。
	defaultBatchSize = 16
	defaultTimeout   = 60 * time.Second
	maxErrorBody     = 512
	// probeText 用于探测模型维度。取一段短中文，避免触发过长的推理。
	probeText = "维度探测"
)

// Options 是构造参数。
type Options struct {
	// BaseURL 是 Ollama 服务地址，为空时用本地默认值。
	BaseURL string
	// Model 是模型名，例如 "bge-m3:latest"。
	Model string
	// BatchSize 是单次请求的文本条数，<=0 时用默认值。
	BatchSize int
	// HTTPClient 可选。
	HTTPClient *http.Client
	// Token 可选：Ollama 直连不需要，经反向代理暴露时可能需要。
	Token string
}

// Embedder 实现 model.Embedder。
type Embedder struct {
	baseURL   string
	model     string
	batchSize int
	token     string
	http      *http.Client

	// dimension 在首次成功调用或显式探测后确定。
	dimension int
}

// New 构造 Embedder。维度未知，需要调用 DetectDimension 或首次 Embed 后才有值。
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
		model:     opts.Model,
		batchSize: batchSize,
		token:     strings.TrimSpace(opts.Token),
		http:      client,
	}
}

// Name 实现 model.Embedder。
func (e *Embedder) Name() string { return e.model }

// Dimension 实现 model.Embedder。尚未探测过时返回 0。
func (e *Embedder) Dimension() int { return e.dimension }

// DetectDimension 通过一次探测调用确定向量维度。
//
// 之所以要探测而不是靠配置写死：模型名与维度没有强制绑定，写死容易在换模型时
// 悄悄留下错值；探测拿到的是服务端的真实输出。
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

// Embed 实现 model.Embedder：按批调用 `/api/embed`。
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/api/embed", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if e.token != "" {
		req.Header.Set("Authorization", "Bearer "+e.token)
	}

	resp, err := e.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("调用 Ollama 失败（%s）: %w", e.baseURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取 Ollama 响应失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Ollama 返回 HTTP %d: %s", resp.StatusCode, truncate(string(raw), maxErrorBody))
	}

	var parsed embedResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("解析 Ollama 响应失败: %w", err)
	}
	if parsed.Error != "" {
		return nil, fmt.Errorf("Ollama 报错: %s", parsed.Error)
	}
	if len(parsed.Embeddings) != len(texts) {
		return nil, fmt.Errorf("Ollama 返回的向量数（%d）与输入文本数（%d）不一致",
			len(parsed.Embeddings), len(texts))
	}

	// 维度一致性校验：服务端换了模型、或模型被重新下载成不同版本时，
	// 维度会静默变化。这里显式报错，避免把不同维度的向量混进同一个索引。
	for i, vector := range parsed.Embeddings {
		if len(vector) == 0 {
			return nil, fmt.Errorf("Ollama 返回的第 %d 条向量为空", i)
		}
		if e.dimension == 0 {
			e.dimension = len(vector)
			continue
		}
		if len(vector) != e.dimension {
			return nil, fmt.Errorf("%w: 模型 %s 本次返回 %d 维，与先前观测到的 %d 维不一致",
				model.ErrDimensionMismatch, e.model, len(vector), e.dimension)
		}
	}
	return parsed.Embeddings, nil
}

// embedRequest 对应 Ollama 的 /api/embed 请求体。
type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// embedResponse 对应 /api/embed 响应体（只取用到的字段）。
type embedResponse struct {
	Model      string      `json:"model"`
	Embeddings [][]float32 `json:"embeddings"`
	Error      string      `json:"error"`
}

// truncate 限制错误信息长度，避免把大段响应体带进日志。
func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}
