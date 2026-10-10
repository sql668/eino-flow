package ollama_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"einoflow-backend/internal/engine/model"
	"einoflow-backend/internal/engine/model/ollama"
)

// fakeOllama 起一个假的 /api/embed 服务。
// dim 为返回的向量维度；若请求体里的 model 等于 wantModel 才正常返回。
func fakeOllama(t *testing.T, dim int, failStatus int) (*httptest.Server, *int64) {
	t.Helper()
	var calls int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&calls, 1)
		if r.URL.Path != "/api/embed" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if failStatus != 0 {
			w.WriteHeader(failStatus)
			_, _ = w.Write([]byte(`{"error":"model not found"}`))
			return
		}

		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		embeddings := make([][]float32, len(req.Input))
		for i := range req.Input {
			embeddings[i] = make([]float32, dim)
			embeddings[i][0] = float32(i)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"model": req.Model, "embeddings": embeddings})
	}))
	t.Cleanup(server.Close)
	return server, &calls
}

func TestEmbed_BatchesRequests(t *testing.T) {
	server, calls := fakeOllama(t, 8, 0)
	embedder := ollama.New(ollama.Options{BaseURL: server.URL, Model: "bge-m3", BatchSize: 2})

	vectors, err := embedder.Embed(context.Background(), []string{"a", "b", "c", "d", "e"})
	if err != nil {
		t.Fatalf("Embed 返回错误: %v", err)
	}
	if len(vectors) != 5 {
		t.Fatalf("返回向量数 = %d, 期望 5", len(vectors))
	}
	// 5 条文本、批大小 2，应当是 3 次请求（2 + 2 + 1），而不是 5 次单条请求
	if got := atomic.LoadInt64(calls); got != 3 {
		t.Errorf("请求次数 = %d, 期望 3（批量应合并请求）", got)
	}
	if embedder.Dimension() != 8 {
		t.Errorf("Dimension() = %d, 期望 8", embedder.Dimension())
	}
}

func TestEmbed_EmptyInput(t *testing.T) {
	server, calls := fakeOllama(t, 4, 0)
	embedder := ollama.New(ollama.Options{BaseURL: server.URL, Model: "m"})

	vectors, err := embedder.Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("空输入不应报错: %v", err)
	}
	if vectors != nil {
		t.Errorf("空输入应返回 nil, 实际 %v", vectors)
	}
	if atomic.LoadInt64(calls) != 0 {
		t.Error("空输入不应发起请求")
	}
}

func TestEmbed_DimensionChangeIsRejected(t *testing.T) {
	// 第一次返回 4 维，之后返回 8 维——模拟服务端模型被换掉。
	var calls int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dim := 4
		if atomic.AddInt64(&calls, 1) > 1 {
			dim = 8
		}
		embeddings := [][]float32{make([]float32, dim)}
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": embeddings})
	}))
	defer server.Close()

	embedder := ollama.New(ollama.Options{BaseURL: server.URL, Model: "m"})
	if _, err := embedder.Embed(context.Background(), []string{"first"}); err != nil {
		t.Fatalf("首次调用不应报错: %v", err)
	}

	// 维度静默变化必须显式失败：混入不同维度的向量会污染整个索引
	_, err := embedder.Embed(context.Background(), []string{"second"})
	if !errors.Is(err, model.ErrDimensionMismatch) {
		t.Fatalf("维度变化应返回 ErrDimensionMismatch, 实际: %v", err)
	}
}

func TestEmbed_ServerError(t *testing.T) {
	server, _ := fakeOllama(t, 4, http.StatusNotFound)
	embedder := ollama.New(ollama.Options{BaseURL: server.URL, Model: "missing-model"})

	_, err := embedder.Embed(context.Background(), []string{"x"})
	if err == nil {
		t.Fatal("服务端报错时应返回错误")
	}
	// 错误里要能看到原因，否则排查「模型没下载」这类问题只能靠猜
	if !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "model not found") {
		t.Errorf("错误信息缺少关键上下文: %v", err)
	}
}

func TestEmbed_UnreachableService(t *testing.T) {
	// 指向一个必然连不上的端口
	embedder := ollama.New(ollama.Options{BaseURL: "http://127.0.0.1:1", Model: "m"})
	if _, err := embedder.Embed(context.Background(), []string{"x"}); err == nil {
		t.Fatal("服务不可达时应返回错误")
	}
}

func TestEmbed_CountMismatchIsRejected(t *testing.T) {
	// 服务端返回的向量数少于输入文本数——按顺序对应关系会被打乱，
	// 必须报错而不是将错就错地拼接。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float32{{1, 2}}})
	}))
	defer server.Close()

	embedder := ollama.New(ollama.Options{BaseURL: server.URL, Model: "m"})
	_, err := embedder.Embed(context.Background(), []string{"a", "b", "c"})
	if err == nil {
		t.Fatal("向量数与文本数不一致时应报错")
	}
	if !strings.Contains(err.Error(), "不一致") {
		t.Errorf("错误信息应说明数量不一致: %v", err)
	}
}

func TestEmbed_DimensionUnknownBeforeFirstCall(t *testing.T) {
	server, _ := fakeOllama(t, 16, 0)
	embedder := ollama.New(ollama.Options{BaseURL: server.URL, Model: "m"})
	if embedder.Dimension() != 0 {
		t.Errorf("未调用前维度应为 0, 实际 %d", embedder.Dimension())
	}
	if _, err := embedder.DetectDimension(context.Background()); err != nil {
		t.Fatalf("DetectDimension 返回错误: %v", err)
	}
	if embedder.Dimension() != 16 {
		t.Errorf("探测后维度 = %d, 期望 16", embedder.Dimension())
	}
}
