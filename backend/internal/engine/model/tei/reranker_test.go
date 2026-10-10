package tei_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"einoflow-backend/internal/engine/model/tei"
)

// fakeTEI 起一个假的 /rerank 服务。
// scores 是「输入下标 → 分数」的映射，返回时**刻意按分数降序**，
// 以复现真实 TEI 的行为（返回顺序不等于输入顺序）。
func fakeTEI(t *testing.T, scores map[int]float32, status int) (*httptest.Server, *int64) {
	t.Helper()
	var calls int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rerank" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		atomic.AddInt64(&calls, 1)
		if status != 0 {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"detail":"model not loaded"}`))
			return
		}

		var req struct {
			Query string   `json:"query"`
			Texts []string `json:"texts"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		type item struct {
			Index int     `json:"index"`
			Score float32 `json:"score"`
		}
		items := make([]item, 0, len(req.Texts))
		for index := range req.Texts {
			score, ok := scores[index]
			if !ok {
				continue // 模拟「服务漏返回某些候选」
			}
			items = append(items, item{Index: index, Score: score})
		}
		// 简单插入排序实现降序，模拟真实服务。
		for i := 1; i < len(items); i++ {
			for j := i; j > 0 && items[j].Score > items[j-1].Score; j-- {
				items[j], items[j-1] = items[j-1], items[j]
			}
		}
		_ = json.NewEncoder(w).Encode(items)
	}))
	t.Cleanup(server.Close)
	return server, &calls
}

// TestReranker_Rerank 覆盖核心语义：**分数必须按 index 归位**。
//
// 这是重排里最容易写错的点：服务端按分数降序返回，若直接按返回顺序当作输入顺序，
// 分数就会配到错误的片段上——检索仍会给出「看起来合理」的排序，错误极难发现。
func TestReranker_Rerank(t *testing.T) {
	// 第 0 条分数最低、第 2 条最高，服务端会先返回 index=2。
	server, calls := fakeTEI(t, map[int]float32{0: 0.1, 1: 0.5, 2: 0.9}, 0)
	reranker := tei.New(tei.Options{BaseURL: server.URL, Model: "BAAI/bge-reranker-v2-m3"})

	if reranker.Name() != "BAAI/bge-reranker-v2-m3" {
		t.Errorf("Name = %q", reranker.Name())
	}

	scores, err := reranker.Rerank(context.Background(), "如何测量电压", []string{"段A", "段B", "段C"})
	if err != nil {
		t.Fatalf("重排失败: %v", err)
	}
	if len(scores) != 3 {
		t.Fatalf("分数数量 = %d, 期望 3", len(scores))
	}
	expected := []float32{0.1, 0.5, 0.9}
	for i := range expected {
		if scores[i] != expected[i] {
			t.Errorf("第 %d 条分数 = %v, 期望 %v（下标未归位）", i, scores[i], expected[i])
		}
	}
	if *calls != 1 {
		t.Errorf("请求次数 = %d, 期望 1", *calls)
	}
}

// TestReranker_Batches 覆盖分批：候选数超过单批上限时拆成多次请求，
// 且各批分数要拼回正确位置。
func TestReranker_Batches(t *testing.T) {
	// 每批最多 2 条，批内下标 0 / 1 分别给 0.2 / 0.8。
	server, calls := fakeTEI(t, map[int]float32{0: 0.2, 1: 0.8}, 0)
	reranker := tei.New(tei.Options{BaseURL: server.URL, Model: "m", BatchSize: 2})

	scores, err := reranker.Rerank(context.Background(), "q", []string{"1", "2", "3", "4", "5"})
	if err != nil {
		t.Fatalf("重排失败: %v", err)
	}
	if len(scores) != 5 {
		t.Fatalf("分数数量 = %d, 期望 5", len(scores))
	}
	if *calls != 3 {
		t.Errorf("请求次数 = %d, 期望 3（每批 2 条 → 2+2+1）", *calls)
	}
	expected := []float32{0.2, 0.8, 0.2, 0.8, 0.2}
	for i := range expected {
		if scores[i] != expected[i] {
			t.Errorf("第 %d 条分数 = %v, 期望 %v", i, scores[i], expected[i])
		}
	}
}

// TestReranker_IncompleteResult 覆盖「服务漏返回部分候选」：
// 必须报错而不是把缺失项当 0 分——0 是合法分数，留 0 会让漏数据伪装成「不相关」。
func TestReranker_IncompleteResult(t *testing.T) {
	server, _ := fakeTEI(t, map[int]float32{0: 0.7}, 0) // 只返回第 0 条
	reranker := tei.New(tei.Options{BaseURL: server.URL, Model: "m"})

	_, err := reranker.Rerank(context.Background(), "q", []string{"a", "b"})
	if err == nil || !strings.Contains(err.Error(), "只返回了") {
		t.Fatalf("错误 = %v, 期望提示返回条数不足", err)
	}
	if !strings.Contains(err.Error(), "缺少下标 1") {
		t.Errorf("错误信息未指出缺哪个下标: %v", err)
	}
}

// TestReranker_Errors 覆盖服务报错与非法入参。
func TestReranker_Errors(t *testing.T) {
	t.Run("HTTP 错误", func(t *testing.T) {
		server, _ := fakeTEI(t, nil, http.StatusServiceUnavailable)
		reranker := tei.New(tei.Options{BaseURL: server.URL, Model: "m"})
		_, err := reranker.Rerank(context.Background(), "q", []string{"a"})
		if err == nil || !strings.Contains(err.Error(), "503") {
			t.Fatalf("错误 = %v, 期望包含状态码", err)
		}
	})

	t.Run("空查询", func(t *testing.T) {
		reranker := tei.New(tei.Options{BaseURL: "http://127.0.0.1:1", Model: "m"})
		if _, err := reranker.Rerank(context.Background(), "   ", []string{"a"}); err == nil {
			t.Fatal("空查询应报错")
		}
	})

	t.Run("空候选", func(t *testing.T) {
		reranker := tei.New(tei.Options{BaseURL: "http://127.0.0.1:1", Model: "m"})
		scores, err := reranker.Rerank(context.Background(), "q", nil)
		if err != nil || scores != nil {
			t.Fatalf("空候选应直接返回，实际 scores=%v err=%v", scores, err)
		}
	})

	t.Run("服务不可用", func(t *testing.T) {
		server, _ := fakeTEI(t, nil, 0)
		url := server.URL
		server.Close()
		reranker := tei.New(tei.Options{BaseURL: url, Model: "m"})
		_, err := reranker.Rerank(context.Background(), "q", []string{"a"})
		if err == nil || !strings.Contains(err.Error(), "调用重排服务失败") {
			t.Fatalf("错误 = %v, 期望提示调用失败", err)
		}
	})
}
