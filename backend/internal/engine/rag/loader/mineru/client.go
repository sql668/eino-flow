// Package mineru 是 MinerU V1 HTTP API 的客户端。
//
// 依据 2026-10 对 MinerU 4.0.11 的实测，调用流程是：
//
//	POST /v1/uploads                   创建上传，拿到 upload_url
//	PUT  {upload_url}                  写入文件字节
//	POST /v1/uploads/{id}/complete     完成上传，拿到 file_id
//	POST /v1/parse/jobs                建解析任务，返回 202 + job_id
//	GET  /v1/parse/jobs/{job_id}       轮询直到 completed / failed
//	GET  /v1/files/{file_id}/content   取产物内容
//
// 解析必须按异步任务处理：basic 档实测约 0.25 页/秒，扫描件约 0.2 页/秒，
// 一份百页文档要几分钟，同步等待会长期占住连接与 goroutine。
package mineru

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// 任务与产物的状态/格式常量。
const (
	jobStatusCompleted = "completed"
	jobStatusFailed    = "failed"
	jobStatusCanceled  = "canceled"

	outputFormatMiddleJSON = "middle_json"
	outputFormatMarkdown   = "markdown"
)

// 默认参数。解析是 CPU 密集型，轮询超时给得比较宽松。
const (
	defaultBaseURL      = "http://127.0.0.1:8000"
	defaultPollInterval = 2 * time.Second
	defaultPollTimeout  = 15 * time.Minute
	defaultHTTPTimeout  = 60 * time.Second
	// maxErrorBody 限制错误响应体的读取长度，避免异常时把大段内容读进内存。
	maxErrorBody = 512
)

// ClientOptions 是客户端构造参数。
type ClientOptions struct {
	// BaseURL 是 MinerU 服务地址，例如 http://127.0.0.1:8000。
	BaseURL string
	// Token 是可选的访问令牌（服务端开启鉴权时必填）。
	Token string
	// HTTPClient 可选，默认使用带超时的标准客户端。
	HTTPClient *http.Client
	// PollInterval 是任务轮询间隔，默认 2 秒。
	PollInterval time.Duration
	// PollTimeout 是等待任务完成的超时，默认 15 分钟。
	PollTimeout time.Duration
}

// Client 是 MinerU V1 API 客户端。可复用，并发安全。
type Client struct {
	baseURL      string
	token        string
	http         *http.Client
	pollInterval time.Duration
	pollTimeout  time.Duration
}

// NewClient 构造客户端；BaseURL 为空时回落到本地默认地址。
func NewClient(opts ClientOptions) *Client {
	baseURL := strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}

	pollInterval := opts.PollInterval
	if pollInterval <= 0 {
		pollInterval = defaultPollInterval
	}
	pollTimeout := opts.PollTimeout
	if pollTimeout <= 0 {
		pollTimeout = defaultPollTimeout
	}

	return &Client{
		baseURL:      baseURL,
		token:        strings.TrimSpace(opts.Token),
		http:         httpClient,
		pollInterval: pollInterval,
		pollTimeout:  pollTimeout,
	}
}

// doJSON 发起 JSON 请求并解析响应；body 为 nil 表示无请求体（GET）。
func (c *Client) doJSON(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("序列化请求失败: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	c.applyAuth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, readErrorBody(resp.Body))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("解析响应失败: %w", err)
	}
	return nil
}

// doRaw 发起原始字节请求，用于上传文件内容。
func (c *Client) doRaw(ctx context.Context, method, url string, headers map[string]string, payload []byte) error {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	c.applyAuth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, readErrorBody(resp.Body))
	}
	return nil
}

// fetchFile 读取产物内容。
func (c *Client) fetchFile(ctx context.Context, fileID string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/files/"+fileID+"/content", nil)
	if err != nil {
		return nil, err
	}
	c.applyAuth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, readErrorBody(resp.Body))
	}
	return io.ReadAll(resp.Body)
}

// applyAuth 在配置了令牌时附加 Bearer 认证。
func (c *Client) applyAuth(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

// readErrorBody 读取有限长度的错误响应体，用于拼装可读的错误信息。
func readErrorBody(body io.Reader) string {
	raw, err := io.ReadAll(io.LimitReader(body, maxErrorBody))
	if err != nil {
		return "(读取响应体失败)"
	}
	return strings.TrimSpace(string(raw))
}
