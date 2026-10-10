package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	enginemodel "einoflow-backend/internal/engine/model"
	"einoflow-backend/internal/engine/model/openai"
	"einoflow-backend/internal/model"
)

// openaiCompatibleProvider 覆盖所有遵循 OpenAI 接口约定的厂商与本地推理服务。
//
// 它的存在让「接入一家新厂商」在多数情况下不需要写代码：
// 填地址、填密钥、填模型名即可（DeepSeek / DashScope 兼容模式 / 硅基流动 / vLLM …）。
type openaiCompatibleProvider struct {
	http *http.Client
}

func newOpenAICompatible() *openaiCompatibleProvider {
	return &openaiCompatibleProvider{http: &http.Client{Timeout: 20 * time.Second}}
}

func (p *openaiCompatibleProvider) ID() string { return "openai_compatible" }

func (p *openaiCompatibleProvider) DisplayName() string {
	return "OpenAI 兼容接口（自填地址与模型）"
}

func (p *openaiCompatibleProvider) DefaultBaseURL() string { return "https://api.openai.com/v1" }

func (p *openaiCompatibleProvider) Kinds() []model.ModelType {
	return []model.ModelType{model.ModelTypeLLM, model.ModelTypeEmbedding}
}

func (p *openaiCompatibleProvider) CredentialFields() []CredentialField {
	return []CredentialField{{
		Key:         "api_key",
		Label:       "API Key",
		Required:    true,
		Secret:      true,
		Placeholder: "sk-...",
	}}
}

// openaiModelsResponse 对应 GET /models。
type openaiModelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Probe 通过列举模型完成连通性测试。
//
// 为什么用 /models 而不是发一次真实请求：列举是只读且便宜的，能看到密钥是否有效、
// 地址是否写对（少写或多了 /v1 是最常见的错误），还能顺带把模型名给使用者挑。
func (p *openaiCompatibleProvider) Probe(ctx context.Context, cfg Config) (ProbeResult, error) {
	baseURL := cfg.BaseURLOrDefault(p.DefaultBaseURL())
	return measure(func() (ProbeResult, error) {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
		if err != nil {
			return ProbeResult{}, err
		}
		if key := cfg.Credential("api_key"); key != "" {
			request.Header.Set("Authorization", "Bearer "+key)
		}

		response, err := p.http.Do(request)
		if err != nil {
			return ProbeResult{}, fmt.Errorf("无法连接（%s）：%w", baseURL, err)
		}
		defer func() { _ = response.Body.Close() }()

		body, err := io.ReadAll(response.Body)
		if err != nil {
			return ProbeResult{}, fmt.Errorf("读取响应失败: %w", err)
		}
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			return ProbeResult{}, fmt.Errorf("凭证被拒绝（HTTP %d），请检查 API Key", response.StatusCode)
		}
		if response.StatusCode != http.StatusOK {
			return ProbeResult{}, fmt.Errorf("接口返回 HTTP %d：%s", response.StatusCode, truncate(string(body), 200))
		}

		var parsed openaiModelsResponse
		if err := json.Unmarshal(body, &parsed); err != nil {
			// 部分兼容端点不实现 /models，会在根路径返回一段 HTML 或空体。
			// 这种情况不算「连不上」，但必须说清楚，否则使用者会以为密钥错了。
			return ProbeResult{
				OK:      true,
				Message: "地址可访问，但该端点未提供模型列表（不影响按模型名调用）",
			}, nil
		}
		if parsed.Error != nil && parsed.Error.Message != "" {
			return ProbeResult{}, fmt.Errorf("接口报错: %s", parsed.Error.Message)
		}

		names := make([]string, 0, len(parsed.Data))
		for _, item := range parsed.Data {
			if item.ID != "" {
				names = append(names, item.ID)
			}
		}
		return buildProbeResult("接口连接正常，凭证有效", names, cfg.ModelName), nil
	})
}

// NewEmbedder 构造 OpenAI 兼容的向量化实现。
func (p *openaiCompatibleProvider) NewEmbedder(cfg Config) (enginemodel.Embedder, error) {
	if strings.TrimSpace(cfg.ModelName) == "" {
		return nil, fmt.Errorf("未指定模型名")
	}
	instance := openai.New(openai.Options{
		BaseURL: cfg.BaseURLOrDefault(p.DefaultBaseURL()),
		Model:   strings.TrimSpace(cfg.ModelName),
		APIKey:  cfg.Credential("api_key"),
	})
	if _, err := instance.DetectDimension(context.Background()); err != nil {
		return nil, fmt.Errorf("模型 %s 不可用: %w", cfg.ModelName, err)
	}
	return instance, nil
}
