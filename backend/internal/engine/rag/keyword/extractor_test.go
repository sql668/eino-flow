package keyword_test

import (
	"context"
	"errors"
	"testing"

	"einoflow-backend/internal/engine/rag/keyword"
)

// fakeTokenizer 按「预置分词表」返回结果，用来验证抽取逻辑而不依赖 Milvus。
type fakeTokenizer struct {
	table map[string][]string
	err   error
	// gotTexts 记录最后一次调用收到的文本，用于断言批量行为。
	gotTexts []string
}

func (f *fakeTokenizer) Tokenize(_ context.Context, texts []string) ([][]string, error) {
	f.gotTexts = texts
	if f.err != nil {
		return nil, f.err
	}
	out := make([][]string, len(texts))
	for i, text := range texts {
		out[i] = f.table[text]
	}
	return out, nil
}

// shortTokenizer 模拟「服务返回条数少于入参」的异常行为。
type shortTokenizer struct{}

func (shortTokenizer) Tokenize(_ context.Context, texts []string) ([][]string, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	return make([][]string, len(texts)-1), nil
}

func TestTFIDF_ExtractBasic(t *testing.T) {
	tokenizer := &fakeTokenizer{table: map[string][]string{
		"段一": {"直流电压", "测量", "表笔", "表笔"},
		"段二": {"交流电压", "测量", "频率"},
	}}
	extractor := keyword.NewTFIDF(tokenizer)

	got, err := extractor.Extract(context.Background(), []string{"段一", "段二"}, 3)
	if err != nil {
		t.Fatalf("抽取失败: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("结果条数 = %d, 期望 2", len(got))
	}
	// 段一里「表笔」出现两次且只在本段出现 → 权重最高，应排第一。
	if got[0][0] != "表笔" {
		t.Errorf("段一首位关键词 = %q, 期望 表笔（词频高且只在本文档出现）: %v", got[0][0], got[0])
	}
	if len(got[0]) > 3 || len(got[1]) > 3 {
		t.Errorf("关键词数超过上限: %v / %v", got[0], got[1])
	}
}

func TestTFIDF_FiltersStopwordsAndNoise(t *testing.T) {
	tokenizer := &fakeTokenizer{table: map[string][]string{
		"段": {"的", "可以", "的", "1234", "——", "电压", "测"},
	}}
	extractor := keyword.NewTFIDF(tokenizer)

	got, err := extractor.Extract(context.Background(), []string{"段"}, 5)
	if err != nil {
		t.Fatalf("抽取失败: %v", err)
	}
	// 期望只留下「电压」：停用词、纯数字、纯符号、中文单字都被过滤。
	if len(got[0]) != 1 || got[0][0] != "电压" {
		t.Fatalf("关键词 = %v, 期望只剩 [电压]", got[0])
	}
}

func TestTFIDF_DeterministicOrder(t *testing.T) {
	// 同分词的排序必须稳定，否则每次入库关键词都会变，人工改过的会被冲掉。
	tokenizer := &fakeTokenizer{table: map[string][]string{"段": {"甲词", "乙词", "丙词"}}}
	extractor := keyword.NewTFIDF(tokenizer)

	first, err := extractor.Extract(context.Background(), []string{"段"}, 3)
	if err != nil {
		t.Fatalf("抽取失败: %v", err)
	}
	for i := 0; i < 5; i++ {
		again, err := extractor.Extract(context.Background(), []string{"段"}, 3)
		if err != nil {
			t.Fatalf("第 %d 次抽取失败: %v", i, err)
		}
		for j := range first[0] {
			if first[0][j] != again[0][j] {
				t.Fatalf("第 %d 次结果不一致：%v vs %v", i, first[0], again[0])
			}
		}
	}
}

func TestTFIDF_BatchesAndErrors(t *testing.T) {
	t.Run("批量调用一次", func(t *testing.T) {
		tokenizer := &fakeTokenizer{table: map[string][]string{"a": {"甲"}, "b": {"乙"}}}
		extractor := keyword.NewTFIDF(tokenizer)
		if _, err := extractor.Extract(context.Background(), []string{"a", "b"}, 2); err != nil {
			t.Fatalf("抽取失败: %v", err)
		}
		if len(tokenizer.gotTexts) != 2 {
			t.Errorf("分词入参条数 = %d, 期望 2（应一次批量提交）", len(tokenizer.gotTexts))
		}
	})

	t.Run("分词失败向上传递", func(t *testing.T) {
		extractor := keyword.NewTFIDF(&fakeTokenizer{err: errors.New("分析器不可用")})
		if _, err := extractor.Extract(context.Background(), []string{"a"}, 2); err == nil {
			t.Fatal("分词失败时应报错")
		}
	})

	t.Run("条数不一致要报错", func(t *testing.T) {
		// 分词服务少返回一条时不能错位配对：那会把 A 段的关键词安到 B 段上。
		extractor := keyword.NewTFIDF(shortTokenizer{})
		_, err := extractor.Extract(context.Background(), []string{"a", "b"}, 2)
		if err == nil {
			t.Fatal("条数不一致时应报错")
		}
	})

	t.Run("空输入", func(t *testing.T) {
		extractor := keyword.NewTFIDF(&fakeTokenizer{})
		got, err := extractor.Extract(context.Background(), nil, 3)
		if err != nil || got != nil {
			t.Fatalf("空输入应直接返回，实际 %v / %v", got, err)
		}
	})
}
