package providers

import (
	"context"
	"fmt"
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

func (p *openaiCompatibleProvider) DisplayName() string { return "OpenAI 兼容接口" }

// Description 说明适用范围：这一类厂商靠「接口约定」统一，不点名具体服务商。
func (p *openaiCompatibleProvider) Description() string {
	return "任何遵循 OpenAI 接口约定的服务：DeepSeek、通义千问兼容模式、硅基流动、vLLM 等，" +
		"填地址与密钥即可用（模型名需与对方一致）。"
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

// Probe 通过列举模型完成连通性测试。
//
// 为什么用 /models 而不是发一次真实请求：列举是只读且便宜的，能看到密钥是否有效、
// 地址是否写对（少写或多了 /v1 是最常见的错误），还能顺带把模型名给使用者挑。
func (p *openaiCompatibleProvider) Probe(ctx context.Context, cfg Config) (ProbeResult, error) {
	return probeOpenAIStyleModels(
		ctx, p.http, cfg.BaseURLOrDefault(p.DefaultBaseURL()), cfg.Credential("api_key"), cfg.ModelName)
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

// NewReranker 不支持：OpenAI 兼容协议里没有统一的重排接口
// （各家的重排端点路径与出入参都不一样），因此不在这个厂商里硬凑，
// 统一走 TEI 边车（见 D5）。
func (p *openaiCompatibleProvider) NewReranker(Config) (enginemodel.Reranker, error) {
	return nil, fmt.Errorf("%w: OpenAI 兼容接口没有统一的重排端点，请使用 TEI 边车承载重排",
		enginemodel.ErrRerankUnsupported)
}
