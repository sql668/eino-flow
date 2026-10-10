package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	enginemodel "einoflow-backend/internal/engine/model"
	"einoflow-backend/internal/engine/model/resolver"
	"einoflow-backend/internal/model"
	"einoflow-backend/internal/repository"
	"einoflow-backend/internal/security"
	"einoflow-backend/internal/service"
)

// ---------- 内存版仓储替身 ----------

// fakeModelStore 用一个结构体承载全部模型配置相关的替身，
// 便于测试里直接检查「库里到底存了什么」（尤其是凭证必须是密文）。
type fakeModelStore struct {
	providers map[string]*model.ModelProvider
	instances map[uint64]*model.ModelInstance
	bindings  []*model.ModelBinding
	nextID    uint64
}

func newFakeModelStore() *fakeModelStore {
	return &fakeModelStore{
		providers: map[string]*model.ModelProvider{},
		instances: map[uint64]*model.ModelInstance{},
	}
}

func (f *fakeModelStore) Upsert(_ context.Context, provider *model.ModelProvider) error {
	copied := *provider
	f.nextID++
	copied.ID = f.nextID
	f.providers[provider.Provider] = &copied
	return nil
}

func (f *fakeModelStore) GetByProvider(_ context.Context, provider string) (*model.ModelProvider, error) {
	record, ok := f.providers[provider]
	if !ok {
		return nil, repository.ErrNotFound
	}
	copied := *record
	return &copied, nil
}

func (f *fakeModelStore) GetByID(_ context.Context, id uint64) (*model.ModelProvider, error) {
	for _, record := range f.providers {
		if record.ID == id {
			copied := *record
			return &copied, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (f *fakeModelStore) List(context.Context) ([]*model.ModelProvider, error) {
	items := make([]*model.ModelProvider, 0, len(f.providers))
	for _, record := range f.providers {
		copied := *record
		items = append(items, &copied)
	}
	return items, nil
}

func (f *fakeModelStore) Delete(_ context.Context, provider string) error {
	if _, ok := f.providers[provider]; !ok {
		return repository.ErrNotFound
	}
	delete(f.providers, provider)
	return nil
}

func (f *fakeModelStore) Create(_ context.Context, instance *model.ModelInstance) error {
	f.nextID++
	copied := *instance
	copied.ID = f.nextID
	f.instances[copied.ID] = &copied
	instance.ID = copied.ID
	return nil
}

func (f *fakeModelStore) GetInstanceByID(_ context.Context, id uint64) (*model.ModelInstance, error) {
	record, ok := f.instances[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	copied := *record
	return &copied, nil
}

func (f *fakeModelStore) ListInstances(
	_ context.Context, filter repository.ModelInstanceFilter,
) ([]*model.ModelInstance, error) {
	items := make([]*model.ModelInstance, 0, len(f.instances))
	for _, record := range f.instances {
		if filter.ModelType != "" && record.ModelType != filter.ModelType {
			continue
		}
		copied := *record
		items = append(items, &copied)
	}
	return items, nil
}

func (f *fakeModelStore) UpdateInstance(_ context.Context, instance *model.ModelInstance) error {
	if _, ok := f.instances[instance.ID]; !ok {
		return repository.ErrNotFound
	}
	copied := *instance
	f.instances[instance.ID] = &copied
	return nil
}

func (f *fakeModelStore) DeleteInstance(_ context.Context, id uint64) error {
	if _, ok := f.instances[id]; !ok {
		return repository.ErrNotFound
	}
	delete(f.instances, id)
	return nil
}

func (f *fakeModelStore) CountByProvider(_ context.Context, providerID uint64) (int64, error) {
	var count int64
	for _, record := range f.instances {
		if record.ProviderID == providerID {
			count++
		}
	}
	return count, nil
}

func (f *fakeModelStore) UpsertBinding(_ context.Context, binding *model.ModelBinding) error {
	for i, existing := range f.bindings {
		if existing.Scope == binding.Scope && existing.AppID == binding.AppID &&
			existing.DatasetID == binding.DatasetID && existing.ModelType == binding.ModelType {
			copied := *binding
			f.bindings[i] = &copied
			return nil
		}
	}
	copied := *binding
	f.bindings = append(f.bindings, &copied)
	return nil
}

func (f *fakeModelStore) GetBinding(
	_ context.Context, scope model.ModelScope, appID, datasetID uint64, kind model.ModelType,
) (*model.ModelBinding, error) {
	for _, existing := range f.bindings {
		if existing.Scope == scope && existing.AppID == appID &&
			existing.DatasetID == datasetID && existing.ModelType == kind {
			copied := *existing
			return &copied, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (f *fakeModelStore) ListBindings(
	_ context.Context, scope model.ModelScope, appID, datasetID uint64,
) ([]*model.ModelBinding, error) {
	items := []*model.ModelBinding{}
	for _, existing := range f.bindings {
		if existing.Scope == scope && existing.AppID == appID && existing.DatasetID == datasetID {
			copied := *existing
			items = append(items, &copied)
		}
	}
	return items, nil
}

func (f *fakeModelStore) DeleteBinding(
	_ context.Context, scope model.ModelScope, appID, datasetID uint64, kind model.ModelType,
) error {
	for i, existing := range f.bindings {
		if existing.Scope == scope && existing.AppID == appID &&
			existing.DatasetID == datasetID && existing.ModelType == kind {
			f.bindings = append(f.bindings[:i], f.bindings[i+1:]...)
			return nil
		}
	}
	return repository.ErrNotFound
}

func (f *fakeModelStore) CountByInstance(_ context.Context, instanceID uint64) (int64, error) {
	var count int64
	for _, existing := range f.bindings {
		if existing.ModelInstanceID == instanceID {
			count++
		}
	}
	return count, nil
}

// providerRepo 适配 repository.ModelProviderRepository。
type providerRepo struct{ *fakeModelStore }

func (r providerRepo) GetByProvider(ctx context.Context, provider string) (*model.ModelProvider, error) {
	return r.fakeModelStore.GetByProvider(ctx, provider)
}

// instanceRepo 适配 repository.ModelInstanceRepository。
type instanceRepo struct{ *fakeModelStore }

func (r instanceRepo) GetByID(ctx context.Context, id uint64) (*model.ModelInstance, error) {
	return r.fakeModelStore.GetInstanceByID(ctx, id)
}

func (r instanceRepo) List(
	ctx context.Context, filter repository.ModelInstanceFilter,
) ([]*model.ModelInstance, error) {
	return r.fakeModelStore.ListInstances(ctx, filter)
}

func (r instanceRepo) Update(ctx context.Context, instance *model.ModelInstance) error {
	return r.fakeModelStore.UpdateInstance(ctx, instance)
}

func (r instanceRepo) Delete(ctx context.Context, id uint64) error {
	return r.fakeModelStore.DeleteInstance(ctx, id)
}

// bindingRepo 适配 repository.ModelBindingRepository。
type bindingRepo struct{ *fakeModelStore }

func (r bindingRepo) Upsert(ctx context.Context, binding *model.ModelBinding) error {
	return r.fakeModelStore.UpsertBinding(ctx, binding)
}

func (r bindingRepo) Get(
	ctx context.Context, scope model.ModelScope, appID, datasetID uint64, kind model.ModelType,
) (*model.ModelBinding, error) {
	return r.fakeModelStore.GetBinding(ctx, scope, appID, datasetID, kind)
}

func (r bindingRepo) ListByScope(
	ctx context.Context, scope model.ModelScope, appID, datasetID uint64,
) ([]*model.ModelBinding, error) {
	return r.fakeModelStore.ListBindings(ctx, scope, appID, datasetID)
}

func (r bindingRepo) Delete(
	ctx context.Context, scope model.ModelScope, appID, datasetID uint64, kind model.ModelType,
) error {
	return r.fakeModelStore.DeleteBinding(ctx, scope, appID, datasetID, kind)
}

func (r bindingRepo) CountByInstance(ctx context.Context, instanceID uint64) (int64, error) {
	return r.fakeModelStore.CountByInstance(ctx, instanceID)
}

// existingAppRepo / existingDatasetRepo：只支持「存在性校验」。
type existingAppRepo struct{ exists map[uint64]bool }

func (r existingAppRepo) Create(context.Context, *model.App) error { panic("未预期调用") }

func (r existingAppRepo) GetByID(_ context.Context, id uint64) (*model.App, error) {
	if r.exists[id] {
		return &model.App{ID: id}, nil
	}
	return nil, repository.ErrNotFound
}

func (r existingAppRepo) List(context.Context, repository.AppListFilter) ([]*model.App, int64, error) {
	panic("未预期调用")
}

func (r existingAppRepo) Update(context.Context, *model.App) error { panic("未预期调用") }
func (r existingAppRepo) SoftDelete(context.Context, uint64) error { panic("未预期调用") }

type existingDatasetRepo struct{ exists map[uint64]bool }

func (r existingDatasetRepo) Create(context.Context, *model.Dataset) error { panic("未预期调用") }

func (r existingDatasetRepo) GetByID(_ context.Context, id uint64) (*model.Dataset, error) {
	if r.exists[id] {
		return &model.Dataset{ID: id}, nil
	}
	return nil, repository.ErrNotFound
}

func (r existingDatasetRepo) List(
	context.Context, repository.DatasetListFilter,
) ([]*model.Dataset, int64, error) {
	panic("未预期调用")
}

func (r existingDatasetRepo) Update(context.Context, *model.Dataset) error { panic("未预期调用") }
func (r existingDatasetRepo) SoftDelete(context.Context, uint64) error     { panic("未预期调用") }

// stubResolver 记录缓存失效次数，并返回预设的「生效模型」。
type stubResolver struct {
	effective  resolver.EffectiveModel
	err        error
	invalidatd int
}

func (r *stubResolver) Effective(
	_ context.Context, _ model.ModelType, _ enginemodel.ScopeRef,
) (resolver.EffectiveModel, error) {
	if r.err != nil {
		return resolver.EffectiveModel{}, r.err
	}
	return r.effective, nil
}

func (r *stubResolver) ChatModel(
	context.Context, enginemodel.ScopeRef,
) (enginemodel.ChatModel, error) {
	return nil, enginemodel.ErrNotConfigured
}

func (r *stubResolver) Invalidate() { r.invalidatd++ }

// ---------- 测试脚手架 ----------

type modelConfigFixture struct {
	service  *service.ModelConfigService
	store    *fakeModelStore
	resolver *stubResolver
	cipher   *security.Cipher
}

func newModelConfigFixture(t *testing.T) *modelConfigFixture {
	t.Helper()
	cipher, err := security.NewCipher([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("构造加密器失败: %v", err)
	}
	store := newFakeModelStore()
	stub := &stubResolver{}
	return &modelConfigFixture{
		service: service.NewModelConfigService(service.ModelConfigDeps{
			Cipher:    cipher,
			Providers: providerRepo{store},
			Instances: instanceRepo{store},
			Bindings:  bindingRepo{store},
			Apps:      existingAppRepo{exists: map[uint64]bool{1: true}},
			Datasets:  existingDatasetRepo{exists: map[uint64]bool{2: true}},
			Resolver:  stub,
		}),
		store:    store,
		resolver: stub,
		cipher:   cipher,
	}
}

// ---------- 用例 ----------

// TestSaveProvider_EncryptsAndMasks 是这一块最重要的一条：
// 凭证必须密文落库、掩码出接口，任何一处泄漏明文都算失败。
func TestSaveProvider_EncryptsAndMasks(t *testing.T) {
	fixture := newModelConfigFixture(t)
	ctx := context.Background()

	const secret = "sk-super-secret-value-123456"
	view, err := fixture.service.SaveProvider(ctx, service.SaveProviderInput{
		Provider:    "openai_compatible",
		BaseURL:     "https://api.example.com/v1",
		Credentials: map[string]string{"api_key": secret},
	})
	if err != nil {
		t.Fatalf("保存厂商失败: %v", err)
	}
	if !view.Configured || view.BaseURL != "https://api.example.com/v1" {
		t.Fatalf("视图不符: %+v", view)
	}
	// 厂商说明也随视图返回：只给名字，使用者不知道这个厂商是干什么的。
	if view.Description == "" {
		t.Error("未带出厂商说明")
	}
	// 内置厂商的名称以代码为准（不接受改名），避免界面显示历史旧名。
	if view.DisplayName != "OpenAI 兼容接口" {
		t.Errorf("展示名 = %q, 期望使用代码里的名称", view.DisplayName)
	}
	if view.CredentialMask == "" || strings.Contains(view.CredentialMask, secret) {
		t.Fatalf("掩码不符（可能泄漏明文）: %q", view.CredentialMask)
	}
	if !strings.Contains(view.CredentialMask, "***") {
		t.Errorf("掩码未做遮蔽: %q", view.CredentialMask)
	}

	// 落库的必须是密文，且能用同一把密钥还原出原始字段。
	stored := fixture.store.providers["openai_compatible"]
	if strings.Contains(stored.Credentials, secret) {
		t.Fatal("数据库里存了明文凭证")
	}
	plaintext, err := fixture.cipher.Decrypt(stored.Credentials)
	if err != nil {
		t.Fatalf("解密落库凭证失败: %v", err)
	}
	if plaintext != `{"api_key":"`+secret+`"}` {
		t.Errorf("落库内容 = %s, 期望包含原始字段", plaintext)
	}

	// 列表接口同样不能出现明文。
	views, err := fixture.service.ListProviders(ctx)
	if err != nil {
		t.Fatalf("查询厂商列表失败: %v", err)
	}
	for _, item := range views {
		if strings.Contains(item.CredentialMask, secret) {
			t.Fatal("列表接口泄漏了明文凭证")
		}
	}
}

// TestSaveProvider_KeepsExistingCredentials 覆盖「只改地址不填凭证」的场景：
// 不能把已存的凭证顺手清掉。
func TestSaveProvider_KeepsExistingCredentials(t *testing.T) {
	fixture := newModelConfigFixture(t)
	ctx := context.Background()

	if _, err := fixture.service.SaveProvider(ctx, service.SaveProviderInput{
		Provider: "openai_compatible", Credentials: map[string]string{"api_key": "sk-first"},
	}); err != nil {
		t.Fatalf("首次保存失败: %v", err)
	}
	before := fixture.store.providers["openai_compatible"].Credentials

	// Credentials 传 nil 表示不改动。
	if _, err := fixture.service.SaveProvider(ctx, service.SaveProviderInput{
		Provider: "openai_compatible", BaseURL: "https://new.example.com/v1",
	}); err != nil {
		t.Fatalf("二次保存失败: %v", err)
	}
	after := fixture.store.providers["openai_compatible"]
	if after.Credentials != before {
		t.Error("未传凭证时不应改动已存凭证")
	}
	if after.BaseURL != "https://new.example.com/v1" {
		t.Errorf("地址未更新: %s", after.BaseURL)
	}
}

// TestSaveProvider_ValidatesRequiredCredentials 覆盖必填凭证校验。
func TestSaveProvider_ValidatesRequiredCredentials(t *testing.T) {
	fixture := newModelConfigFixture(t)

	_, err := fixture.service.SaveProvider(context.Background(), service.SaveProviderInput{
		Provider: "openai_compatible", Credentials: map[string]string{"api_key": "  "},
	})
	if !errors.Is(err, service.ErrInvalidInput) {
		t.Fatalf("错误 = %v, 期望 ErrInvalidInput", err)
	}
	if !strings.Contains(err.Error(), "API Key") {
		t.Errorf("提示未指明缺哪个字段: %v", err)
	}

	// Ollama 的令牌是可选的：不填也不该报错。
	if _, err := fixture.service.SaveProvider(context.Background(), service.SaveProviderInput{
		Provider: "ollama", Credentials: map[string]string{"api_key": ""},
	}); err != nil {
		t.Fatalf("Ollama 可选凭证不应报错: %v", err)
	}
}

// TestDeleteProvider_BlockedByModels 覆盖「厂商下还有模型时拒绝删除」。
func TestDeleteProvider_BlockedByModels(t *testing.T) {
	fixture := newModelConfigFixture(t)
	ctx := context.Background()

	if _, err := fixture.service.SaveProvider(ctx, service.SaveProviderInput{Provider: "ollama"}); err != nil {
		t.Fatalf("保存厂商失败: %v", err)
	}
	if _, err := fixture.service.CreateModel(ctx, service.CreateModelInput{
		Provider: "ollama", ModelName: "bge-m3:latest", ModelType: model.ModelTypeEmbedding,
	}); err != nil {
		t.Fatalf("创建模型失败: %v", err)
	}

	err := fixture.service.DeleteProvider(ctx, "ollama")
	if !errors.Is(err, service.ErrInvalidInput) || !strings.Contains(err.Error(), "先删除模型") {
		t.Fatalf("错误 = %v, 期望提示先删除模型", err)
	}

	// 清空模型后应可删除，并且缓存要失效。
	models, err := fixture.service.ListModels(ctx, "")
	if err != nil {
		t.Fatalf("查询模型失败: %v", err)
	}
	if err := fixture.service.DeleteModel(ctx, models[0].ID); err != nil {
		t.Fatalf("删除模型失败: %v", err)
	}
	invalidationsBefore := fixture.resolver.invalidatd
	if err := fixture.service.DeleteProvider(ctx, "ollama"); err != nil {
		t.Fatalf("删除厂商失败: %v", err)
	}
	if fixture.resolver.invalidatd <= invalidationsBefore {
		t.Error("删除厂商后未失效解析缓存")
	}
}

// TestCreateModel_Validation 覆盖模型创建的各类拒绝路径。
func TestCreateModel_Validation(t *testing.T) {
	fixture := newModelConfigFixture(t)
	ctx := context.Background()

	// 未知厂商。
	if _, err := fixture.service.CreateModel(ctx, service.CreateModelInput{
		Provider: "nope", ModelName: "x", ModelType: model.ModelTypeEmbedding,
	}); !errors.Is(err, service.ErrInvalidInput) {
		t.Errorf("未知厂商错误 = %v, 期望 ErrInvalidInput", err)
	}

	// 厂商不支持该能力：TEI 不做向量化。
	if _, err := fixture.service.CreateModel(ctx, service.CreateModelInput{
		Provider: "tei", ModelName: "x", ModelType: model.ModelTypeEmbedding,
	}); !errors.Is(err, service.ErrInvalidInput) {
		t.Errorf("能力不匹配错误 = %v, 期望 ErrInvalidInput", err)
	}

	// 尚未保存厂商配置：必须提示先保存，而不是创建一个不知道连哪儿的模型。
	if _, err := fixture.service.CreateModel(ctx, service.CreateModelInput{
		Provider: "ollama", ModelName: "bge-m3:latest", ModelType: model.ModelTypeEmbedding,
	}); !errors.Is(err, service.ErrInvalidInput) || !strings.Contains(err.Error(), "请先保存厂商") {
		t.Errorf("未配置厂商错误 = %v, 期望提示先保存厂商配置", err)
	}

	// 正常路径。
	if _, err := fixture.service.SaveProvider(ctx, service.SaveProviderInput{Provider: "ollama"}); err != nil {
		t.Fatalf("保存厂商失败: %v", err)
	}
	view, err := fixture.service.CreateModel(ctx, service.CreateModelInput{
		Provider: "ollama", ModelName: " bge-m3:latest ", ModelType: model.ModelTypeEmbedding,
	})
	if err != nil {
		t.Fatalf("创建模型失败: %v", err)
	}
	if view.ModelName != "bge-m3:latest" {
		t.Errorf("模型名未去空白: %q", view.ModelName)
	}
	// 展示名以代码为准，而不是库里的历史值：改过名字的老环境不该继续显示旧名。
	if view.ProviderName != "Ollama（本地模型）" {
		t.Errorf("厂商展示名 = %q, 期望使用代码里的名称", view.ProviderName)
	}
}

// TestBindModel 覆盖绑定校验与正常绑定。
func TestBindModel(t *testing.T) {
	fixture := newModelConfigFixture(t)
	ctx := context.Background()

	if _, err := fixture.service.SaveProvider(ctx, service.SaveProviderInput{Provider: "ollama"}); err != nil {
		t.Fatalf("保存厂商失败: %v", err)
	}
	instance, err := fixture.service.CreateModel(ctx, service.CreateModelInput{
		Provider: "ollama", ModelName: "bge-m3:latest", ModelType: model.ModelTypeEmbedding,
	})
	if err != nil {
		t.Fatalf("创建模型失败: %v", err)
	}

	cases := []struct {
		name string
		in   service.BindModelInput
	}{
		{"作用域非法", service.BindModelInput{Scope: "team", ModelType: model.ModelTypeEmbedding, ModelInstanceID: instance.ID}},
		{"应用级缺 appId", service.BindModelInput{Scope: model.ModelScopeApp, ModelType: model.ModelTypeEmbedding, ModelInstanceID: instance.ID}},
		{
			"应用不存在",
			service.BindModelInput{Scope: model.ModelScopeApp, AppID: 999, ModelType: model.ModelTypeEmbedding, ModelInstanceID: instance.ID},
		},
		{
			"知识库不存在",
			service.BindModelInput{Scope: model.ModelScopeDataset, DatasetID: 999, ModelType: model.ModelTypeEmbedding, ModelInstanceID: instance.ID},
		},
		{
			"类型不匹配",
			service.BindModelInput{Scope: model.ModelScopeGlobal, ModelType: model.ModelTypeLLM, ModelInstanceID: instance.ID},
		},
		{
			"模型不存在",
			service.BindModelInput{Scope: model.ModelScopeGlobal, ModelType: model.ModelTypeEmbedding, ModelInstanceID: 4242},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := fixture.service.BindModel(ctx, tc.in); !errors.Is(err, service.ErrInvalidInput) {
				t.Fatalf("错误 = %v, 期望 ErrInvalidInput", err)
			}
		})
	}

	// 全局绑定：应写入 scope=global 且作用域 ID 归零。
	if err := fixture.service.BindModel(ctx, service.BindModelInput{
		Scope: model.ModelScopeGlobal, ModelType: model.ModelTypeEmbedding, ModelInstanceID: instance.ID,
	}); err != nil {
		t.Fatalf("绑定失败: %v", err)
	}
	binding, err := fixture.store.GetBinding(ctx, model.ModelScopeGlobal, 0, 0, model.ModelTypeEmbedding)
	if err != nil {
		t.Fatalf("查询绑定失败: %v", err)
	}
	if binding.ModelInstanceID != instance.ID || binding.AppID != 0 || binding.DatasetID != 0 {
		t.Fatalf("绑定内容不符: %+v", binding)
	}

	// 知识库覆盖：同一模型可以同时绑在多个作用域。
	if err := fixture.service.BindModel(ctx, service.BindModelInput{
		Scope: model.ModelScopeDataset, DatasetID: 2,
		ModelType: model.ModelTypeEmbedding, ModelInstanceID: instance.ID,
	}); err != nil {
		t.Fatalf("知识库绑定失败: %v", err)
	}
	views, err := fixture.service.ListBindings(ctx, model.ModelScopeDataset, 0, 2)
	if err != nil {
		t.Fatalf("查询绑定失败: %v", err)
	}
	if len(views) != 1 || views[0].ModelName != "bge-m3:latest" {
		t.Fatalf("绑定列表不符: %+v", views)
	}

	// 解绑后应查不到。
	if err := fixture.service.UnbindModel(ctx, model.ModelScopeDataset, 0, 2, model.ModelTypeEmbedding); err != nil {
		t.Fatalf("解绑失败: %v", err)
	}
	if _, err := fixture.store.GetBinding(ctx, model.ModelScopeDataset, 0, 2, model.ModelTypeEmbedding); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("解绑后仍能查到绑定: %v", err)
	}
}

// TestEffectiveEmbedding 覆盖「当前生效模型」查询与错误透传。
func TestEffectiveEmbedding(t *testing.T) {
	fixture := newModelConfigFixture(t)
	fixture.resolver.effective = resolver.EffectiveModel{
		InstanceID: 7, Provider: "ollama", ProviderName: "Ollama",
		ModelName: "bge-m3:latest", ModelType: model.ModelTypeEmbedding,
		Dimension: 1024, Scope: model.ModelScopeGlobal,
	}

	effective, err := fixture.service.EffectiveModel(context.Background(), model.ModelTypeEmbedding, 0, 2)
	if err != nil {
		t.Fatalf("查询生效模型失败: %v", err)
	}
	if effective.ModelName != "bge-m3:latest" || effective.Dimension != 1024 {
		t.Fatalf("生效模型不符: %+v", effective)
	}

	// 未配置任何模型时：应返回「未配置」类错误而不是空结果，
	// 否则前端会显示成「已就绪但模型是空的」。
	fixture.resolver.err = enginemodel.ErrNotConfigured
	if _, err := fixture.service.EffectiveModel(
		context.Background(), model.ModelTypeEmbedding, 0, 0); err == nil {
		t.Fatal("未配置模型时应报错")
	}

	// 类型非法要在服务层挡掉，而不是把空类型透传给解析器。
	if _, err := fixture.service.EffectiveModel(
		context.Background(), model.ModelType("speech"), 0, 0); !errors.Is(err, service.ErrInvalidInput) {
		t.Fatalf("非法类型错误 = %v, 期望 ErrInvalidInput", err)
	}
}
