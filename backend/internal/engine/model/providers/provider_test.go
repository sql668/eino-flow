package providers_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	enginemodel "einoflow-backend/internal/engine/model"
	"einoflow-backend/internal/engine/model/providers"
	"einoflow-backend/internal/model"
)

// TestRegistry 覆盖注册表的稳定性与错误提示。
func TestRegistry(t *testing.T) {
	all := providers.All()
	if len(all) == 0 {
		t.Fatal("内置厂商注册表为空")
	}
	// 顺序必须稳定，否则接口输出会随 map 遍历顺序抖动。
	for i := 1; i < len(all); i++ {
		if all[i-1].ID() >= all[i].ID() {
			t.Fatalf("厂商顺序不稳定：%s 在 %s 之前", all[i-1].ID(), all[i].ID())
		}
	}

	plugin, err := providers.Get("Ollama")
	if err != nil {
		t.Fatalf("大小写不敏感查找失败: %v", err)
	}
	if plugin.ID() != "ollama" {
		t.Fatalf("厂商 ID = %s, 期望 ollama", plugin.ID())
	}

	// 找不到时必须把可选项列出来，避免使用者对着「不支持」猜。
	_, err = providers.Get("no-such-provider")
	if err == nil || !strings.Contains(err.Error(), "ollama") {
		t.Fatalf("未知厂商错误 = %v, 期望包含可选值列表", err)
	}
}

// TestSupports 覆盖能力声明：Ollama 不做 rerank、TEI 不做 embedding（D5 的落地约束）。
func TestSupports(t *testing.T) {
	ollama, _ := providers.Get("ollama")
	if !providers.Supports(ollama, model.ModelTypeEmbedding) {
		t.Error("Ollama 应支持 embedding")
	}
	if providers.Supports(ollama, model.ModelTypeRerank) {
		t.Error("Ollama 不应声明支持 rerank（实测其重排模型调用即崩溃）")
	}

	tei, _ := providers.Get("tei")
	if !providers.Supports(tei, model.ModelTypeRerank) {
		t.Error("TEI 应支持 rerank")
	}
	if providers.Supports(tei, model.ModelTypeEmbedding) {
		t.Error("本项目里 TEI 专职重排，不应声明支持 embedding")
	}
}

// TestOllamaProbe 覆盖三种结果：模型已下载、模型未下载、服务不可用。
func TestOllamaProbe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// 校验可选令牌确实被带上了：经反向代理暴露的 Ollama 需要它。
		if token := r.Header.Get("Authorization"); token != "" && token != "Bearer secret-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"models":[{"name":"bge-m3:latest"},{"name":"qwen3:8b"}]}`))
	}))
	defer server.Close()

	plugin, err := providers.Get("ollama")
	if err != nil {
		t.Fatalf("取厂商失败: %v", err)
	}
	ctx := context.Background()

	t.Run("模型已下载", func(t *testing.T) {
		result, err := plugin.Probe(ctx, providers.Config{
			BaseURL: server.URL, ModelName: "bge-m3:latest",
			Credentials: map[string]string{"api_key": "secret-token"},
		})
		if err != nil {
			t.Fatalf("探测失败: %v", err)
		}
		if !result.OK || !result.ModelAvailable {
			t.Fatalf("结果不符: %+v", result)
		}
		if !strings.Contains(result.Message, "bge-m3:latest") {
			t.Errorf("提示未包含模型名: %s", result.Message)
		}
		if len(result.Models) != 2 {
			t.Errorf("模型列表 = %v, 期望 2 个", result.Models)
		}
	})

	t.Run("模型未下载", func(t *testing.T) {
		result, err := plugin.Probe(ctx, providers.Config{BaseURL: server.URL, ModelName: "not-pulled:latest"})
		if err != nil {
			t.Fatalf("探测失败: %v", err)
		}
		// 连接是通的，所以 OK 仍为 true，但必须明确提示模型不在列表里，
		// 否则使用者会把「模型没拉取」误判成「地址填错」。
		if !result.OK || result.ModelAvailable {
			t.Fatalf("结果不符: %+v", result)
		}
		if !strings.Contains(result.Message, "未找到模型") {
			t.Errorf("提示未说明模型缺失: %s", result.Message)
		}
	})

	t.Run("服务不可用", func(t *testing.T) {
		dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		dead.Close()
		if _, err := plugin.Probe(ctx, providers.Config{BaseURL: dead.URL}); err == nil {
			t.Fatal("服务不可用时应报错")
		}
	})
}

// TestOpenAICompatibleProbe 覆盖 OpenAI 兼容端点的探测与错误映射。
func TestOpenAICompatibleProbe(t *testing.T) {
	ctx := context.Background()
	plugin, err := providers.Get("openai_compatible")
	if err != nil {
		t.Fatalf("取厂商失败: %v", err)
	}

	t.Run("凭证有效", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/models" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if r.Header.Get("Authorization") != "Bearer sk-test" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"text-embedding-v3"},{"id":"qwen-plus"}]}`))
		}))
		defer server.Close()

		result, err := plugin.Probe(ctx, providers.Config{
			BaseURL: server.URL + "/v1", ModelName: "text-embedding-v3",
			Credentials: map[string]string{"api_key": "sk-test"},
		})
		if err != nil {
			t.Fatalf("探测失败: %v", err)
		}
		if !result.ModelAvailable || len(result.Models) != 2 {
			t.Fatalf("结果不符: %+v", result)
		}
	})

	t.Run("凭证无效", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
		}))
		defer server.Close()

		_, err := plugin.Probe(ctx, providers.Config{
			BaseURL: server.URL, Credentials: map[string]string{"api_key": "bad"},
		})
		// 401 必须翻译成「凭证被拒绝」：只说 HTTP 401，使用者会去查网络。
		if err == nil || !strings.Contains(err.Error(), "凭证被拒绝") {
			t.Fatalf("错误 = %v, 期望提示凭证被拒绝", err)
		}
	})

	t.Run("端点不提供模型列表", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			// 有些兼容端点 /models 返回 HTML 或空体。
			_, _ = w.Write([]byte(`<html>ok</html>`))
		}))
		defer server.Close()

		result, err := plugin.Probe(ctx, providers.Config{BaseURL: server.URL})
		if err != nil {
			t.Fatalf("不应把「不提供模型列表」当成失败: %v", err)
		}
		if !result.OK || !strings.Contains(result.Message, "未提供模型列表") {
			t.Fatalf("结果不符: %+v", result)
		}
	})
}

// TestTEIProbe 覆盖边车探测：必须把「实际加载的模型」报出来。
func TestTEIProbe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/info" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"model_id":"/data/models/BAAI/bge-reranker-v2-m3","model_dtype":"float32"}`))
	}))
	defer server.Close()

	plugin, err := providers.Get("tei")
	if err != nil {
		t.Fatalf("取厂商失败: %v", err)
	}
	ctx := context.Background()

	t.Run("模型一致", func(t *testing.T) {
		result, err := plugin.Probe(ctx, providers.Config{
			BaseURL: server.URL, ModelName: "BAAI/bge-reranker-v2-m3",
		})
		if err != nil {
			t.Fatalf("探测失败: %v", err)
		}
		if !result.ModelAvailable || !strings.Contains(result.Message, "一致") {
			t.Fatalf("结果不符: %+v", result)
		}
	})

	t.Run("本地路径形式的模型名视为一致", func(t *testing.T) {
		// 本项目用本地路径启动边车（绕开 TEI 自带下载器对 ETag 的要求），
		// /info 会回报路径；这属于同一个模型，不能报「不一致」吓人。
		result, err := plugin.Probe(ctx, providers.Config{
			BaseURL: server.URL, ModelName: "BAAI/bge-reranker-v2-m3",
		})
		if err != nil {
			t.Fatalf("探测失败: %v", err)
		}
		if !result.ModelAvailable {
			t.Errorf("同名的本地路径应判定为一致: %+v", result)
		}
	})

	t.Run("模型不一致", func(t *testing.T) {
		// 这是「界面显示 A、实际跑 B」的现场，必须让使用者看见真实加载的模型名。
		result, err := plugin.Probe(ctx, providers.Config{BaseURL: server.URL, ModelName: "其它模型"})
		if err != nil {
			t.Fatalf("探测失败: %v", err)
		}
		if result.ModelAvailable {
			t.Error("模型不一致时不应判定为可用")
		}
		if !strings.Contains(result.Message, "/data/models/BAAI/bge-reranker-v2-m3") {
			t.Errorf("提示未报出实际加载的模型: %s", result.Message)
		}
	})
}

// TestTEINewEmbedderRejected 覆盖「TEI 不做向量化」这条约束被显式拒绝。
func TestTEINewEmbedderRejected(t *testing.T) {
	plugin, _ := providers.Get("tei")
	if _, err := plugin.NewEmbedder(providers.Config{ModelName: "x"}); err == nil {
		t.Fatal("TEI 构造向量化实现应报错")
	}
}

// TestRerankerFactories 覆盖重排实现的构造与拒绝：
// 只有 TEI 能造出重排器，另两家必须给出可执行的替代方案。
func TestRerankerFactories(t *testing.T) {
	t.Run("TEI 可用", func(t *testing.T) {
		plugin, _ := providers.Get("tei")
		reranker, err := plugin.NewReranker(providers.Config{
			BaseURL: "http://127.0.0.1:8080", ModelName: "BAAI/bge-reranker-v2-m3",
		})
		if err != nil {
			t.Fatalf("构造失败: %v", err)
		}
		if reranker.Name() != "BAAI/bge-reranker-v2-m3" {
			t.Errorf("Name = %q", reranker.Name())
		}
		// 没填模型名必须拒绝：没有目标模型就无从调用。
		if _, err := plugin.NewReranker(providers.Config{}); err == nil {
			t.Error("未指定模型名应报错")
		}
	})

	t.Run("Ollama 拒绝并给出指引", func(t *testing.T) {
		plugin, _ := providers.Get("ollama")
		_, err := plugin.NewReranker(providers.Config{ModelName: "dengcao/bge-reranker-v2-m3:latest"})
		if !errors.Is(err, enginemodel.ErrRerankUnsupported) {
			t.Fatalf("错误 = %v, 期望 ErrRerankUnsupported", err)
		}
		// 只说「不支持」会让人反复试模型名；必须给出该做什么。
		if !strings.Contains(err.Error(), "TEI") {
			t.Errorf("错误信息未给出替代方案: %v", err)
		}
	})

	t.Run("OpenAI 兼容接口拒绝", func(t *testing.T) {
		plugin, _ := providers.Get("openai_compatible")
		if _, err := plugin.NewReranker(
			providers.Config{ModelName: "x"}); !errors.Is(err, enginemodel.ErrRerankUnsupported) {
			t.Fatalf("错误 = %v, 期望 ErrRerankUnsupported", err)
		}
	})
}

// TestDeepSeek 覆盖 DeepSeek 这类「只有对话能力」的厂商：
// 能力声明必须准确，否则它会被当成向量化模型使用，直到上传文档才暴错。
func TestDeepSeek(t *testing.T) {
	plugin, err := providers.Get("deepseek")
	if err != nil {
		t.Fatalf("取厂商失败: %v", err)
	}

	if plugin.DefaultBaseURL() != "https://api.deepseek.com/v1" {
		t.Errorf("默认地址 = %s", plugin.DefaultBaseURL())
	}
	kinds := plugin.Kinds()
	if len(kinds) != 1 || kinds[0] != model.ModelTypeLLM {
		t.Fatalf("能力 = %v, 期望只有 llm（DeepSeek 没有向量化与重排接口）", kinds)
	}
	if !providers.Supports(plugin, model.ModelTypeLLM) {
		t.Error("应支持对话")
	}

	// 两个工厂都要给出「不支持」而不是「未配置」：前者的修法是换厂商。
	if _, err := plugin.NewEmbedder(providers.Config{ModelName: "deepseek-chat"}); !errors.Is(err, enginemodel.ErrEmbeddingUnsupported) {
		t.Errorf("向量化错误 = %v, 期望 ErrEmbeddingUnsupported", err)
	}
	if _, err := plugin.NewReranker(providers.Config{ModelName: "deepseek-chat"}); !errors.Is(err, enginemodel.ErrRerankUnsupported) {
		t.Errorf("重排错误 = %v, 期望 ErrRerankUnsupported", err)
	}

	ctx := context.Background()
	t.Run("凭证有效", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/models" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if r.Header.Get("Authorization") != "Bearer sk-deepseek" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"deepseek-chat"},{"id":"deepseek-reasoner"}]}`))
		}))
		defer server.Close()

		result, err := plugin.Probe(ctx, providers.Config{
			BaseURL: server.URL + "/v1", ModelName: "deepseek-chat",
			Credentials: map[string]string{"api_key": "sk-deepseek"},
		})
		if err != nil {
			t.Fatalf("探测失败: %v", err)
		}
		if !result.ModelAvailable || len(result.Models) != 2 {
			t.Fatalf("结果不符: %+v", result)
		}
	})

	t.Run("密钥无效", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"Authentication Fails"}}`))
		}))
		defer server.Close()

		_, err := plugin.Probe(ctx, providers.Config{
			BaseURL: server.URL, Credentials: map[string]string{"api_key": "bad"},
		})
		if err == nil || !strings.Contains(err.Error(), "凭证被拒绝") {
			t.Fatalf("错误 = %v, 期望提示凭证被拒绝", err)
		}
	})
}

// TestRegistryContainsBuiltins 防止「加了厂商但忘了注册」这类低级问题。
func TestRegistryContainsBuiltins(t *testing.T) {
	for _, id := range []string{"ollama", "openai_compatible", "deepseek", "tei"} {
		if _, err := providers.Get(id); err != nil {
			t.Errorf("内置厂商 %s 未注册: %v", id, err)
		}
	}
}
