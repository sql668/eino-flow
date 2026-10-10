package providers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	enginemodel "einoflow-backend/internal/engine/model"
	"einoflow-backend/internal/model"
)

// deepseekProvider 是 DeepSeek 官方接口。
//
// 之所以给它单独一个内置条目（而不是让用户用「OpenAI 兼容接口」自己填）：
//  1. 默认地址、模型名（deepseek-chat / deepseek-reasoner）都是固定的，
//     内置后使用者不必查文档，也不会把地址少写或多写 /v1；
//  2. **它只有对话能力**。如果混在「OpenAI 兼容接口」里，很容易被当成向量化模型用，
//     直到上传文档时才报错。能力声明清楚，界面上的下拉里也不会出现它。
//
// 接口本身是 OpenAI 风格的，因此探测与调用复用同一套实现。
type deepseekProvider struct {
	http *http.Client
}

func newDeepSeek() *deepseekProvider {
	return &deepseekProvider{http: &http.Client{Timeout: 20 * time.Second}}
}

func (p *deepseekProvider) ID() string          { return "deepseek" }
func (p *deepseekProvider) DisplayName() string { return "DeepSeek" }

func (p *deepseekProvider) Description() string {
	return "DeepSeek 官方接口，对话模型 deepseek-chat / deepseek-reasoner；" +
		"不提供向量化与重排接口（检索与重排请用其它厂商）。"
}

// DefaultBaseURL 带上 /v1：DeepSeek 兼容 OpenAI SDK，两种写法都能通，
// 但带上 /v1 后与「OpenAI 兼容接口」的填法一致，少一个踩坑点。
func (p *deepseekProvider) DefaultBaseURL() string { return "https://api.deepseek.com/v1" }

func (p *deepseekProvider) Kinds() []model.ModelType {
	// 只有 llm：DeepSeek 没有 embedding 接口，也不能做重排。
	return []model.ModelType{model.ModelTypeLLM}
}

func (p *deepseekProvider) CredentialFields() []CredentialField {
	return []CredentialField{{
		Key:         "api_key",
		Label:       "API Key",
		Required:    true,
		Secret:      true,
		Placeholder: "sk-...",
	}}
}

// Probe 用 GET /models 验证地址与密钥：只读、便宜，还能顺带列出可用模型名。
func (p *deepseekProvider) Probe(ctx context.Context, cfg Config) (ProbeResult, error) {
	return probeOpenAIStyleModels(
		ctx, p.http, cfg.BaseURLOrDefault(p.DefaultBaseURL()), cfg.Credential("api_key"), cfg.ModelName)
}

// NewEmbedder 明确拒绝：DeepSeek 没有向量化接口。
//
// 报「不支持」而不是「未配置」，因为这两件事的修法不同：前者要换厂商，后者去填凭证。
func (p *deepseekProvider) NewEmbedder(Config) (enginemodel.Embedder, error) {
	return nil, fmt.Errorf("%w: DeepSeek 不提供向量化接口，请改用 Ollama 或其它 OpenAI 兼容服务",
		enginemodel.ErrEmbeddingUnsupported)
}

// NewReranker 明确拒绝：DeepSeek 没有重排接口。
func (p *deepseekProvider) NewReranker(Config) (enginemodel.Reranker, error) {
	return nil, fmt.Errorf("%w: DeepSeek 不提供重排接口，请使用 TEI 重排服务",
		enginemodel.ErrRerankUnsupported)
}

// 编译期确认接口实现完整，避免漏方法到注册表处才报错。
var _ Provider = (*deepseekProvider)(nil)
