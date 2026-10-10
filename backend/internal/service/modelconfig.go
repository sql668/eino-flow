package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	enginemodel "einoflow-backend/internal/engine/model"
	"einoflow-backend/internal/engine/model/providers"
	"einoflow-backend/internal/engine/model/resolver"
	"einoflow-backend/internal/model"
	"einoflow-backend/internal/repository"
	"einoflow-backend/internal/security"
)

// maxCredentialRunes 限制单个凭证字段的长度。
// 凭证不该很长，超过这个量级基本是把别的内容粘错了。
const maxCredentialRunes = 2048

// ProviderView 是对外的厂商视图。
//
// 有一条硬性约束：**只回掩码，绝不回明文**。
// 因此这里没有存放任何原始凭证的字段，视图由服务层组装，不给「顺手带出明文」留口子。
type ProviderView struct {
	Provider         string
	DisplayName      string
	Kinds            []model.ModelType
	DefaultBaseURL   string
	BaseURL          string
	CredentialFields []providers.CredentialField
	// CredentialMask 是已存凭证的掩码；空表示尚未配置。
	CredentialMask string
	// Configured 表示数据库里已有该厂商的配置记录。
	Configured bool
	Enabled    bool
	ModelCount int
	UpdatedAt  time.Time
}

// ModelView 是对外的模型视图。
type ModelView struct {
	ID           uint64
	Provider     string
	ProviderName string
	ModelName    string
	ModelType    model.ModelType
	DisplayName  string
	Enabled      bool
	// BindingCount 是指向该模型的绑定数量，删除前给使用者一个判断依据。
	BindingCount int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// BindingView 是对外的绑定视图。
type BindingView struct {
	Scope        model.ModelScope
	AppID        uint64
	DatasetID    uint64
	ModelType    model.ModelType
	InstanceID   uint64
	ModelName    string
	Provider     string
	ProviderName string
	Enabled      bool
}

// SaveProviderInput 是保存厂商配置的入参。
type SaveProviderInput struct {
	Provider    string
	DisplayName string
	BaseURL     string
	// Credentials 为 nil 表示「不改动已存凭证」；空 map 表示清空。
	Credentials map[string]string
	Enabled     *bool
}

// TestProviderInput 是厂商连通性测试的入参。
//
// 支持带上**尚未保存**的凭证做测试：先测通再保存，比「先保存再发现密钥错」的体验好，
// 也避免测试产生垃圾配置。Credentials 为空时用已保存的凭证。
type TestProviderInput struct {
	Provider    string
	BaseURL     string
	ModelName   string
	Credentials map[string]string
}

// CreateModelInput 是新增模型的入参。
type CreateModelInput struct {
	Provider    string
	ModelName   string
	ModelType   model.ModelType
	DisplayName string
	Enabled     *bool
}

// UpdateModelInput 是更新模型的入参；指针字段为 nil 表示不改。
type UpdateModelInput struct {
	ID          uint64
	DisplayName *string
	Enabled     *bool
}

// BindModelInput 是绑定模型的入参。
type BindModelInput struct {
	Scope           model.ModelScope
	AppID           uint64
	DatasetID       uint64
	ModelType       model.ModelType
	ModelInstanceID uint64
}

// effectiveResolver 是服务需要的「生效模型解析」能力。
//
// 用接口而不是具体的 *resolver.DatabaseResolver：配置页要展示「当前生效模型」，
// 而单测需要打桩（真实解析会去连 Ollama / 数据库）。
type effectiveResolver interface {
	// Effective 按模型类型返回「当前生效的模型」。
	// 用类型参数而不是按类型各写一个方法：三类能力走同一条解析链，
	// 加一类能力（比如将来的语音）不该再改接口。
	Effective(ctx context.Context, kind model.ModelType, scope enginemodel.ScopeRef) (resolver.EffectiveModel, error)
	Invalidate()
}

// ModelConfigService 承载模型厂商、模型清单与绑定关系的管理。
type ModelConfigService struct {
	cipher    *security.Cipher
	providers repository.ModelProviderRepository
	instances repository.ModelInstanceRepository
	bindings  repository.ModelBindingRepository
	apps      repository.AppRepository
	datasets  repository.DatasetRepository
	resolver  effectiveResolver
}

// ModelConfigDeps 是服务依赖。
type ModelConfigDeps struct {
	Cipher    *security.Cipher
	Providers repository.ModelProviderRepository
	Instances repository.ModelInstanceRepository
	Bindings  repository.ModelBindingRepository
	Apps      repository.AppRepository
	Datasets  repository.DatasetRepository
	Resolver  effectiveResolver
}

// NewModelConfigService 构造模型配置服务。
func NewModelConfigService(deps ModelConfigDeps) *ModelConfigService {
	return &ModelConfigService{
		cipher:    deps.Cipher,
		providers: deps.Providers,
		instances: deps.Instances,
		bindings:  deps.Bindings,
		apps:      deps.Apps,
		datasets:  deps.Datasets,
		resolver:  deps.Resolver,
	}
}

// ListProviders 返回全部内置厂商，并带上本地配置状态。
func (s *ModelConfigService) ListProviders(ctx context.Context) ([]ProviderView, error) {
	saved, err := s.providers.List(ctx)
	if err != nil {
		return nil, err
	}
	byProvider := make(map[string]*model.ModelProvider, len(saved))
	for _, item := range saved {
		byProvider[item.Provider] = item
	}

	views := make([]ProviderView, 0, len(providers.All()))
	for _, plugin := range providers.All() {
		view := ProviderView{
			Provider:         plugin.ID(),
			DisplayName:      plugin.DisplayName(),
			Kinds:            plugin.Kinds(),
			DefaultBaseURL:   plugin.DefaultBaseURL(),
			CredentialFields: plugin.CredentialFields(),
			Enabled:          true,
		}
		if record, ok := byProvider[plugin.ID()]; ok {
			view.Configured = true
			view.BaseURL = record.BaseURL
			view.Enabled = record.Enabled
			view.UpdatedAt = record.UpdatedAt
			if record.DisplayName != "" {
				view.DisplayName = record.DisplayName
			}
			if mask, err := s.maskCredentials(record.Credentials); err != nil {
				return nil, err
			} else {
				view.CredentialMask = mask
			}
		}
		views = append(views, view)
	}
	return views, nil
}

// SaveProvider 保存厂商配置（不存在则创建）。
func (s *ModelConfigService) SaveProvider(ctx context.Context, in SaveProviderInput) (*ProviderView, error) {
	plugin, err := providers.Get(in.Provider)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidInput, err.Error())
	}

	record, err := s.providers.GetByProvider(ctx, plugin.ID())
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	if record == nil {
		record = &model.ModelProvider{Provider: plugin.ID(), Enabled: true}
	}

	record.BaseURL = strings.TrimSpace(in.BaseURL)
	record.DisplayName = strings.TrimSpace(in.DisplayName)
	if in.Enabled != nil {
		record.Enabled = *in.Enabled
	}

	// Credentials 为 nil 表示不改动：更新「地址」不应该顺手把凭证清掉。
	if in.Credentials != nil {
		if err := validateCredentials(plugin, in.Credentials); err != nil {
			return nil, err
		}
		encrypted, err := s.encryptCredentials(in.Credentials)
		if err != nil {
			return nil, err
		}
		record.Credentials = encrypted
	}

	if err := s.providers.Upsert(ctx, record); err != nil {
		return nil, err
	}
	s.resolver.Invalidate()

	views, err := s.ListProviders(ctx)
	if err != nil {
		return nil, err
	}
	for i := range views {
		if views[i].Provider == plugin.ID() {
			return &views[i], nil
		}
	}
	return nil, fmt.Errorf("保存后未找到厂商 %s 的配置", plugin.ID())
}

// DeleteProvider 删除厂商配置；仍有模型时拒绝删除。
func (s *ModelConfigService) DeleteProvider(ctx context.Context, provider string) error {
	plugin, err := providers.Get(provider)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidInput, err.Error())
	}
	record, err := s.providers.GetByProvider(ctx, plugin.ID())
	if err != nil {
		return translateError(err)
	}

	// 先挡住「删掉厂商但模型还在」：那会留下无法使用也无法解释的模型记录。
	count, err := s.instances.CountByProvider(ctx, record.ID)
	if err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("%w: 该厂商下还有 %d 个模型，请先删除模型", ErrInvalidInput, count)
	}

	if err := s.providers.Delete(ctx, plugin.ID()); err != nil {
		return translateError(err)
	}
	s.resolver.Invalidate()
	return nil
}

// TestProvider 测试厂商连通性（可用未保存的凭证）。
func (s *ModelConfigService) TestProvider(ctx context.Context, in TestProviderInput) (providers.ProbeResult, error) {
	plugin, err := providers.Get(in.Provider)
	if err != nil {
		return providers.ProbeResult{}, fmt.Errorf("%w: %s", ErrInvalidInput, err.Error())
	}

	credentials, err := s.resolveTestCredentials(ctx, plugin.ID(), in.Credentials)
	if err != nil {
		return providers.ProbeResult{}, err
	}
	if err := validateCredentials(plugin, credentials); err != nil {
		return providers.ProbeResult{}, err
	}

	result, err := plugin.Probe(ctx, providers.Config{
		Provider:    plugin.ID(),
		BaseURL:     in.BaseURL,
		ModelName:   in.ModelName,
		Credentials: credentials,
	})
	if err != nil {
		// 探测失败是配置问题而不是服务端故障，归为入参错误（400），
		// 让使用者能直接在界面上看到原因并改。
		return providers.ProbeResult{}, fmt.Errorf("%w: %s", ErrInvalidInput, err.Error())
	}
	return result, nil
}

// ListModels 返回模型列表，可按能力类型过滤。
func (s *ModelConfigService) ListModels(ctx context.Context, kind model.ModelType) ([]ModelView, error) {
	if kind != "" && !kind.IsValid() {
		return nil, fmt.Errorf("%w: 不支持的模型类型 %q", ErrInvalidInput, kind)
	}
	instances, err := s.instances.List(ctx, repository.ModelInstanceFilter{ModelType: kind})
	if err != nil {
		return nil, err
	}

	// 厂商信息一次读全，避免每个模型查一次厂商（N+1）。
	providerRecords, err := s.providers.List(ctx)
	if err != nil {
		return nil, err
	}
	providerByID := make(map[uint64]*model.ModelProvider, len(providerRecords))
	for _, item := range providerRecords {
		providerByID[item.ID] = item
	}

	views := make([]ModelView, 0, len(instances))
	for _, instance := range instances {
		view := ModelView{
			ID:          instance.ID,
			ModelName:   instance.ModelName,
			ModelType:   instance.ModelType,
			DisplayName: instance.DisplayName,
			Enabled:     instance.Enabled,
			CreatedAt:   instance.CreatedAt,
			UpdatedAt:   instance.UpdatedAt,
		}
		if record, ok := providerByID[instance.ProviderID]; ok {
			view.Provider = record.Provider
			view.ProviderName = record.DisplayName
			if view.ProviderName == "" {
				view.ProviderName = record.Provider
			}
		}
		if count, err := s.bindings.CountByInstance(ctx, instance.ID); err == nil {
			view.BindingCount = count
		}
		views = append(views, view)
	}
	return views, nil
}

// CreateModel 新增模型。
func (s *ModelConfigService) CreateModel(ctx context.Context, in CreateModelInput) (*ModelView, error) {
	plugin, err := providers.Get(in.Provider)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidInput, err.Error())
	}
	if !in.ModelType.IsValid() {
		return nil, fmt.Errorf("%w: 不支持的模型类型 %q（可选：llm / embedding / rerank）", ErrInvalidInput, in.ModelType)
	}
	if !providers.Supports(plugin, in.ModelType) {
		return nil, fmt.Errorf("%w: 厂商 %s 不支持 %s 类型（该厂商支持：%v）",
			ErrInvalidInput, plugin.DisplayName(), in.ModelType, plugin.Kinds())
	}
	modelName, err := validateRequiredName(in.ModelName, "模型名", 128)
	if err != nil {
		return nil, err
	}

	// 必须先有厂商配置：地址与凭证存在厂商记录里，没有它模型不知道自己该连哪里。
	record, err := s.providers.GetByProvider(ctx, plugin.ID())
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("%w: 请先保存厂商 %s 的配置（地址与凭证）", ErrInvalidInput, plugin.DisplayName())
		}
		return nil, err
	}

	instance := &model.ModelInstance{
		ProviderID:  record.ID,
		ModelName:   modelName,
		ModelType:   in.ModelType,
		DisplayName: strings.TrimSpace(in.DisplayName),
		Enabled:     true,
	}
	if in.Enabled != nil {
		instance.Enabled = *in.Enabled
	}
	if err := s.instances.Create(ctx, instance); err != nil {
		return nil, err
	}

	views, err := s.ListModels(ctx, "")
	if err != nil {
		return nil, err
	}
	for i := range views {
		if views[i].ID == instance.ID {
			return &views[i], nil
		}
	}
	return nil, fmt.Errorf("创建后未找到模型 %d", instance.ID)
}

// UpdateModel 更新模型的展示名与启用状态。
func (s *ModelConfigService) UpdateModel(ctx context.Context, in UpdateModelInput) (*ModelView, error) {
	instance, err := s.instances.GetByID(ctx, in.ID)
	if err != nil {
		return nil, translateError(err)
	}
	if in.DisplayName != nil {
		instance.DisplayName = strings.TrimSpace(*in.DisplayName)
	}
	if in.Enabled != nil {
		instance.Enabled = *in.Enabled
	}
	if err := s.instances.Update(ctx, instance); err != nil {
		return nil, translateError(err)
	}
	// 停用 / 启用都会影响解析结果，必须让缓存立即失效。
	s.resolver.Invalidate()

	views, err := s.ListModels(ctx, "")
	if err != nil {
		return nil, err
	}
	for i := range views {
		if views[i].ID == instance.ID {
			return &views[i], nil
		}
	}
	return nil, fmt.Errorf("更新后未找到模型 %d", instance.ID)
}

// DeleteModel 删除模型（同时删除指向它的绑定）。
func (s *ModelConfigService) DeleteModel(ctx context.Context, id uint64) error {
	if err := s.instances.Delete(ctx, id); err != nil {
		return translateError(err)
	}
	s.resolver.Invalidate()
	return nil
}

// TestModel 测试模型是否真的可用。
//
// embedding 会真实构造一次实现（含维度探测）——这才是「能不能用」的答案；
// llm / rerank 尚无实现，退化为厂商级探测并校验模型名是否在列表里。
func (s *ModelConfigService) TestModel(ctx context.Context, id uint64) (providers.ProbeResult, error) {
	instance, err := s.instances.GetByID(ctx, id)
	if err != nil {
		return providers.ProbeResult{}, translateError(err)
	}
	record, err := s.providers.GetByID(ctx, instance.ProviderID)
	if err != nil {
		return providers.ProbeResult{}, translateError(err)
	}
	plugin, err := providers.Get(record.Provider)
	if err != nil {
		return providers.ProbeResult{}, fmt.Errorf("%w: %s", ErrInvalidInput, err.Error())
	}

	credentials, err := s.decryptCredentials(record)
	if err != nil {
		return providers.ProbeResult{}, err
	}
	cfg := providers.Config{
		Provider:    record.Provider,
		BaseURL:     record.BaseURL,
		ModelName:   instance.ModelName,
		Credentials: credentials,
	}

	if instance.ModelType == model.ModelTypeEmbedding {
		started := time.Now()
		embedder, err := plugin.NewEmbedder(cfg)
		elapsed := int(time.Since(started).Milliseconds())
		if err != nil {
			return providers.ProbeResult{}, fmt.Errorf("%w: %s", ErrInvalidInput, err.Error())
		}
		return providers.ProbeResult{
			OK:             true,
			Message:        fmt.Sprintf("模型可用，向量维度 %d", embedder.Dimension()),
			ModelAvailable: true,
			LatencyMS:      elapsed,
		}, nil
	}

	result, err := plugin.Probe(ctx, cfg)
	if err != nil {
		return providers.ProbeResult{}, fmt.Errorf("%w: %s", ErrInvalidInput, err.Error())
	}
	return result, nil
}

// BindModel 绑定模型（全局默认 / 应用覆盖 / 知识库覆盖）。
func (s *ModelConfigService) BindModel(ctx context.Context, in BindModelInput) error {
	if !in.Scope.IsValid() {
		return fmt.Errorf("%w: 不支持的绑定作用域 %q（可选：global / app / dataset）", ErrInvalidInput, in.Scope)
	}

	// 作用域与 ID 必须配套：scope=app 却不给 appId，绑定会落到 app_id=0 上，
	// 既不会生效也查不出来，属于静默错误的典型，必须当场拒绝。
	switch in.Scope {
	case model.ModelScopeApp:
		if in.AppID == 0 {
			return fmt.Errorf("%w: 应用级绑定必须指定 appId", ErrInvalidInput)
		}
		if _, err := s.apps.GetByID(ctx, in.AppID); err != nil {
			return fmt.Errorf("%w: 应用 %d 不存在", ErrInvalidInput, in.AppID)
		}
	case model.ModelScopeDataset:
		if in.DatasetID == 0 {
			return fmt.Errorf("%w: 知识库级绑定必须指定 datasetId", ErrInvalidInput)
		}
		if _, err := s.datasets.GetByID(ctx, in.DatasetID); err != nil {
			return fmt.Errorf("%w: 知识库 %d 不存在", ErrInvalidInput, in.DatasetID)
		}
	}

	instance, err := s.instances.GetByID(ctx, in.ModelInstanceID)
	if err != nil {
		return fmt.Errorf("%w: 模型 %d 不存在", ErrInvalidInput, in.ModelInstanceID)
	}
	if instance.ModelType != in.ModelType {
		return fmt.Errorf("%w: 模型 %s 是 %s 类型，不能绑定为 %s",
			ErrInvalidInput, instance.ModelName, instance.ModelType, in.ModelType)
	}
	if !instance.Enabled {
		return fmt.Errorf("%w: 模型 %s 已停用，请先启用", ErrInvalidInput, instance.ModelName)
	}

	binding := &model.ModelBinding{
		Scope:           in.Scope,
		AppID:           in.AppID,
		DatasetID:       in.DatasetID,
		ModelType:       in.ModelType,
		ModelInstanceID: in.ModelInstanceID,
	}
	if in.Scope == model.ModelScopeGlobal {
		// 全局绑定的作用域 ID 一律归零，避免历史脏数据让「全局」变成一条查不到的记录。
		binding.AppID = 0
		binding.DatasetID = 0
	}
	if err := s.bindings.Upsert(ctx, binding); err != nil {
		return err
	}
	s.resolver.Invalidate()
	return nil
}

// UnbindModel 解绑；解绑后该作用域回落到上一级覆盖或配置兜底。
func (s *ModelConfigService) UnbindModel(
	ctx context.Context, scope model.ModelScope, appID, datasetID uint64, kind model.ModelType,
) error {
	if !scope.IsValid() {
		return fmt.Errorf("%w: 不支持的绑定作用域 %q", ErrInvalidInput, scope)
	}
	if err := s.bindings.Delete(ctx, scope, appID, datasetID, kind); err != nil {
		return translateError(err)
	}
	s.resolver.Invalidate()
	return nil
}

// ListBindings 返回某作用域下的全部绑定。
func (s *ModelConfigService) ListBindings(
	ctx context.Context, scope model.ModelScope, appID, datasetID uint64,
) ([]BindingView, error) {
	if !scope.IsValid() {
		return nil, fmt.Errorf("%w: 不支持的绑定作用域 %q", ErrInvalidInput, scope)
	}
	rows, err := s.bindings.ListByScope(ctx, scope, appID, datasetID)
	if err != nil {
		return nil, err
	}

	views := make([]BindingView, 0, len(rows))
	for _, row := range rows {
		view := BindingView{
			Scope:      row.Scope,
			AppID:      row.AppID,
			DatasetID:  row.DatasetID,
			ModelType:  row.ModelType,
			InstanceID: row.ModelInstanceID,
		}
		if instance, err := s.instances.GetByID(ctx, row.ModelInstanceID); err == nil {
			view.ModelName = instance.ModelName
			view.Enabled = instance.Enabled
			if provider, err := s.providers.GetByID(ctx, instance.ProviderID); err == nil {
				view.Provider = provider.Provider
				view.ProviderName = provider.DisplayName
			}
		}
		views = append(views, view)
	}
	return views, nil
}

// EffectiveModel 返回当前生效的模型。
//
// 这是配置页最有价值的一块信息：它回答「现在到底在用哪个模型」，
// 而不是「我配了哪些模型」。三级覆盖 + 配置兜底叠加起来，光看列表很难推断结果。
func (s *ModelConfigService) EffectiveModel(
	ctx context.Context, kind model.ModelType, appID, datasetID uint64,
) (resolver.EffectiveModel, error) {
	if !kind.IsValid() {
		return resolver.EffectiveModel{}, fmt.Errorf("%w: 不支持的模型类型 %q", ErrInvalidInput, kind)
	}
	effective, err := s.resolver.Effective(
		ctx, kind, enginemodel.ScopeRef{AppID: appID, DatasetID: datasetID})
	if err != nil {
		return resolver.EffectiveModel{}, translateError(err)
	}
	return effective, nil
}

// resolveTestCredentials 取测试用凭证：优先用入参，否则用已保存的。
func (s *ModelConfigService) resolveTestCredentials(
	ctx context.Context, provider string, provided map[string]string,
) (map[string]string, error) {
	if provided != nil {
		return provided, nil
	}
	record, err := s.providers.GetByProvider(ctx, provider)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			// 没存过配置也没提供凭证：交给必填校验去报错，这里返回空表即可。
			return map[string]string{}, nil
		}
		return nil, err
	}
	return s.decryptCredentials(record)
}

// encryptCredentials 把凭证字段序列化后加密。
func (s *ModelConfigService) encryptCredentials(credentials map[string]string) (string, error) {
	cleaned := make(map[string]string, len(credentials))
	for key, value := range credentials {
		cleaned[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	payload, err := json.Marshal(cleaned)
	if err != nil {
		return "", fmt.Errorf("序列化凭证失败: %w", err)
	}
	encrypted, err := s.cipher.Encrypt(string(payload))
	if err != nil {
		return "", fmt.Errorf("加密凭证失败: %w", err)
	}
	return encrypted, nil
}

// decryptCredentials 解密凭证。
func (s *ModelConfigService) decryptCredentials(record *model.ModelProvider) (map[string]string, error) {
	plaintext, err := s.cipher.Decrypt(record.Credentials)
	if err != nil {
		return nil, fmt.Errorf("%w: 厂商 %s 的凭证无法解密（加密密钥是否更换过？）",
			ErrInvalidInput, record.Provider)
	}
	if strings.TrimSpace(plaintext) == "" {
		return map[string]string{}, nil
	}
	var parsed map[string]string
	if err := json.Unmarshal([]byte(plaintext), &parsed); err != nil {
		return nil, fmt.Errorf("凭证格式不正确: %w", err)
	}
	return parsed, nil
}

// maskCredentials 生成凭证掩码：取所有非空字段的掩码并排序，拼成一行展示。
func (s *ModelConfigService) maskCredentials(encrypted string) (string, error) {
	plaintext, err := s.cipher.Decrypt(encrypted)
	if err != nil {
		return "", fmt.Errorf("%w: 凭证无法解密（加密密钥是否更换过？）", ErrInvalidInput)
	}
	if strings.TrimSpace(plaintext) == "" {
		return "", nil
	}
	var parsed map[string]string
	if err := json.Unmarshal([]byte(plaintext), &parsed); err != nil {
		return "", fmt.Errorf("凭证格式不正确: %w", err)
	}

	keys := make([]string, 0, len(parsed))
	for key := range parsed {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		if strings.TrimSpace(parsed[key]) == "" {
			continue
		}
		parts = append(parts, security.Mask(parsed[key]))
	}
	return strings.Join(parts, " / "), nil
}

// validateCredentials 按厂商声明的字段校验凭证。
func validateCredentials(plugin providers.Provider, credentials map[string]string) error {
	for _, field := range plugin.CredentialFields() {
		value := strings.TrimSpace(credentials[field.Key])
		if field.Required && value == "" {
			return fmt.Errorf("%w: %s 不能为空", ErrInvalidInput, field.Label)
		}
		if len([]rune(value)) > maxCredentialRunes {
			return fmt.Errorf("%w: %s 过长（最多 %d 个字符）", ErrInvalidInput, field.Label, maxCredentialRunes)
		}
	}
	return nil
}
