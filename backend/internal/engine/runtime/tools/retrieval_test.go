package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"

	"einoflow-backend/internal/engine/runtime"
)

// fakeSearcher 记录调用参数，用于断言"校验失败时没有发起检索"。
type fakeSearcher struct {
	calls      int
	gotDataset uint64
	gotQuery   string
	hits       []RetrievalHit
	err        error
}

func (f *fakeSearcher) Search(_ context.Context, datasetID uint64, query string) ([]RetrievalHit, error) {
	f.calls++
	f.gotDataset = datasetID
	f.gotQuery = query
	return f.hits, f.err
}

func newRetrievalToolForTest(t *testing.T, searcher Searcher, defaultDatasetID uint64) (tool.InvokableTool, error) {
	t.Helper()
	return NewKnowledgeRetrievalTool(searcher, defaultDatasetID)
}

func TestKnowledgeRetrievalTool_ReturnsHitsWithSources(t *testing.T) {
	searcher := &fakeSearcher{
		hits: []RetrievalHit{
			{
				DocumentName: "产品手册.pdf",
				Page:         6,
				TitlePath:    []string{"电池", "更换电池"},
				Content:      "先断开电源，再取下电池盖。",
			},
			{
				DocumentName: "安全手册.pdf",
				Content:      "操作前请佩戴绝缘手套。",
			},
		},
	}
	retrievalTool, err := newRetrievalToolForTest(t, searcher, 7)
	if err != nil {
		t.Fatalf("构造检索工具失败: %v", err)
	}

	got, err := retrievalTool.InvokableRun(context.Background(),
		`{"dataset_ids":[7],"query":"怎么更换电池"}`)
	if err != nil {
		t.Fatalf("执行检索工具失败: %v", err)
	}
	if searcher.calls != 1 || searcher.gotDataset != 7 || searcher.gotQuery != "怎么更换电池" {
		t.Fatalf("检索参数传递错误: calls=%d dataset=%d query=%q",
			searcher.calls, searcher.gotDataset, searcher.gotQuery)
	}

	// 「带出处」是工具结果能被引用的前提：文档名、页码、标题路径都要出现在给模型的文本里。
	for _, want := range []string{
		"产品手册.pdf", "第 6 页", "电池 / 更换电池", "先断开电源",
		"安全手册.pdf", "绝缘手套",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("工具结果缺少 %q:\n%s", want, got)
		}
	}
}

func TestKnowledgeRetrievalTool_EmptyResultIsNotError(t *testing.T) {
	searcher := &fakeSearcher{}
	retrievalTool, err := newRetrievalToolForTest(t, searcher, 7)
	if err != nil {
		t.Fatalf("构造检索工具失败: %v", err)
	}

	got, err := retrievalTool.InvokableRun(context.Background(),
		`{"dataset_ids":[7],"query":"不存在的主题"}`)
	if err != nil {
		t.Fatalf("空知识库不应报错（没有命中是正常结果）: %v", err)
	}
	if !strings.Contains(got, "没有命中") {
		t.Fatalf("空结果应明确告诉模型没有命中，实际: %q", got)
	}
}

func TestKnowledgeRetrievalTool_UsesBoundDatasetWhenOmitted(t *testing.T) {
	searcher := &fakeSearcher{}
	retrievalTool, err := newRetrievalToolForTest(t, searcher, 7)
	if err != nil {
		t.Fatalf("构造检索工具失败: %v", err)
	}

	if _, err := retrievalTool.InvokableRun(context.Background(), `{"query":"怎么更换电池"}`); err != nil {
		t.Fatalf("省略 dataset_ids 时应使用本次绑定的知识库: %v", err)
	}
	if searcher.calls != 1 || searcher.gotDataset != 7 {
		t.Fatalf("应检索绑定的知识库 7，实际 calls=%d dataset=%d", searcher.calls, searcher.gotDataset)
	}
}

func TestKnowledgeRetrievalTool_RejectsOutOfScopeDataset(t *testing.T) {
	searcher := &fakeSearcher{}
	retrievalTool, err := newRetrievalToolForTest(t, searcher, 7)
	if err != nil {
		t.Fatalf("构造检索工具失败: %v", err)
	}

	_, err = retrievalTool.InvokableRun(context.Background(),
		`{"dataset_ids":[8],"query":"怎么更换电池"}`)
	if !errors.Is(err, ErrDatasetOutOfScope) {
		t.Fatalf("越界知识库应返回 ErrDatasetOutOfScope，实际: %v", err)
	}
	if searcher.calls != 0 {
		t.Fatalf("越界时不允许发起检索，实际调用了 %d 次", searcher.calls)
	}
}

func TestKnowledgeRetrievalTool_RejectsMultipleDatasetsBeforeSearch(t *testing.T) {
	searcher := &fakeSearcher{}
	retrievalTool, err := newRetrievalToolForTest(t, searcher, 7)
	if err != nil {
		t.Fatalf("构造检索工具失败: %v", err)
	}

	_, err = retrievalTool.InvokableRun(context.Background(),
		`{"dataset_ids":[1,2],"query":"怎么更换电池"}`)
	if !errors.Is(err, ErrMultipleDatasets) {
		t.Fatalf("传两个知识库应返回可判定的 ErrMultipleDatasets，实际: %v", err)
	}
	if searcher.calls != 0 {
		t.Fatalf("校验失败时不允许发起任何检索，实际调用了 %d 次", searcher.calls)
	}
}

func TestKnowledgeRetrievalTool_RequiresDatasetBeforeSearch(t *testing.T) {
	searcher := &fakeSearcher{}
	retrievalTool, err := newRetrievalToolForTest(t, searcher, 0)
	if err != nil {
		t.Fatalf("构造检索工具失败: %v", err)
	}

	for _, args := range []string{
		`{"dataset_ids":[],"query":"怎么更换电池"}`,
		`{"dataset_ids":[0],"query":"怎么更换电池"}`,
	} {
		_, err := retrievalTool.InvokableRun(context.Background(), args)
		if !errors.Is(err, ErrMissingDataset) {
			t.Fatalf("缺少知识库应返回 ErrMissingDataset，参数 %s 实际: %v", args, err)
		}
	}
	if searcher.calls != 0 {
		t.Fatalf("校验失败时不允许发起任何检索，实际调用了 %d 次", searcher.calls)
	}
}

func TestKnowledgeRetrievalTool_PropagatesSearcherError(t *testing.T) {
	sentinel := errors.New("向量库不可用")
	searcher := &fakeSearcher{err: sentinel}
	retrievalTool, err := newRetrievalToolForTest(t, searcher, 7)
	if err != nil {
		t.Fatalf("构造检索工具失败: %v", err)
	}

	_, err = retrievalTool.InvokableRun(context.Background(),
		`{"dataset_ids":[7],"query":"怎么更换电池"}`)
	if !errors.Is(err, sentinel) {
		t.Fatalf("检索失败应原样上抛给运行时，实际: %v", err)
	}
}

func TestKnowledgeRetrievalTool_RejectsEmptyQuery(t *testing.T) {
	searcher := &fakeSearcher{}
	retrievalTool, err := newRetrievalToolForTest(t, searcher, 7)
	if err != nil {
		t.Fatalf("构造检索工具失败: %v", err)
	}

	if _, err := retrievalTool.InvokableRun(context.Background(),
		`{"dataset_ids":[7],"query":"   "}`); err == nil {
		t.Fatal("空查询必须报错，否则会白跑一次检索")
	}
	if searcher.calls != 0 {
		t.Fatalf("空查询不应发起检索，实际调用了 %d 次", searcher.calls)
	}
}

func TestToolNamesForChat(t *testing.T) {
	names, err := ToolNamesForChat(nil)
	if err != nil {
		t.Fatalf("未指定知识库不应报错: %v", err)
	}
	if len(names) != 1 || names[0] != calculatorName {
		t.Fatalf("未指定知识库时只应有计算工具，实际: %v", names)
	}

	names, err = ToolNamesForChat([]uint64{7})
	if err != nil {
		t.Fatalf("指定一个知识库不应报错: %v", err)
	}
	if len(names) != 2 || names[0] != RetrievalToolName || names[1] != calculatorName {
		t.Fatalf("指定知识库时应启用检索 + 计算，实际: %v", names)
	}

	if _, err := ToolNamesForChat([]uint64{1, 2}); !errors.Is(err, ErrMultipleDatasets) {
		t.Fatalf("多个知识库应返回 ErrMultipleDatasets，实际: %v", err)
	}
}

func TestToolNamesForChat_SelectsFromRegistry(t *testing.T) {
	searcher := &fakeSearcher{}
	retrievalTool, err := NewKnowledgeRetrievalTool(searcher, 7)
	if err != nil {
		t.Fatalf("构造检索工具失败: %v", err)
	}
	calculator, err := NewCalculator()
	if err != nil {
		t.Fatalf("构造计算工具失败: %v", err)
	}

	registry := runtime.NewRegistry()
	if err := registry.Register(context.Background(), retrievalTool); err != nil {
		t.Fatalf("注册检索工具失败: %v", err)
	}
	if err := registry.Register(context.Background(), calculator); err != nil {
		t.Fatalf("注册计算工具失败: %v", err)
	}

	names, err := ToolNamesForChat(nil)
	if err != nil {
		t.Fatalf("组装工具名失败: %v", err)
	}
	subset, err := registry.Select(names...)
	if err != nil {
		t.Fatalf("按本次问答组装工具集失败: %v", err)
	}
	if got := subset.Names(); len(got) != 1 || got[0] != calculatorName {
		t.Fatalf("未指定知识库时工具集不应含检索工具，实际: %v", got)
	}
}
