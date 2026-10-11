package service_test

import (
	"context"
	"errors"
	"testing"

	"einoflow-backend/internal/engine/rag"
	"einoflow-backend/internal/model"
	"einoflow-backend/internal/repository"
	"einoflow-backend/internal/service"
)

// fakeDatasetRepo 只需要 GetByID 能返回预设结果。
type fakeDatasetRepo struct {
	err error
}

func (f *fakeDatasetRepo) Create(context.Context, *model.Dataset) error { panic("未预期调用") }

func (f *fakeDatasetRepo) GetByID(context.Context, uint64) (*model.Dataset, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &model.Dataset{ID: 1, Name: "知识库"}, nil
}

func (f *fakeDatasetRepo) List(context.Context, repository.DatasetListFilter) ([]*model.Dataset, int64, error) {
	panic("未预期调用")
}

func (f *fakeDatasetRepo) Update(context.Context, *model.Dataset) error { panic("未预期调用") }

func (f *fakeDatasetRepo) SoftDelete(context.Context, uint64) error { panic("未预期调用") }

// fakeRetriever 是可编程的检索替身。
type fakeRetriever struct {
	output rag.RetrieveOutput
	err    error
	got    rag.RetrieveRequest
}

func (f *fakeRetriever) Retrieve(_ context.Context, req rag.RetrieveRequest) (rag.RetrieveOutput, error) {
	f.got = req
	return f.output, f.err
}

// fakeTraceRepo 记录留痕写入，用于断言检索留痕的内容。
type fakeTraceRepo struct {
	run       *model.RetrievalRun
	hits      []*model.RetrievalHit
	nextRunID uint64
	err       error
}

func (f *fakeTraceRepo) CreateRun(_ context.Context, run *model.RetrievalRun) error {
	if f.err != nil {
		return f.err
	}
	f.nextRunID++
	run.ID = f.nextRunID
	f.run = run
	return nil
}

func (f *fakeTraceRepo) CreateHits(_ context.Context, hits []*model.RetrievalHit) error {
	if f.err != nil {
		return f.err
	}
	f.hits = hits
	return nil
}

// newTestRetrievalService 组装一个依赖全部打桩的检索服务。
func newTestRetrievalService(
	datasets repository.DatasetRepository, retriever service.Retriever, traces repository.RetrievalRepository,
) *service.RetrievalService {
	return service.NewRetrievalService(datasets, retriever, traces, service.RetrievalDefaults{TopK: 4})
}

// TestRetrievalService_SearchRecordsTrace 覆盖正常检索：返回命中，并把检索与命中写入留痕。
func TestRetrievalService_SearchRecordsTrace(t *testing.T) {
	// 构造两阶段数据：召回 3 条，重排后留 2 条，且顺序发生了变化。
	// 这正是「两阶段分数落库」要还原的内容。
	// 向量检索下排序分与向量路分数是同一个值，两者都要写进留痕。
	scoreFirst, scoreSecond, scoreThird := float32(0.93), float32(0.81), float32(0.75)
	rerankFirst, rerankSecond := float32(0.95), float32(0.60)
	retriever := &fakeRetriever{output: rag.RetrieveOutput{
		EmbeddingModel: "bge-m3:latest",
		Mode:           model.SearchModeVector,
		RerankModel:    "bge-reranker-v2-m3",
		Candidates: []rag.Hit{
			{SegmentID: 11, DocumentID: 1, DocumentName: "a.pdf", Rank: 1, Score: scoreFirst, VectorScore: &scoreFirst, Content: "第一段", Page: 1},
			{SegmentID: 22, DocumentID: 2, DocumentName: "b.pdf", Rank: 2, Score: scoreSecond, VectorScore: &scoreSecond, Content: "第二段", Page: 3},
			{SegmentID: 33, DocumentID: 3, DocumentName: "c.pdf", Rank: 3, Score: scoreThird, VectorScore: &scoreThird, Content: "第三段", Page: 5},
		},
		Hits: []rag.Hit{
			{SegmentID: 22, DocumentID: 2, DocumentName: "b.pdf", Rank: 1, Score: scoreSecond, VectorScore: &scoreSecond, RerankScore: &rerankFirst, Content: "第二段", Page: 3},
			{SegmentID: 11, DocumentID: 1, DocumentName: "a.pdf", Rank: 2, Score: scoreFirst, VectorScore: &scoreFirst, RerankScore: &rerankSecond, Content: "第一段", Page: 1},
		},
	}}
	traces := &fakeTraceRepo{}

	result, err := newTestRetrievalService(&fakeDatasetRepo{}, retriever, traces).Search(
		context.Background(), service.SearchInput{DatasetID: 1, Query: "  电压测量  "})
	if err != nil {
		t.Fatalf("检索失败: %v", err)
	}

	// 问题应先去掉首尾空白再进入检索链路。
	if retriever.got.Query != "电压测量" {
		t.Errorf("透传的查询 = %q, 期望已去空白", retriever.got.Query)
	}
	if retriever.got.TopK != 4 {
		t.Errorf("透传的 topK = %d, 期望默认值 4", retriever.got.TopK)
	}
	if result.RunID == 0 {
		t.Error("检索记录 ID 为空，留痕未回填")
	}
	if len(result.Hits) != 2 {
		t.Fatalf("命中数 = %d, 期望 2", len(result.Hits))
	}

	if traces.run == nil {
		t.Fatal("未写入检索记录")
	}
	if traces.run.Query != "电压测量" || traces.run.HitCount != 2 || traces.run.TopK != 4 {
		t.Errorf("检索记录内容不符: %+v", traces.run)
	}
	if traces.run.EmbeddingModel != "bge-m3:latest" {
		t.Errorf("检索记录里的向量模型 = %q, 期望 bge-m3:latest", traces.run.EmbeddingModel)
	}
	if traces.run.RerankModel != "bge-reranker-v2-m3" {
		t.Errorf("检索记录里的重排模型 = %q", traces.run.RerankModel)
	}
	// 检索方式必须留痕：同一个问题换个方式结果就变了，不记就解释不清差异。
	if traces.run.SearchMode != string(model.SearchModeVector) {
		t.Errorf("检索记录里的检索方式 = %q, 期望 vector", traces.run.SearchMode)
	}
	// 召回条数是重排前的数量（3），不是最终命中数（2）：两阶段都要能还原。
	if traces.run.CandidateCount != 3 {
		t.Errorf("召回条数 = %d, 期望 3", traces.run.CandidateCount)
	}
	if traces.run.ErrorMessage != "" {
		t.Errorf("成功检索不应有错误信息: %q", traces.run.ErrorMessage)
	}
	// 留痕应有两组：召回阶段 3 条（向量分数、无重排分）+ 重排阶段 2 条（两个分数都有）。
	vectorRows := filterStage(traces.hits, model.RetrievalStageVector)
	rerankRows := filterStage(traces.hits, model.RetrievalStageRerank)
	if len(vectorRows) != 3 {
		t.Fatalf("召回阶段留痕 = %d 条, 期望 3", len(vectorRows))
	}
	if len(rerankRows) != 2 {
		t.Fatalf("重排阶段留痕 = %d 条, 期望 2", len(rerankRows))
	}
	if vectorRows[0].RerankScore != nil {
		t.Error("召回阶段不应带重排分数")
	}
	if vectorRows[2].Rank != 3 || vectorRows[2].SegmentID != 33 {
		t.Errorf("召回阶段第三条不符: %+v", vectorRows[2])
	}
	// 重排阶段的排名反映精排顺序：第 1 名是 22（向量第 2 名），并带上两个分数。
	if rerankRows[0].Rank != 1 || rerankRows[0].SegmentID != 22 {
		t.Errorf("重排阶段首位不符: %+v", rerankRows[0])
	}
	if rerankRows[0].RerankScore == nil || *rerankRows[0].RerankScore != 0.95 {
		t.Errorf("重排分数不符: %v", rerankRows[0].RerankScore)
	}
	if rerankRows[0].Score != 0.81 {
		t.Errorf("重排行的向量分数 = %v, 期望 0.81（两阶段分数都要留）", rerankRows[0].Score)
	}
}

// filterStage 按阶段筛选留痕，避免测试里到处写循环。
func filterStage(hits []*model.RetrievalHit, stage model.RetrievalStage) []*model.RetrievalHit {
	filtered := make([]*model.RetrievalHit, 0, len(hits))
	for _, hit := range hits {
		if hit.Stage == stage {
			filtered = append(filtered, hit)
		}
	}
	return filtered
}

// TestRetrievalService_SearchRecordsFailure 覆盖检索失败也要留痕：
// 「用户说搜不到」时必须有据可查，失败不记录等于把问题留给下次复现。
func TestRetrievalService_SearchRecordsFailure(t *testing.T) {
	traces := &fakeTraceRepo{}
	retriever := &fakeRetriever{
		err:    errors.New("向量库不可用"),
		output: rag.RetrieveOutput{EmbeddingModel: "bge-m3:latest"},
	}

	_, err := newTestRetrievalService(&fakeDatasetRepo{}, retriever, traces).Search(
		context.Background(), service.SearchInput{DatasetID: 1, Query: "问题"})
	if err == nil {
		t.Fatal("检索失败时必须返回错误")
	}
	if traces.run == nil {
		t.Fatal("失败的检索也必须留痕")
	}
	if traces.run.ErrorMessage == "" {
		t.Error("失败留痕里应记录错误原因")
	}
}

// TestRetrievalService_SearchTraceFailureDoesNotFailSearch 覆盖留痕异常不影响检索结果：
// 记录是副作用，不能因为它写不进去就让用户拿不到答案。
func TestRetrievalService_SearchTraceFailureDoesNotFailSearch(t *testing.T) {
	traces := &fakeTraceRepo{err: errors.New("数据库抖动")}
	retriever := &fakeRetriever{output: rag.RetrieveOutput{
		EmbeddingModel: "bge-m3:latest",
		Hits:           []rag.Hit{{SegmentID: 11, Rank: 1, Score: 0.9, Content: "内容"}},
	}}

	result, err := newTestRetrievalService(&fakeDatasetRepo{}, retriever, traces).Search(
		context.Background(), service.SearchInput{DatasetID: 1, Query: "问题"})
	if err != nil {
		t.Fatalf("留痕失败不应影响检索: %v", err)
	}
	if len(result.Hits) != 1 {
		t.Errorf("命中数 = %d, 期望 1", len(result.Hits))
	}
}

// TestRetrievalService_SearchInvalidInput 覆盖入参校验与错误语义。
func TestRetrievalService_SearchInvalidInput(t *testing.T) {
	cases := []struct {
		name    string
		input   service.SearchInput
		wantErr error
	}{
		{"缺知识库", service.SearchInput{Query: "问题"}, service.ErrInvalidInput},
		{"空问题", service.SearchInput{DatasetID: 1, Query: "   "}, service.ErrInvalidInput},
		{
			"topK 超上限",
			service.SearchInput{DatasetID: 1, Query: "问题", TopK: model.MaxRetrievalTopK + 1},
			service.ErrInvalidInput,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newTestRetrievalService(
				&fakeDatasetRepo{}, &fakeRetriever{}, &fakeTraceRepo{}).Search(context.Background(), tc.input)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("错误 = %v, 期望 %v", err, tc.wantErr)
			}
		})
	}
}

// TestRetrievalService_SearchDatasetNotFound 覆盖知识库不存在时返回「不存在」而不是空结果。
func TestRetrievalService_SearchDatasetNotFound(t *testing.T) {
	// 仓储层返回的是 ErrNotFound，服务层必须翻译成对外的 ErrNotFound（对应 HTTP 404）。
	_, err := newTestRetrievalService(
		&fakeDatasetRepo{err: repository.ErrNotFound}, &fakeRetriever{}, &fakeTraceRepo{},
	).Search(context.Background(), service.SearchInput{DatasetID: 999, Query: "问题"})
	if !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("错误 = %v, 期望 ErrNotFound", err)
	}
}
