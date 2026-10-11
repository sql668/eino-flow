package milvus_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"einoflow-backend/internal/engine/model"
	"einoflow-backend/internal/repository"
	milvusstore "einoflow-backend/internal/repository/milvus"
)

// 集成测试：需要真实的 Milvus 服务，用环境变量开启。
//
//	MILVUS_TEST_ADDRESS=127.0.0.1:19530 MILVUS_TEST_DATABASE=einoflow go test ./internal/repository/milvus/ -v
//
// 假服务覆盖不了这一类问题：真实 Milvus 的集合加载时机、写入可见性、
// 维度校验的实际报错形态，只有连上真实实例才能验证。
//
// 测试用的集合名带后缀，不会碰到业务集合；每次运行前先删掉，保证可重复执行。
const (
	envAddress  = "MILVUS_TEST_ADDRESS"
	envDatabase = "MILVUS_TEST_DATABASE"

	testCollection = "einoflow_segments_itest"
)

// testStore 连接 Milvus 并清掉测试集合。
func testStore(t *testing.T) *milvusstore.Store {
	t.Helper()
	address := os.Getenv(envAddress)
	if address == "" {
		t.Skipf("未设置 %s，跳过集成测试", envAddress)
	}

	store, err := milvusstore.New(context.Background(), milvusstore.Options{
		Address:    address,
		Database:   os.Getenv(envDatabase),
		Collection: testCollection,
	})
	if err != nil {
		t.Fatalf("连接 Milvus 失败: %v", err)
	}
	t.Cleanup(func() {
		dropCollection(t, store, testCollection)
		if err := store.Close(context.Background()); err != nil {
			t.Logf("关闭连接失败: %v", err)
		}
	})

	// 先清空，避免上次运行留下的数据影响断言。
	dropCollection(t, store, testCollection)
	return store
}

// dropCollection 删除集合。集合不存在时 Milvus 会返错，这里忽略：
// 清理是「尽力而为」的动作，不存在本身就是想要的状态。
func dropCollection(t *testing.T, store *milvusstore.Store, name string) {
	t.Helper()
	_ = store.DropCollection(context.Background(), name)
}

// unitVector 造一个指定维度、指定分量位置为 1 的向量。
// 用正交向量而不是随机向量：相似度排序才是确定的，断言不会因浮点抖动而偶发失败。
func unitVector(dim, at int) []float32 {
	vector := make([]float32, dim)
	vector[at] = 1
	return vector
}

// TestStore_RoundTrip 覆盖「建集合 → 写入 → 检索 → 按文档删除」的完整往返。
func TestStore_RoundTrip(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	const dim = 8

	if err := store.EnsureCollection(ctx, dim); err != nil {
		t.Fatalf("建集合失败: %v", err)
	}
	// 重复调用必须幂等：服务每次启动都会走一次 EnsureCollection。
	if err := store.EnsureCollection(ctx, dim); err != nil {
		t.Fatalf("重复建集合失败: %v", err)
	}

	records := []repository.VectorRecord{
		{ChunkID: "chunk-101", DatasetID: 1, DocumentID: 11, UserID: "u1", TitlePath: "第一章 / 1.1", Page: 1, Kind: "text", Vector: unitVector(dim, 0)},
		{ChunkID: "chunk-102", DatasetID: 1, DocumentID: 11, UserID: "u1", TitlePath: "第一章 / 1.1", Page: 1, Kind: "text", Vector: unitVector(dim, 1)},
		{ChunkID: "chunk-201", DatasetID: 2, DocumentID: 22, UserID: "u1", TitlePath: "第一章 / 1.1", Page: 1, Kind: "text", Vector: unitVector(dim, 2)},
	}
	if err := store.Upsert(ctx, records); err != nil {
		t.Fatalf("写入向量失败: %v", err)
	}

	// 1) 检索必须限定在指定知识库内：不做过滤会把别的知识库内容召回。
	matches, err := store.Search(ctx, repository.SearchFilter{DatasetID: 1, TopK: 10}, unitVector(dim, 0))
	if err != nil {
		t.Fatalf("检索失败: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("知识库 1 命中 %d 条, 期望 2 条（相同条件的知识库 2 的数据不应被召回）", len(matches))
	}
	if matches[0].ChunkID != "chunk-101" {
		t.Errorf("首位命中 = %s, 期望与查询向量同向的 chunk-101", matches[0].ChunkID)
	}
	if matches[0].Score < matches[1].Score {
		t.Errorf("命中未按相似度降序: %v", matches)
	}

	// 2) 覆盖写入：同一主键重复写入不应报错，也不应产生重复记录。
	if err := store.Upsert(ctx, []repository.VectorRecord{
		{ChunkID: "chunk-101", DatasetID: 1, DocumentID: 11, UserID: "u1", TitlePath: "第一章 / 1.1", Page: 1, Kind: "text", Vector: unitVector(dim, 3)},
	}); err != nil {
		t.Fatalf("覆盖写入失败: %v", err)
	}
	matches, err = store.Search(ctx, repository.SearchFilter{DatasetID: 1, TopK: 10}, unitVector(dim, 3))
	if err != nil {
		t.Fatalf("覆盖后检索失败: %v", err)
	}
	if len(matches) != 2 || matches[0].ChunkID != "chunk-101" {
		t.Fatalf("覆盖写入后命中不符: %+v", matches)
	}

	// 3) 按文档删除：重新解析文档时必须先清掉旧向量。
	deleted, err := store.DeleteByDocument(ctx, 11)
	if err != nil {
		t.Fatalf("按文档删除失败: %v", err)
	}
	if deleted == 0 {
		t.Error("按文档删除返回 0 条，与预期不符")
	}
	matches, err = store.Search(ctx, repository.SearchFilter{DatasetID: 1, TopK: 10}, unitVector(dim, 0))
	if err != nil {
		t.Fatalf("删除后检索失败: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("删除后仍命中 %d 条: %+v", len(matches), matches)
	}

	// 4) 按分段主键删除。
	if _, err := store.Delete(ctx, []string{"chunk-201"}); err != nil {
		t.Fatalf("按主键删除失败: %v", err)
	}
	if matches, err = store.Search(ctx, repository.SearchFilter{DatasetID: 2, TopK: 10}, unitVector(dim, 2)); err != nil {
		t.Fatalf("检索知识库 2 失败: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("按主键删除后仍命中 %d 条", len(matches))
	}
}

// TestStore_DimensionMismatch 覆盖维度校验：
// 已有集合的维度与当前模型不一致时必须显式报错，而不是静默放行。
func TestStore_DimensionMismatch(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	if err := store.EnsureCollection(ctx, 8); err != nil {
		t.Fatalf("建集合失败: %v", err)
	}
	err := store.EnsureCollection(ctx, 16)
	if !errors.Is(err, model.ErrDimensionMismatch) {
		t.Fatalf("错误 = %v, 期望 ErrDimensionMismatch", err)
	}
}

// TestStore_UpsertValidatesVectorDim 覆盖写入侧的防御性校验：
// 入库链路的向量来自模型，模型换了而集合没重建时，必须在写入前拦下。
func TestStore_UpsertValidatesVectorDim(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	if err := store.EnsureCollection(ctx, 8); err != nil {
		t.Fatalf("建集合失败: %v", err)
	}
	err := store.Upsert(ctx, []repository.VectorRecord{
		{ChunkID: "chunk-1", DatasetID: 1, DocumentID: 1, UserID: "u1", TitlePath: "第一章 / 1.1", Page: 1, Kind: "text", Vector: unitVector(4, 0)},
	})
	if !errors.Is(err, model.ErrDimensionMismatch) {
		t.Fatalf("错误 = %v, 期望 ErrDimensionMismatch", err)
	}
}

// TestStore_EnsureCollectionInvalidDim 覆盖非法维度：0 或负数应在发出请求前就被拒绝。
func TestStore_EnsureCollectionInvalidDim(t *testing.T) {
	store := testStore(t)
	if err := store.EnsureCollection(context.Background(), 0); err == nil {
		t.Fatal("维度为 0 时应报错")
	}
}
