package resolver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	enginemodel "einoflow-backend/internal/engine/model"
	"einoflow-backend/internal/engine/model/providers"
	"einoflow-backend/internal/model"
	"einoflow-backend/internal/repository"
	"einoflow-backend/internal/security"
)

// defaultCacheTTL 是解析结果（已构造好的模型实例）的缓存时长。
//
// 缓存的意义：构造一次向量化实现要探测维度（一次真实网络调用），
// 而检索是高频路径，不能每次都探测。除 TTL 外还提供 Invalidate，
// 配置一改就立即失效，不必等 TTL 到期。
const defaultCacheTTL = 60 * time.Second

// DatabaseResolver 从数据库解析当前生效的模型。
//
// 解析顺序（embedding）：知识库覆盖 → 应用覆盖 → 全局默认 → 配置兜底。
// **只有在「完全没有绑定」时才回落到配置文件里的模型**：
// 如果绑定了但配置有问题（凭证缺失、厂商不支持该能力），必须显式报错——
// 静默回落会让使用者以为配置生效了，实际跑的是另一套，这类问题极难发现。
type DatabaseResolver struct {
	cipher    *security.Cipher
	providers repository.ModelProviderRepository
	instances repository.ModelInstanceRepository
	bindings  repository.ModelBindingRepository
	// fallback 是数据库里没有任何绑定时的兜底来源（当前是配置文件）。
	fallback enginemodel.Resolver
	ttl      time.Duration

	mu sync.RWMutex
	// cache 同时缓存向量化与重排实现，键里带类型前缀（如 "embed:12" / "rerank:9"）。
	// 两类实现都是「构造一次要一次网络往返」的产物，用同一个 TTL 与同一套失效逻辑最省心。
	cache map[string]cachedModel
}

// cachedModel 是缓存的模型实现（值为 enginemodel.Embedder 或 enginemodel.Reranker）。
type cachedModel struct {
	value     any
	expiresAt time.Time
}

// 缓存键前缀。
const (
	cacheKeyEmbedder = "embed"
	cacheKeyReranker = "rerank"
)

// cacheKey 拼缓存键。
func cacheKey(kind string, instanceID uint64) string {
	return fmt.Sprintf("%s:%d", kind, instanceID)
}

// DatabaseOptions 是构造参数。
type DatabaseOptions struct {
	Cipher    *security.Cipher
	Providers repository.ModelProviderRepository
	Instances repository.ModelInstanceRepository
	Bindings  repository.ModelBindingRepository
	// Fallback 可选：没有任何绑定时的兜底解析器。
	Fallback enginemodel.Resolver
	// CacheTTL <= 0 时用默认值。
	CacheTTL time.Duration
}

// NewDatabase 构造数据库驱动的解析器。
func NewDatabase(opts DatabaseOptions) (*DatabaseResolver, error) {
	if opts.Cipher == nil {
		return nil, errors.New("缺少凭证解密器（security.Cipher）")
	}
	if opts.Providers == nil || opts.Instances == nil || opts.Bindings == nil {
		return nil, errors.New("缺少模型配置仓储依赖")
	}
	ttl := opts.CacheTTL
	if ttl <= 0 {
		ttl = defaultCacheTTL
	}
	return &DatabaseResolver{
		cipher:    opts.Cipher,
		providers: opts.Providers,
		instances: opts.Instances,
		bindings:  opts.Bindings,
		fallback:  opts.Fallback,
		ttl:       ttl,
		cache:     make(map[string]cachedModel),
	}, nil
}

// EffectiveModel 描述「当前生效的是哪个模型」，用于配置页展示与排查。
type EffectiveModel struct {
	// InstanceID 为 0 表示来自配置兜底（数据库里没有任何绑定）。
	InstanceID   uint64
	Provider     string
	ProviderName string
	ModelName    string
	ModelType    model.ModelType
	BaseURL      string
	Dimension    int
	// Scope 是生效绑定的来源作用域；来自兜底时为空。
	Scope model.ModelScope
}

// EffectiveEmbedder 是「生效模型 + 可用实例」的组合。
type EffectiveEmbedder struct {
	EffectiveModel
	Embedder enginemodel.Embedder
}

// EffectiveReranker 是「生效重排模型 + 可用实例」的组合。
type EffectiveReranker struct {
	EffectiveModel
	Reranker enginemodel.Reranker
}

// Effective 按模型类型返回生效模型信息，供配置页展示。
//
// 统一入口而不是按类型各写一个方法：配置页要展示的是「某类能力当前用哪个模型」，
// 三类能力走同一条解析链，没必要暴露三条几乎一样的路径。
func (r *DatabaseResolver) Effective(
	ctx context.Context, kind model.ModelType, scope enginemodel.ScopeRef,
) (EffectiveModel, error) {
	switch kind {
	case model.ModelTypeEmbedding:
		effective, err := r.EmbedderFor(ctx, scope)
		if err != nil {
			return EffectiveModel{}, err
		}
		return effective.EffectiveModel, nil
	case model.ModelTypeRerank:
		effective, err := r.RerankerFor(ctx, scope)
		if err != nil {
			return EffectiveModel{}, err
		}
		return effective.EffectiveModel, nil
	case model.ModelTypeLLM:
		// 对话模型还没有消费方（随 SSE 对话落地），这里明确说清楚，
		// 而不是回一个空模型让界面显示成「已就绪」。
		return EffectiveModel{}, fmt.Errorf("%w: 对话模型的调用尚未接入（V0.1 收尾时随流式对话落地）",
			enginemodel.ErrNotConfigured)
	default:
		return EffectiveModel{}, fmt.Errorf("不支持的模型类型 %q", kind)
	}
}

// Reranker 实现 enginemodel.Resolver。
func (r *DatabaseResolver) Reranker(ctx context.Context, scope enginemodel.ScopeRef) (enginemodel.Reranker, error) {
	effective, err := r.RerankerFor(ctx, scope)
	if err != nil {
		return nil, err
	}
	return effective.Reranker, nil
}

// RerankerFor 解析并返回当前生效的重排模型及其来源信息。
func (r *DatabaseResolver) RerankerFor(
	ctx context.Context, scope enginemodel.ScopeRef,
) (EffectiveReranker, error) {
	resolved, err := r.resolve(ctx, scope, model.ModelTypeRerank)
	if err != nil {
		return EffectiveReranker{}, err
	}

	if resolved == nil {
		// 数据库里没有绑定：回落到配置文件里的重排配置；没配就返回 ErrNotConfigured，
		// 由调用方决定「跳过重排但继续检索」。
		if r.fallback == nil {
			return EffectiveReranker{}, fmt.Errorf(
				"%w: 尚未配置重排模型（可在「模型配置」中添加 TEI 模型并设为默认）", enginemodel.ErrNotConfigured)
		}
		reranker, err := r.fallback.Reranker(ctx, scope)
		if err != nil {
			return EffectiveReranker{}, err
		}
		return EffectiveReranker{
			EffectiveModel: EffectiveModel{
				ModelName: reranker.Name(),
				ModelType: model.ModelTypeRerank,
			},
			Reranker: reranker,
		}, nil
	}

	reranker, err := r.rerankerFor(*resolved.instance, *resolved.provider)
	if err != nil {
		return EffectiveReranker{}, err
	}
	return EffectiveReranker{
		EffectiveModel: EffectiveModel{
			InstanceID:   resolved.instance.ID,
			Provider:     resolved.provider.Provider,
			ProviderName: resolved.provider.DisplayName,
			ModelName:    resolved.instance.ModelName,
			ModelType:    resolved.instance.ModelType,
			BaseURL:      resolved.provider.BaseURL,
			Scope:        resolved.scope,
		},
		Reranker: reranker,
	}, nil
}

// rerankerFor 构造（或取缓存）重排实现。
//
// 与向量化不同，这里构造时**不发起任何网络请求**：
// 重排客户端是纯 HTTP 封装，服务端是否就绪在调用时才暴露。
func (r *DatabaseResolver) rerankerFor(
	instance model.ModelInstance, provider model.ModelProvider,
) (enginemodel.Reranker, error) {
	if cached, ok := r.cachedReranker(instance.ID); ok {
		return cached, nil
	}

	plugin, err := providers.Get(provider.Provider)
	if err != nil {
		return nil, err
	}
	if !providers.Supports(plugin, instance.ModelType) {
		return nil, fmt.Errorf("%w: 厂商 %s 不支持 %s 类型（该厂商支持：%v）",
			enginemodel.ErrRerankUnsupported, plugin.DisplayName(), instance.ModelType, plugin.Kinds())
	}

	credentials, err := r.decryptCredentials(provider)
	if err != nil {
		return nil, err
	}

	reranker, err := plugin.NewReranker(providers.Config{
		Provider:    provider.Provider,
		BaseURL:     provider.BaseURL,
		ModelName:   instance.ModelName,
		Credentials: credentials,
	})
	if err != nil {
		return nil, err
	}

	r.cachePut(cacheKey(cacheKeyReranker, instance.ID), reranker)
	return reranker, nil
}

// cachedReranker 读取未过期的重排实现缓存。
func (r *DatabaseResolver) cachedReranker(instanceID uint64) (enginemodel.Reranker, bool) {
	value, ok := r.cacheGet(cacheKey(cacheKeyReranker, instanceID))
	if !ok {
		return nil, false
	}
	reranker, ok := value.(enginemodel.Reranker)
	return reranker, ok
}

// Embedder 实现 enginemodel.Resolver。
func (r *DatabaseResolver) Embedder(ctx context.Context, scope enginemodel.ScopeRef) (enginemodel.Embedder, error) {
	effective, err := r.EmbedderFor(ctx, scope)
	if err != nil {
		return nil, err
	}
	return effective.Embedder, nil
}

// EmbedderFor 解析并返回当前生效的向量化模型及其来源信息。
func (r *DatabaseResolver) EmbedderFor(ctx context.Context, scope enginemodel.ScopeRef) (EffectiveEmbedder, error) {
	resolved, err := r.resolve(ctx, scope, model.ModelTypeEmbedding)
	if err != nil {
		return EffectiveEmbedder{}, err
	}

	if resolved == nil {
		// 数据库里没有任何绑定：回落到配置文件里的模型。
		if r.fallback == nil {
			return EffectiveEmbedder{}, fmt.Errorf(
				"%w: 尚未配置向量化模型（请在「模型配置」中添加模型并设为默认）", enginemodel.ErrNotConfigured)
		}
		embedder, err := r.fallback.Embedder(ctx, scope)
		if err != nil {
			return EffectiveEmbedder{}, err
		}
		return EffectiveEmbedder{
			EffectiveModel: EffectiveModel{
				ModelName: embedder.Name(),
				ModelType: model.ModelTypeEmbedding,
				Dimension: embedder.Dimension(),
			},
			Embedder: embedder,
		}, nil
	}

	embedder, err := r.embedderFor(ctx, *resolved.instance, *resolved.provider)
	if err != nil {
		return EffectiveEmbedder{}, err
	}
	return EffectiveEmbedder{
		EffectiveModel: EffectiveModel{
			InstanceID:   resolved.instance.ID,
			Provider:     resolved.provider.Provider,
			ProviderName: resolved.provider.DisplayName,
			ModelName:    resolved.instance.ModelName,
			ModelType:    resolved.instance.ModelType,
			BaseURL:      resolved.provider.BaseURL,
			Dimension:    embedder.Dimension(),
			Scope:        resolved.scope,
		},
		Embedder: embedder,
	}, nil
}

// resolved 是一次成功解析的结果。
type resolved struct {
	binding  *model.ModelBinding
	instance *model.ModelInstance
	provider *model.ModelProvider
	scope    model.ModelScope
}

// resolve 按「知识库 → 应用 → 全局」的顺序找绑定；都没有时返回 nil, nil。
func (r *DatabaseResolver) resolve(
	ctx context.Context, scope enginemodel.ScopeRef, kind model.ModelType,
) (*resolved, error) {
	// 顺序即优先级：先看最具体的覆盖，再逐级向上。
	candidates := []struct {
		scope     model.ModelScope
		appID     uint64
		datasetID uint64
	}{
		{model.ModelScopeDataset, 0, scope.DatasetID},
		{model.ModelScopeApp, scope.AppID, 0},
		{model.ModelScopeGlobal, 0, 0},
	}

	for _, candidate := range candidates {
		// 没有对应 ID 的层级直接跳过，避免去查 scope=dataset、dataset_id=0 这种无意义记录。
		if candidate.scope == model.ModelScopeDataset && candidate.datasetID == 0 {
			continue
		}
		if candidate.scope == model.ModelScopeApp && candidate.appID == 0 {
			continue
		}

		binding, err := r.bindings.Get(ctx, candidate.scope, candidate.appID, candidate.datasetID, kind)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				continue
			}
			return nil, fmt.Errorf("查询模型绑定失败: %w", err)
		}

		instance, err := r.instances.GetByID(ctx, binding.ModelInstanceID)
		if err != nil {
			return nil, fmt.Errorf("绑定的模型不存在（绑定 %d → 模型 %d）: %w",
				binding.ID, binding.ModelInstanceID, err)
		}
		if !instance.Enabled {
			return nil, fmt.Errorf("生效的模型 %s（%s）已停用，请启用它或改绑其它模型",
				instance.ModelName, candidate.scope)
		}
		// 类型校验：把 embedding 绑成 llm 属于配置错误，必须当场说清楚，
		// 否则会在调用时以「接口返回 400」的形式暴露，排查链路长得多。
		if instance.ModelType != kind {
			return nil, fmt.Errorf("生效的模型 %s 是 %s 类型，不能用于 %s",
				instance.ModelName, instance.ModelType, kind)
		}

		provider, err := r.providers.GetByID(ctx, instance.ProviderID)
		if err != nil {
			return nil, fmt.Errorf("模型 %s 所属厂商配置不存在: %w", instance.ModelName, err)
		}
		if !provider.Enabled {
			return nil, fmt.Errorf("模型 %s 所属厂商 %s 已停用", instance.ModelName, provider.Provider)
		}

		return &resolved{binding: binding, instance: instance, provider: provider, scope: candidate.scope}, nil
	}
	return nil, nil
}

// embedderFor 构造（或取缓存）向量化实现。
func (r *DatabaseResolver) embedderFor(
	ctx context.Context, instance model.ModelInstance, provider model.ModelProvider,
) (enginemodel.Embedder, error) {
	if cached, ok := r.cachedEmbedder(instance.ID); ok {
		return cached, nil
	}

	plugin, err := providers.Get(provider.Provider)
	if err != nil {
		return nil, err
	}
	if !providers.Supports(plugin, instance.ModelType) {
		return nil, fmt.Errorf("厂商 %s 不支持 %s 类型（该厂商支持：%v）",
			plugin.DisplayName(), instance.ModelType, plugin.Kinds())
	}

	credentials, err := r.decryptCredentials(provider)
	if err != nil {
		return nil, err
	}

	// 注意：构造过程含一次真实探测（取维度），因此这里**不持锁**。
	// 持锁做网络调用会把所有并发的检索请求一起堵住；代价是并发首次解析可能重复构造一次，可接受。
	embedder, err := plugin.NewEmbedder(providers.Config{
		Provider:    provider.Provider,
		BaseURL:     provider.BaseURL,
		ModelName:   instance.ModelName,
		Credentials: credentials,
	})
	if err != nil {
		return nil, err
	}

	r.cachePut(cacheKey(cacheKeyEmbedder, instance.ID), embedder)
	return embedder, nil
}

// cachedEmbedder 读取未过期的向量化实现缓存。
func (r *DatabaseResolver) cachedEmbedder(instanceID uint64) (enginemodel.Embedder, bool) {
	value, ok := r.cacheGet(cacheKey(cacheKeyEmbedder, instanceID))
	if !ok {
		return nil, false
	}
	embedder, ok := value.(enginemodel.Embedder)
	return embedder, ok
}

// cacheGet 读取未过期的缓存项。
func (r *DatabaseResolver) cacheGet(key string) (any, bool) {
	r.mu.RLock()
	entry, ok := r.cache[key]
	r.mu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.value, true
}

// cachePut 写入缓存项。并发首次构造时后写者覆盖先写者，两者等价，无需加锁重查。
func (r *DatabaseResolver) cachePut(key string, value any) {
	r.mu.Lock()
	r.cache[key] = cachedModel{value: value, expiresAt: time.Now().Add(r.ttl)}
	r.mu.Unlock()
}

// decryptCredentials 解密凭证并解析成字段表。
//
// 解密失败必须报错而不是当作「空凭证」继续：那会变成「密钥换过 → 所有模型静默变成未鉴权调用 →
// 表现为一堆 401」，而真正的现场已经被抹掉了。
func (r *DatabaseResolver) decryptCredentials(provider model.ModelProvider) (map[string]string, error) {
	plaintext, err := r.cipher.Decrypt(provider.Credentials)
	if err != nil {
		return nil, fmt.Errorf("厂商 %s 的凭证无法解密: %w", provider.Provider, err)
	}
	if strings.TrimSpace(plaintext) == "" {
		return map[string]string{}, nil
	}
	parsed, err := parseCredentials(plaintext)
	if err != nil {
		return nil, fmt.Errorf("厂商 %s 的凭证格式不正确: %w", provider.Provider, err)
	}
	return parsed, nil
}

// Invalidate 清空解析缓存。
//
// 配置写入后必须调用：否则最长一个 TTL 之内，使用者会看到「明明改了配置却还在用旧模型」。
func (r *DatabaseResolver) Invalidate() {
	r.mu.Lock()
	r.cache = make(map[string]cachedModel)
	r.mu.Unlock()
}

// parseCredentials 把凭证 JSON 解析成字段表。
//
// 用 JSON 而不是「逗号分隔的字符串」：凭证字段会增多（key / secret / project），
// 用 JSON 存新增字段不必改表也不必改解析代码。
func parseCredentials(plaintext string) (map[string]string, error) {
	var parsed map[string]string
	if err := json.Unmarshal([]byte(plaintext), &parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}
