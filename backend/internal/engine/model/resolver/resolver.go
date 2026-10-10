// Package resolver 提供配置驱动的模型解析实现。
//
// 单独成包的原因：`model/ollama` 需要引用 `model` 里的接口与错误，
// 如果把解析器也放进 `model`，就会出现导入环。
//
// 这里是**阶段 1 的临时形态**：从配置读模型信息。阶段 2 会换成从数据库读取
// （内置厂商 + 凭证 + 模型列表 + 应用级覆盖），届时本包被替换，调用方不变。
package resolver

import (
	"context"
	"fmt"
	"strings"

	"einoflow-backend/internal/engine/model"
	"einoflow-backend/internal/engine/model/ollama"
	"einoflow-backend/internal/engine/model/providers"
	domainmodel "einoflow-backend/internal/model"
)

// 支持的厂商标识。
const (
	ProviderOllama = "ollama"
)

// supportedProviders 用于在配置写错时给出可选项，而不是只说「不支持」。
var supportedProviders = []string{ProviderOllama}

// Options 是配置驱动的解析参数。
type Options struct {
	// EmbeddingProvider 是向量化服务的厂商标识。
	EmbeddingProvider string
	// EmbeddingBaseURL 是服务地址。
	EmbeddingBaseURL string
	// EmbeddingModel 是模型名，例如 bge-m3:latest。
	EmbeddingModel string
	// EmbeddingBatchSize 是单次请求的文本条数，<=0 用默认值。
	EmbeddingBatchSize int
	// RerankProvider / RerankBaseURL / RerankModel 是重排模型的兜底配置。
	// RerankModel 为空表示「没有配置重排」，此时 Reranker 返回 ErrNotConfigured，
	// 调用方据此跳过重排而不是把检索判为失败。
	RerankProvider string
	RerankBaseURL  string
	RerankModel    string
}

// ConfigResolver 实现 model.Resolver。
type ConfigResolver struct {
	embedder model.Embedder
	reranker model.Reranker
	// rerankErr 记录重排不可用的原因（未配置 / 厂商不支持），调用时原样返回。
	rerankErr error
}

// New 构造解析器，并**在构造时完成可用性校验**。
//
// 构造即校验而不是延迟到首次使用：模型不可用是最容易静默出错的一环
// （比如配置了不存在的模型名，直到上传文档才暴露，而且表现为「文档一直失败」）。
// 启动时就报出来，排查成本最低——这与 main 里既有的「启动即校验依赖」一致。
func New(ctx context.Context, opts Options) (*ConfigResolver, error) {
	embedder, err := newEmbedder(ctx, opts)
	if err != nil {
		return nil, err
	}
	reranker, rerankErr := newReranker(opts)
	return &ConfigResolver{embedder: embedder, reranker: reranker, rerankErr: rerankErr}, nil
}

// Embedder 实现 model.Resolver。
//
// 忽略 scope：配置文件里的模型是全局唯一的一份，没有「知识库覆盖」这个概念。
// 需要覆盖能力时请用 DatabaseResolver（它会按作用域解析，并把它作为兜底）。
func (r *ConfigResolver) Embedder(context.Context, model.ScopeRef) (model.Embedder, error) {
	return r.embedder, nil
}

// Reranker 实现 model.Resolver。
func (r *ConfigResolver) Reranker(context.Context, model.ScopeRef) (model.Reranker, error) {
	if r.rerankErr != nil {
		return nil, r.rerankErr
	}
	return r.reranker, nil
}

// newEmbedder 按厂商标识构造向量化模型并探测维度。
func newEmbedder(ctx context.Context, opts Options) (model.Embedder, error) {
	provider := strings.ToLower(strings.TrimSpace(opts.EmbeddingProvider))
	modelName := strings.TrimSpace(opts.EmbeddingModel)
	if modelName == "" {
		return nil, fmt.Errorf("%w: 未配置 embedding 模型名", model.ErrNotConfigured)
	}

	switch provider {
	case ProviderOllama, "":
		instance := ollama.New(ollama.Options{
			BaseURL:   opts.EmbeddingBaseURL,
			Model:     modelName,
			BatchSize: opts.EmbeddingBatchSize,
		})
		// 探测维度顺带完成可用性校验；探到的维度已记录在实例上，
		// 之后通过 instance.Dimension() 读取，供向量集合创建与校验使用。
		if _, err := instance.DetectDimension(ctx); err != nil {
			return nil, fmt.Errorf("%w: 无法使用 Ollama 模型 %s（请确认服务已启动且模型已下载）: %w",
				model.ErrNotConfigured, modelName, err)
		}
		return instance, nil
	default:
		return nil, fmt.Errorf("不支持的 embedding 厂商 %q（已支持: %s）",
			opts.EmbeddingProvider, strings.Join(supportedProviders, ", "))
	}
}

// newReranker 按厂商标识构造重排模型。
//
// 与向量化不同，这里**不做启动期探测**：边车可能比后端晚就绪（首次还要下载模型），
// 在启动时硬校验会让「先起后端、再起边车」这个正常顺序变成启动失败。
// 边车是否真的加载了目标模型，由配置页的「测试连接」负责呈现。
func newReranker(opts Options) (model.Reranker, error) {
	modelName := strings.TrimSpace(opts.RerankModel)
	if modelName == "" {
		return nil, fmt.Errorf("%w: 未配置重排模型（可在「模型配置」里添加 TEI 模型并设为默认）",
			model.ErrNotConfigured)
	}

	provider := strings.ToLower(strings.TrimSpace(opts.RerankProvider))
	plugin, err := providers.Get(provider)
	if err != nil {
		return nil, fmt.Errorf("%w（重排配置里的 provider）: %s", model.ErrNotConfigured, err.Error())
	}
	if !providers.Supports(plugin, domainmodel.ModelTypeRerank) {
		return nil, fmt.Errorf("%w: 厂商 %s 不支持重排", model.ErrRerankUnsupported, plugin.DisplayName())
	}

	reranker, err := plugin.NewReranker(providers.Config{
		Provider:  plugin.ID(),
		BaseURL:   opts.RerankBaseURL,
		ModelName: modelName,
	})
	if err != nil {
		return nil, fmt.Errorf("构造重排模型失败: %w", err)
	}
	return reranker, nil
}
