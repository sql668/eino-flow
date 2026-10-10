package ollama_test

import (
	"context"
	"math"
	"os"
	"testing"

	"einoflow-backend/internal/engine/model/ollama"
)

// 集成测试：需要真实的 Ollama 服务与模型，用环境变量开启。
//
//	OLLAMA_TEST_MODEL=bge-m3:latest go test ./internal/engine/model/ollama/ -run Integration -v
//
// 手写的假服务无法覆盖两类真实问题：模型实际输出的维度、以及向量是否真的具有语义区分度。
const (
	envBaseURL = "OLLAMA_TEST_BASE_URL"
	envModel   = "OLLAMA_TEST_MODEL"
)

func TestEmbedder_Integration(t *testing.T) {
	modelName := os.Getenv(envModel)
	if modelName == "" {
		t.Skipf("未设置 %s，跳过集成测试", envModel)
	}
	baseURL := os.Getenv(envBaseURL)

	embedder := ollama.New(ollama.Options{BaseURL: baseURL, Model: modelName, BatchSize: 8})
	ctx := context.Background()

	dimension, err := embedder.DetectDimension(ctx)
	if err != nil {
		t.Fatalf("探测维度失败: %v", err)
	}
	if dimension <= 0 {
		t.Fatalf("维度非法: %d", dimension)
	}
	t.Logf("模型 %s 维度 = %d", modelName, dimension)

	// 语义区分度：相关文本对的余弦相似度必须明显高于无关文本对。
	// 这条断言是有实际意义的——nomic-embed-text 在中文上会给出「无关文本相似度 0.86」的
	// 退化结果，区分度甚至是负的（已实测），单看「能返回向量」是发现不了的。
	texts := []string{
		"直流电压测量",   // 0 基准
		"如何测量直流电压", // 1 与 0 高度相关
		"更换电池的步骤",  // 2 与 0 无关
	}
	vectors, err := embedder.Embed(ctx, texts)
	if err != nil {
		t.Fatalf("批量向量化失败: %v", err)
	}
	if len(vectors) != len(texts) {
		t.Fatalf("返回向量数 = %d, 期望 %d", len(vectors), len(texts))
	}
	for i, vector := range vectors {
		if len(vector) != dimension {
			t.Fatalf("第 %d 条向量维度 = %d, 与探测值 %d 不一致", i, len(vector), dimension)
		}
	}

	related := cosine(vectors[0], vectors[1])
	unrelated := cosine(vectors[0], vectors[2])
	t.Logf("相关对相似度 = %.4f | 无关对相似度 = %.4f | 区分度 = %.4f",
		related, unrelated, related-unrelated)

	if related <= unrelated {
		t.Errorf("相关文本的相似度（%.4f）应显著高于无关文本（%.4f）；该模型不适合当前语料",
			related, unrelated)
	}
	if related-unrelated < 0.1 {
		t.Errorf("区分度过低（%.4f），检索效果会很差", related-unrelated)
	}
}

// cosine 计算余弦相似度。
func cosine(a, b []float32) float64 {
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
