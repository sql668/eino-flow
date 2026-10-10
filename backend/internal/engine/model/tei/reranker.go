// Package tei 提供 HuggingFace text-embeddings-inference（TEI）的调用实现。
//
// 本项目只把 TEI 用于**重排**：Ollama 没有 /rerank 端点（见 docs/PROGRESS.md D5），
// 重排必须有独立承载者。TEI 的 /rerank 接口语义简单（给查询与若干候选，回相关性分数），
// 正好补上这一环。
package tei

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
	defaultBaseURL = "http://127.0.0.1:8080"
	// defaultBatchSize 是单次 /rerank 请求携带的候选数。
	// TEI 有 max-client-batch-size 限制（默认 32），超过会直接报错，
	// 因此在客户端侧主动分批，而不是把「候选太多」留给服务端报错。
	defaultBatchSize = 32
	// defaultTimeout 给得比向量化宽松：重排是逐对（查询,候选）过模型，本身比 embedding 慢，
	// 边车还可能部署在远程；超时过短会把「慢」误判成「坏」。
	// 本机（Apple Silicon 模拟 amd64）实测 2 条候选要 200 秒以上，那是环境问题，
	// 用 EINOFLOW_RETRIEVAL_RERANK=off 关掉重排即可，而不是靠调小超时来「修复」。
	defaultTimeout = 120 * time.Second
	maxErrorBody   = 512
)

// Options 是构造参数。
type Options struct {
	BaseURL string
	// Model 仅用于展示与不一致校验，TEI 服务端加载哪个模型由它的启动参数决定。
	Model      string
	APIKey     string
	BatchSize  int
	HTTPClient *http.Client
}

// Reranker 实现 enginemodel.Reranker。
type Reranker struct {
	baseURL   string
	model     string
	apiKey    string
	batchSize int
	http      *http.Client
}

var _ enginemodel.Reranker = (*Reranker)(nil)

// New 构造 TEI 重排客户端。
func New(opts Options) *Reranker {
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
	return &Reranker{
		baseURL:   baseURL,
		model:     strings.TrimSpace(opts.Model),
		apiKey:    strings.TrimSpace(opts.APIKey),
		batchSize: batchSize,
		http:      client,
	}
}

// Name 实现 enginemodel.Reranker。
func (r *Reranker) Name() string { return r.model }

// Rerank 实现 enginemodel.Reranker：按批调用 /rerank，返回与入参等长的分数。
func (r *Reranker) Rerank(ctx context.Context, query string, documents []string) ([]float32, error) {
	if len(documents) == 0 {
		return nil, nil
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("重排的查询内容不能为空")
	}

	scores := make([]float32, len(documents))
	for start := 0; start < len(documents); start += r.batchSize {
		end := min(start+r.batchSize, len(documents))
		batchScores, err := r.rerankBatch(ctx, query, documents[start:end])
		if err != nil {
			return nil, err
		}
		copy(scores[start:end], batchScores)
	}
	return scores, nil
}

// rerankBatch 调用一次 /rerank。
func (r *Reranker) rerankBatch(ctx context.Context, query string, documents []string) ([]float32, error) {
	payload, err := json.Marshal(rerankRequest{
		Query: query,
		Texts: documents,
		// raw_scores=false 让服务端返回归一化后的相关性分数，便于跨模型比较与展示；
		// return_text=false 因为文本我们已经有了（在 PostgreSQL 里），没必要多传一份。
		RawScores:  false,
		ReturnText: false,
	})
	if err != nil {
		return nil, fmt.Errorf("序列化重排请求失败: %w", err)
	}

	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, r.baseURL+"/rerank", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	if r.apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+r.apiKey)
	}

	response, err := r.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("调用重排服务失败（%s）: %w", r.baseURL, err)
	}
	defer func() { _ = response.Body.Close() }()

	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("读取重排响应失败: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("重排服务返回 HTTP %d: %s", response.StatusCode, truncate(string(raw), maxErrorBody))
	}

	var parsed []rerankResult
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("解析重排响应失败: %w", err)
	}

	// 关键：结果按分数降序返回，index 才是「这条分数属于哪个候选」。
	// 直接按返回顺序当输入顺序，会把分数配到错误的片段上——检索会给出看似合理的错误答案。
	scores := make([]float32, len(documents))
	seen := make([]bool, len(documents))
	for _, item := range parsed {
		if item.Index < 0 || item.Index >= len(documents) {
			return nil, fmt.Errorf("重排服务返回了越界的下标 %d（候选数 %d）", item.Index, len(documents))
		}
		if seen[item.Index] {
			return nil, fmt.Errorf("重排服务重复返回了下标 %d", item.Index)
		}
		seen[item.Index] = true
		scores[item.Index] = item.Score
	}
	for i, ok := range seen {
		if !ok {
			// 缺结果必须报错而不是留 0 分：0 在归一化分数里是合法值，
			// 留 0 会让「服务漏返回」伪装成「这段不相关」，排序结果静默出错。
			return nil, fmt.Errorf("重排服务只返回了 %d/%d 条结果（缺少下标 %d）", len(parsed), len(documents), i)
		}
	}
	return scores, nil
}

// rerankRequest 对应 TEI 的 /rerank 请求体（只取用到的字段）。
type rerankRequest struct {
	Query      string   `json:"query"`
	Texts      []string `json:"texts"`
	RawScores  bool     `json:"raw_scores"`
	ReturnText bool     `json:"return_text"`
}

// rerankResult 对应 /rerank 返回数组中的一项。
type rerankResult struct {
	Index int     `json:"index"`
	Score float32 `json:"score"`
	Text  string  `json:"text"`
}

// truncate 限制错误信息长度，避免把大段响应体带进日志。
func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}
