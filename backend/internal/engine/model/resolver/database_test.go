package resolver_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	enginemodel "einoflow-backend/internal/engine/model"
	"einoflow-backend/internal/engine/model/resolver"
	"einoflow-backend/internal/model"
	"einoflow-backend/internal/repository"
	"einoflow-backend/internal/security"
)

const testDim = 4

// fakeOllama 是一个只支持 /api/embed 与 /api/tags 的假 Ollama。
// 用它来跑完整的「数据库 → 厂商 → 构造向量化实现」链路，避免依赖真实服务。
func fakeOllama(t *testing.T, dim int) (*httptest.Server, *int64) {
	t.Helper()
	var embedCalls int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_, _ = w.Write([]byte(`{"models":[{"name":"model-global"},{"name":"model-app"},{"name":"model-dataset"}]}`))
		case "/api/embed":
			atomic.AddInt64(&embedCalls, 1)
			var req struct {
				Input []string `json:"input"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			embeddings := make([][]float32, len(req.Input))
			for i := range req.Input {
				vector := make([]float32, dim)
				vector[0] = 1
				embeddings[i] = vector
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": embeddings})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server, &embedCalls
}

// store 是三个仓储接口共用的内存实现。
type store struct {
	providers map[uint64]model.ModelProvider
	instances map[uint64]model.ModelInstance
	bindings  map[string]model.ModelBinding
	nextID    uint64
}

func newStore() *store {
	return &store{
		providers: map[uint64]model.ModelProvider{},
		instances: map[uint64]model.ModelInstance{},
		bindings:  map[string]model.ModelBinding{},
	}
}

func (s *store) addProvider(providerID string, baseURL string, enabled bool) uint64 {
	s.nextID++
	s.providers[s.nextID] = model.ModelProvider{
		ID: s.nextID, Provider: providerID, BaseURL: baseURL, Enabled: enabled,
	}
	return s.nextID
}

func (s *store) addInstance(providerID uint64, name string, kind model.ModelType, enabled bool) uint64 {
	s.nextID++
	s.instances[s.nextID] = model.ModelInstance{
		ID: s.nextID, ProviderID: providerID, ModelName: name, ModelType: kind, Enabled: enabled,
	}
	return s.nextID
}

func (s *store) bind(scope model.ModelScope, appID, datasetID uint64, kind model.ModelType, instanceID uint64) {
	key := bindingKey(scope, appID, datasetID, kind)
	s.bindings[key] = model.ModelBinding{
		Scope: scope, AppID: appID, DatasetID: datasetID, ModelType: kind, ModelInstanceID: instanceID,
	}
}

// bindingKey 复刻数据库里的唯一索引（scope, app_id, dataset_id, model_type）。
func bindingKey(scope model.ModelScope, appID, datasetID uint64, kind model.ModelType) string {
	return fmt.Sprintf("%s|%d|%d|%s", scope, appID, datasetID, kind)
}

// 仓储适配（只实现解析器用到的方法；其余方法 panic，避免误用）。
type providerRepo struct{ *store }

func (r providerRepo) Upsert(context.Context, *model.ModelProvider) error { panic("未预期调用") }
func (r providerRepo) GetByProvider(context.Context, string) (*model.ModelProvider, error) {
	panic("未预期调用")
}

func (r providerRepo) GetByID(_ context.Context, id uint64) (*model.ModelProvider, error) {
	record, ok := r.providers[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return &record, nil
}

func (r providerRepo) List(context.Context) ([]*model.ModelProvider, error) { panic("未预期调用") }
func (r providerRepo) Delete(context.Context, string) error                 { panic("未预期调用") }

type instanceRepo struct{ *store }

func (r instanceRepo) Create(context.Context, *model.ModelInstance) error { panic("未预期调用") }

func (r instanceRepo) GetByID(_ context.Context, id uint64) (*model.ModelInstance, error) {
	record, ok := r.instances[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return &record, nil
}

func (r instanceRepo) List(context.Context, repository.ModelInstanceFilter) ([]*model.ModelInstance, error) {
	panic("未预期调用")
}

func (r instanceRepo) Update(context.Context, *model.ModelInstance) error { panic("未预期调用") }
func (r instanceRepo) Delete(context.Context, uint64) error               { panic("未预期调用") }
func (r instanceRepo) CountByProvider(context.Context, uint64) (int64, error) {
	panic("未预期调用")
}

type bindingRepo struct{ *store }

func (r bindingRepo) Upsert(context.Context, *model.ModelBinding) error { panic("未预期调用") }

func (r bindingRepo) Get(
	_ context.Context, scope model.ModelScope, appID, datasetID uint64, kind model.ModelType,
) (*model.ModelBinding, error) {
	record, ok := r.bindings[bindingKey(scope, appID, datasetID, kind)]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return &record, nil
}

func (r bindingRepo) ListByScope(
	context.Context, model.ModelScope, uint64, uint64,
) ([]*model.ModelBinding, error) {
	panic("未预期调用")
}

func (r bindingRepo) Delete(context.Context, model.ModelScope, uint64, uint64, model.ModelType) error {
	panic("未预期调用")
}

func (r bindingRepo) CountByInstance(context.Context, uint64) (int64, error) {
	panic("未预期调用")
}

// fallbackResolver 记录兜底解析被调用的次数。
type fallbackResolver struct {
	name  string
	calls int
}

func (r *fallbackResolver) Embedder(
	context.Context, enginemodel.ScopeRef,
) (enginemodel.Embedder, error) {
	r.calls++
	return fakeEmbedder{name: r.name}, nil
}

type fakeEmbedder struct{ name string }

func (f fakeEmbedder) Name() string   { return f.name }
func (f fakeEmbedder) Dimension() int { return 8 }
func (f fakeEmbedder) Embed(context.Context, []string) ([][]float32, error) {
	return [][]float32{{1, 2, 3, 4, 5, 6, 7, 8}}, nil
}

// newResolver 组装一个全部依赖打桩、但厂商实现走真实代码的解析器。
func newResolver(t *testing.T, data *store, fallback enginemodel.Resolver) *resolver.DatabaseResolver {
	t.Helper()
	cipher, err := security.NewCipher([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("构造加密器失败: %v", err)
	}
	instance, err := resolver.NewDatabase(resolver.DatabaseOptions{
		Cipher:    cipher,
		Providers: providerRepo{data},
		Instances: instanceRepo{data},
		Bindings:  bindingRepo{data},
		Fallback:  fallback,
	})
	if err != nil {
		t.Fatalf("构造解析器失败: %v", err)
	}
	return instance
}

// TestDatabaseResolver_ScopePriority 覆盖三级覆盖的优先级。
func TestDatabaseResolver_ScopePriority(t *testing.T) {
	server, _ := fakeOllama(t, testDim)
	data := newStore()
	providerID := data.addProvider("ollama", server.URL, true)
	globalID := data.addInstance(providerID, "model-global", model.ModelTypeEmbedding, true)
	appID := data.addInstance(providerID, "model-app", model.ModelTypeEmbedding, true)
	datasetID := data.addInstance(providerID, "model-dataset", model.ModelTypeEmbedding, true)
	data.bind(model.ModelScopeGlobal, 0, 0, model.ModelTypeEmbedding, globalID)
	data.bind(model.ModelScopeApp, 9, 0, model.ModelTypeEmbedding, appID)
	data.bind(model.ModelScopeDataset, 0, 2, model.ModelTypeEmbedding, datasetID)

	instance := newResolver(t, data, nil)
	ctx := context.Background()
	cases := []struct {
		name  string
		scope enginemodel.ScopeRef
		want  string
		from  model.ModelScope
	}{
		{"只有全局", enginemodel.ScopeRef{}, "model-global", model.ModelScopeGlobal},
		{"应用覆盖", enginemodel.ScopeRef{AppID: 9}, "model-app", model.ModelScopeApp},
		{"知识库覆盖", enginemodel.ScopeRef{DatasetID: 2}, "model-dataset", model.ModelScopeDataset},
		{"知识库优先于应用", enginemodel.ScopeRef{AppID: 9, DatasetID: 2}, "model-dataset", model.ModelScopeDataset},
		{"无覆盖时回落全局", enginemodel.ScopeRef{AppID: 77, DatasetID: 88}, "model-global", model.ModelScopeGlobal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			effective, err := instance.EmbedderFor(ctx, tc.scope)
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			if effective.ModelName != tc.want {
				t.Errorf("模型 = %s, 期望 %s", effective.ModelName, tc.want)
			}
			if effective.Scope != tc.from {
				t.Errorf("来源 = %s, 期望 %s", effective.Scope, tc.from)
			}
			// 维度来自真实探测，而不是配置里写死的值。
			if effective.Dimension != testDim {
				t.Errorf("维度 = %d, 期望 %d", effective.Dimension, testDim)
			}
		})
	}
}

// TestDatabaseResolver_Fallback 覆盖「完全没有绑定」时才用配置兜底。
func TestDatabaseResolver_Fallback(t *testing.T) {
	_, embedCalls := fakeOllama(t, testDim)
	data := newStore()
	fallback := &fallbackResolver{name: "config-model"}
	instance := newResolver(t, data, fallback)

	effective, err := instance.EmbedderFor(context.Background(), enginemodel.ScopeRef{DatasetID: 2})
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if effective.ModelName != "config-model" || effective.InstanceID != 0 {
		t.Fatalf("未走配置兜底: %+v", effective)
	}
	if fallback.calls != 1 {
		t.Errorf("兜底调用次数 = %d, 期望 1", fallback.calls)
	}
	// 兜底路径不应触发任何厂商探测。
	if *embedCalls != 0 {
		t.Errorf("兜底路径不应调用模型服务，实际调用 %d 次", *embedCalls)
	}
}

// TestDatabaseResolver_NoSilentFallback 覆盖「绑定了但配置有问题」必须报错：
// 静默回落到配置文件会让使用者以为配置生效了，实际跑的是另一套。
func TestDatabaseResolver_NoSilentFallback(t *testing.T) {
	server, _ := fakeOllama(t, testDim)
	ctx := context.Background()

	t.Run("模型被停用", func(t *testing.T) {
		data := newStore()
		providerID := data.addProvider("ollama", server.URL, true)
		instanceID := data.addInstance(providerID, "model-global", model.ModelTypeEmbedding, false)
		data.bind(model.ModelScopeGlobal, 0, 0, model.ModelTypeEmbedding, instanceID)

		fallback := &fallbackResolver{name: "config-model"}
		_, err := newResolver(t, data, fallback).EmbedderFor(ctx, enginemodel.ScopeRef{})
		if err == nil || !strings.Contains(err.Error(), "已停用") {
			t.Fatalf("错误 = %v, 期望提示模型已停用", err)
		}
		if fallback.calls != 0 {
			t.Error("绑定的模型有问题时不应回落到配置")
		}
	})

	t.Run("类型不匹配", func(t *testing.T) {
		data := newStore()
		providerID := data.addProvider("ollama", server.URL, true)
		instanceID := data.addInstance(providerID, "chat-model", model.ModelTypeLLM, true)
		data.bind(model.ModelScopeGlobal, 0, 0, model.ModelTypeEmbedding, instanceID)

		_, err := newResolver(t, data, nil).EmbedderFor(ctx, enginemodel.ScopeRef{})
		if err == nil || !strings.Contains(err.Error(), "不能用于") {
			t.Fatalf("错误 = %v, 期望提示类型不匹配", err)
		}
	})

	t.Run("厂商被停用", func(t *testing.T) {
		data := newStore()
		providerID := data.addProvider("ollama", server.URL, false)
		instanceID := data.addInstance(providerID, "model-global", model.ModelTypeEmbedding, true)
		data.bind(model.ModelScopeGlobal, 0, 0, model.ModelTypeEmbedding, instanceID)

		_, err := newResolver(t, data, nil).EmbedderFor(ctx, enginemodel.ScopeRef{})
		if err == nil || !strings.Contains(err.Error(), "已停用") {
			t.Fatalf("错误 = %v, 期望提示厂商已停用", err)
		}
	})

	t.Run("模型不可用", func(t *testing.T) {
		// 厂商地址指向一个已关闭的服务：构造实现时的维度探测会失败。
		dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		dead.Close()
		data := newStore()
		providerID := data.addProvider("ollama", dead.URL, true)
		instanceID := data.addInstance(providerID, "model-global", model.ModelTypeEmbedding, true)
		data.bind(model.ModelScopeGlobal, 0, 0, model.ModelTypeEmbedding, instanceID)

		_, err := newResolver(t, data, nil).EmbedderFor(ctx, enginemodel.ScopeRef{})
		if err == nil {
			t.Fatal("模型不可用时应报错")
		}
	})
}

// TestDatabaseResolver_Cache 覆盖缓存与显式失效：
// 缓存是为了避免每次检索都探测一次维度，Invalidate 是为了配置改动立刻生效。
func TestDatabaseResolver_Cache(t *testing.T) {
	server, embedCalls := fakeOllama(t, testDim)
	data := newStore()
	providerID := data.addProvider("ollama", server.URL, true)
	instanceID := data.addInstance(providerID, "model-global", model.ModelTypeEmbedding, true)
	data.bind(model.ModelScopeGlobal, 0, 0, model.ModelTypeEmbedding, instanceID)

	instance := newResolver(t, data, nil)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := instance.EmbedderFor(ctx, enginemodel.ScopeRef{}); err != nil {
			t.Fatalf("第 %d 次解析失败: %v", i, err)
		}
	}
	if *embedCalls != 1 {
		t.Errorf("探测次数 = %d, 期望 1（命中缓存）", *embedCalls)
	}

	instance.Invalidate()
	if _, err := instance.EmbedderFor(ctx, enginemodel.ScopeRef{}); err != nil {
		t.Fatalf("失效后解析失败: %v", err)
	}
	if *embedCalls != 2 {
		t.Errorf("失效后探测次数 = %d, 期望 2", *embedCalls)
	}
}

// TestDatabaseResolver_CredentialErrors 覆盖凭证解密失败必须报错而不是当作空凭证。
func TestDatabaseResolver_CredentialErrors(t *testing.T) {
	data := newStore()
	providerID := data.addProvider("ollama", "http://127.0.0.1:1", true)
	provider := data.providers[providerID]
	// 用另一把密钥加密，模拟「换过密钥」或数据来自别的环境。
	other, err := security.NewCipher([]byte("ffffffffffffffffffffffffffffffff"))
	if err != nil {
		t.Fatalf("构造加密器失败: %v", err)
	}
	provider.Credentials, err = other.Encrypt(`{"api_key":"sk-x"}`)
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	data.providers[providerID] = provider

	instanceID := data.addInstance(providerID, "model-global", model.ModelTypeEmbedding, true)
	data.bind(model.ModelScopeGlobal, 0, 0, model.ModelTypeEmbedding, instanceID)

	_, err = newResolver(t, data, nil).EmbedderFor(context.Background(), enginemodel.ScopeRef{})
	if err == nil || !strings.Contains(err.Error(), "无法解密") {
		t.Fatalf("错误 = %v, 期望提示凭证无法解密", err)
	}
	if errors.Is(err, enginemodel.ErrNotConfigured) {
		t.Error("解密失败不应被当成「未配置」")
	}
}
