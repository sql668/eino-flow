package rag_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	enginemodel "einoflow-backend/internal/engine/model"
	"einoflow-backend/internal/engine/rag"
	domainmodel "einoflow-backend/internal/model"
	"einoflow-backend/internal/repository"
)

// fakeEmbedder 是向量化模型的替身：真实实现要连 Ollama，单测里没必要。
type fakeEmbedder struct {
	name string
	dim  int
	// vectors 按调用序号依次返回，用于验证「一次检索只用一条向量」。
	vectors [][]float32
	calls   int
	err     error
}

func (f *fakeEmbedder) Name() string   { return f.name }
func (f *fakeEmbedder) Dimension() int { return f.dim }
func (f *fakeEmbedder) Embed(_ context.Context, _ []string) ([][]float32, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.calls++
	return f.vectors, nil
}

// fakeResolver 固定返回同一个向量化模型。
type fakeResolver struct {
	embedder enginemodel.Embedder
	err      error
}

func (f *fakeResolver) Embedder(context.Context, enginemodel.ScopeRef) (enginemodel.Embedder, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.embedder, nil
}

// fakeVectorStore 记录检索入参并返回预设命中。
type fakeVectorStore struct {
	matches     []repository.VectorMatch
	searchErr   error
	gotDataset  uint64
	gotTopK     int
	gotVector   []float32
	searchCalls int
}

func (f *fakeVectorStore) EnsureCollection(context.Context, int) error { return nil }
func (f *fakeVectorStore) Upsert(context.Context, []repository.VectorRecord) error {
	return nil
}
func (f *fakeVectorStore) DeleteByDocument(context.Context, uint64) (int64, error) { return 0, nil }
func (f *fakeVectorStore) Delete(context.Context, []uint64) (int64, error)         { return 0, nil }
func (f *fakeVectorStore) Close(context.Context) error                             { return nil }

func (f *fakeVectorStore) Search(
	_ context.Context, datasetID uint64, vector []float32, topK int,
) ([]repository.VectorMatch, error) {
	f.searchCalls++
	f.gotDataset = datasetID
	f.gotVector = vector
	f.gotTopK = topK
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	return f.matches, nil
}

// fakeSegmentRepo 只实现检索需要的那一个方法，其余方法直接 panic：
// 一旦检索链路调用了预期之外的方法，测试会立刻失败而不是静默通过。
type fakeSegmentRepo struct {
	items   []repository.RetrievedSegment
	listErr error
	gotIDs  []uint64
}

func (f *fakeSegmentRepo) CreateBatch(context.Context, []*domainmodel.Segment) error {
	panic("未预期调用")
}
func (f *fakeSegmentRepo) List(context.Context, repository.SegmentListFilter) ([]*domainmodel.Segment, int64, error) {
	panic("未预期调用")
}
func (f *fakeSegmentRepo) DeleteByDocument(context.Context, uint64) (int64, error) {
	panic("未预期调用")
}
func (f *fakeSegmentRepo) AttachVectorIDs(context.Context, uint64) (int64, error) {
	panic("未预期调用")
}

func (f *fakeSegmentRepo) ListForRetrieval(
	_ context.Context, _ uint64, ids []uint64,
) ([]repository.RetrievedSegment, error) {
	f.gotIDs = ids
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.items, nil
}

// newTestRetriever 组装一个全部依赖都打桩的检索器。
func newTestRetriever(
	embedder enginemodel.Embedder, vectors *fakeVectorStore, segments *fakeSegmentRepo,
) *rag.Retriever {
	return rag.NewRetriever(rag.RetrieverDeps{
		Models:      &fakeResolver{embedder: embedder},
		Vectors:     vectors,
		Segments:    segments,
		DefaultTopK: 3,
	})
}

// TestRetriever_Retrieve 覆盖正常召回：命中按向量分数顺序组装、排名连续、溯源字段带出。
func TestRetriever_Retrieve(t *testing.T) {
	embedder := &fakeEmbedder{name: "fake-embed", dim: 4, vectors: [][]float32{{0.1, 0.2, 0.3, 0.4}}}
	vectors := &fakeVectorStore{matches: []repository.VectorMatch{
		{SegmentID: 11, Score: 0.91},
		{SegmentID: 22, Score: 0.80},
	}}
	segments := &fakeSegmentRepo{items: []repository.RetrievedSegment{
		// 刻意乱序返回：检索器必须按向量分数而不是按数据库返回顺序组装结果。
		{Segment: &domainmodel.Segment{ID: 22, DocumentID: 2, Content: "第二段", Page: 2, TitlePath: []string{"B"}},
			DocumentName: "b.pdf"},
		{Segment: &domainmodel.Segment{ID: 11, DocumentID: 1, Content: "第一段", Page: 1, Locator: "page:1/block:3",
			Kind: "table", TitlePath: []string{"A", "A1"}}, DocumentName: "a.pdf"},
	}}

	retriever := newTestRetriever(embedder, vectors, segments)
	out, err := retriever.Retrieve(context.Background(), rag.RetrieveRequest{
		DatasetID: 7, Query: "  如何测量电压  ",
	})
	if err != nil {
		t.Fatalf("检索失败: %v", err)
	}
	if out.EmbeddingModel != "fake-embed" {
		t.Errorf("向量模型 = %q, 期望 fake-embed", out.EmbeddingModel)
	}
	if len(out.Hits) != 2 {
		t.Fatalf("命中数 = %d, 期望 2", len(out.Hits))
	}
	if out.Hits[0].SegmentID != 11 || out.Hits[1].SegmentID != 22 {
		t.Errorf("命中顺序 = [%d %d], 期望按分数降序 [11 22]", out.Hits[0].SegmentID, out.Hits[1].SegmentID)
	}
	if out.Hits[0].Rank != 1 || out.Hits[1].Rank != 2 {
		t.Errorf("排名 = [%d %d], 期望 [1 2]", out.Hits[0].Rank, out.Hits[1].Rank)
	}

	first := out.Hits[0]
	if first.DocumentName != "a.pdf" || first.Page != 1 || first.Kind != "table" || first.Locator != "page:1/block:3" {
		t.Errorf("溯源字段未正确带出: %+v", first)
	}
	if len(first.TitlePath) != 2 || first.TitlePath[1] != "A1" {
		t.Errorf("标题路径 = %v, 期望 [A A1]", first.TitlePath)
	}

	// 请求没带 top_k，应落到默认值。
	if vectors.gotTopK != 3 {
		t.Errorf("topK = %d, 期望默认值 3", vectors.gotTopK)
	}
	if vectors.gotDataset != 7 {
		t.Errorf("检索知识库 = %d, 期望 7", vectors.gotDataset)
	}
	if len(vectors.gotVector) != 4 || vectors.gotVector[0] != float32(0.1) {
		t.Errorf("传给向量库的向量 = %v, 期望使用模型输出", vectors.gotVector)
	}
	if embedder.calls != 1 {
		t.Errorf("向量化调用次数 = %d, 期望 1", embedder.calls)
	}
}

// TestRetriever_RetrieveSkipsMissingSegment 覆盖「向量在、分段不在」的情况：
// 文档刚被删除时向量库与业务库会短暂不一致，此时应跳过而不是报错或返回空内容。
func TestRetriever_RetrieveSkipsMissingSegment(t *testing.T) {
	embedder := &fakeEmbedder{name: "fake-embed", dim: 2, vectors: [][]float32{{1, 0}}}
	vectors := &fakeVectorStore{matches: []repository.VectorMatch{
		{SegmentID: 11, Score: 0.9},
		{SegmentID: 22, Score: 0.5},
	}}
	segments := &fakeSegmentRepo{items: []repository.RetrievedSegment{
		{Segment: &domainmodel.Segment{ID: 22, Content: "仅此一段"}, DocumentName: "b.pdf"},
	}}

	out, err := newTestRetriever(embedder, vectors, segments).Retrieve(
		context.Background(), rag.RetrieveRequest{DatasetID: 1, Query: "任意"})
	if err != nil {
		t.Fatalf("检索失败: %v", err)
	}
	if len(out.Hits) != 1 || out.Hits[0].SegmentID != 22 {
		t.Fatalf("命中 = %+v, 期望只保留存在的分段 22", out.Hits)
	}
	// 排名按实际返回顺序重排，不能留下空洞。
	if out.Hits[0].Rank != 1 {
		t.Errorf("排名 = %d, 期望 1", out.Hits[0].Rank)
	}
}

// TestRetriever_RetrieveEmptyResults 覆盖「一条都没召回」：应返回空列表而不是 nil，
// 前端拿到 null 与 [] 的处理路径不同，接口契约要稳定。
func TestRetriever_RetrieveEmptyResults(t *testing.T) {
	embedder := &fakeEmbedder{name: "fake-embed", dim: 2, vectors: [][]float32{{1, 0}}}
	vectors := &fakeVectorStore{}
	segments := &fakeSegmentRepo{}

	out, err := newTestRetriever(embedder, vectors, segments).Retrieve(
		context.Background(), rag.RetrieveRequest{DatasetID: 1, Query: "任意"})
	if err != nil {
		t.Fatalf("检索失败: %v", err)
	}
	if out.Hits == nil {
		t.Error("命中列表为 nil，期望空切片")
	}
	if len(out.Hits) != 0 {
		t.Errorf("命中数 = %d, 期望 0", len(out.Hits))
	}
	// 没有命中就不该再查分段表，省一次无用的数据库往返。
	if segments.gotIDs != nil {
		t.Errorf("无命中时仍查询了分段表: %v", segments.gotIDs)
	}
}

// TestRetriever_RetrieveInvalidInput 覆盖入参校验。
func TestRetriever_RetrieveInvalidInput(t *testing.T) {
	cases := map[string]rag.RetrieveRequest{
		"空问题":  {DatasetID: 1, Query: "   "},
		"缺知识库": {DatasetID: 0, Query: "问题"},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			retriever := newTestRetriever(
				&fakeEmbedder{name: "x", dim: 2, vectors: [][]float32{{1, 0}}},
				&fakeVectorStore{}, &fakeSegmentRepo{})
			if _, err := retriever.Retrieve(context.Background(), req); err == nil {
				t.Fatal("期望报错，实际通过")
			}
		})
	}
}

// TestRetriever_RetrievePropagatesErrors 覆盖依赖失败时的错误透传：
// 向量化失败与向量库失败都必须暴露出来，不能退化成「检索不到」。
func TestRetriever_RetrievePropagatesErrors(t *testing.T) {
	t.Run("向量化失败", func(t *testing.T) {
		retriever := newTestRetriever(
			&fakeEmbedder{err: errors.New("ollama 不可用")},
			&fakeVectorStore{}, &fakeSegmentRepo{})
		_, err := retriever.Retrieve(context.Background(), rag.RetrieveRequest{DatasetID: 1, Query: "问题"})
		if err == nil || !strings.Contains(err.Error(), "向量化检索内容失败") {
			t.Fatalf("错误 = %v, 期望包含「向量化检索内容失败」", err)
		}
	})

	t.Run("向量库失败", func(t *testing.T) {
		retriever := newTestRetriever(
			&fakeEmbedder{name: "x", dim: 2, vectors: [][]float32{{1, 0}}},
			&fakeVectorStore{searchErr: errors.New("milvus 不可用")}, &fakeSegmentRepo{})
		_, err := retriever.Retrieve(context.Background(), rag.RetrieveRequest{DatasetID: 1, Query: "问题"})
		if err == nil || !strings.Contains(err.Error(), "milvus 不可用") {
			t.Fatalf("错误 = %v, 期望包含 milvus 不可用", err)
		}
	})

	t.Run("向量化返回条数异常", func(t *testing.T) {
		// 模型返回两条向量而只问了一个问题，说明服务端行为异常，必须报错。
		retriever := newTestRetriever(
			&fakeEmbedder{name: "x", dim: 2, vectors: [][]float32{{1, 0}, {0, 1}}},
			&fakeVectorStore{}, &fakeSegmentRepo{})
		if _, err := retriever.Retrieve(
			context.Background(), rag.RetrieveRequest{DatasetID: 1, Query: "问题"}); err == nil {
			t.Fatal("期望报错，实际通过")
		}
	})
}
