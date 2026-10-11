package keyword_test

import (
	"context"
	"strings"
	"testing"

	"einoflow-backend/internal/engine/rag/keyword"
)

// TestLocalTokenizer_FindsRepeatedTerms 覆盖核心行为：
// 反复出现的多字术语要被抽出来，而被更长术语包住的短串要被去掉。
func TestLocalTokenizer_FindsRepeatedTerms(t *testing.T) {
	texts := []string{
		"数字万用表用于测量电压与电流，万用表的选择量程很关键。",
		"使用万用表前请检查量程，万用表的量程选择错误会损坏仪表。",
		"万用表的电池仓位于背面，更换电池后请重新确认量程。",
	}
	tokens, err := keyword.NewLocalTokenizer().Tokenize(context.Background(), texts)
	if err != nil {
		t.Fatalf("分词失败: %v", err)
	}
	if len(tokens) != len(texts) {
		t.Fatalf("返回 %d 组词，期望 %d 组", len(tokens), len(texts))
	}

	all := make(map[string]struct{})
	for _, group := range tokens {
		for _, token := range group {
			all[token] = struct{}{}
		}
	}
	if _, ok := all["万用表"]; !ok {
		t.Errorf("未抽出「万用表」：%v", all)
	}
	if _, ok := all["量程"]; !ok {
		t.Errorf("未抽出「量程」：%v", all)
	}
	// 「万用」总是被「万用表」包住，不该单独成词。
	if _, ok := all["万用"]; ok {
		t.Errorf("被更长词包住的短串仍被保留：%v", all)
	}
}

// TestLocalTokenizer_DropsFixedLongPhrase 覆盖邻字多样性过滤：
// 只在固定上下文里出现的长片段不是词（否则会把整句当关键词）。
func TestLocalTokenizer_DropsFixedLongPhrase(t *testing.T) {
	texts := []string{
		"请在使用本表之前仔细阅读该手册。请在使用本表之前确认量程。",
		"请在使用本表之前确认表笔接触良好，量程选择要与测量对象一致。",
	}
	tokens, err := keyword.NewLocalTokenizer().Tokenize(context.Background(), texts)
	if err != nil {
		t.Fatalf("分词失败: %v", err)
	}
	for _, group := range tokens {
		for _, token := range group {
			if strings.HasPrefix(token, "请在使用") {
				t.Errorf("固定长片段被当成词：%q", token)
			}
		}
	}
}

// TestLocalTokenizer_AsciiTokens 覆盖型号这类拉丁词：
// 它们本来就有词边界，直接切出来即可（单字母碎片丢弃）。
func TestLocalTokenizer_AsciiTokens(t *testing.T) {
	texts := []string{
		"型号 RS-12 的显示屏支持 a 级背光，输出 RS-12 校准证书。",
		"RS-12 的电池型号是 9V。",
	}
	tokens, err := keyword.NewLocalTokenizer().Tokenize(context.Background(), texts)
	if err != nil {
		t.Fatalf("分词失败: %v", err)
	}
	found := false
	for _, token := range tokens[0] {
		if token == "rs" {
			found = true
		}
		if token == "a" {
			t.Errorf("单字母碎片不该保留：%v", tokens[0])
		}
	}
	if !found {
		t.Errorf("未抽出型号里的拉丁词 rs：%v", tokens[0])
	}
}

// TestLocalTokenizer_Deterministic 覆盖稳定性：
// 同一份文本重复分词必须得到同样的词序，否则关键词会在每次入库时变化，
// 人工改过的关键词也会被下一次入库冲掉。
func TestLocalTokenizer_Deterministic(t *testing.T) {
	texts := []string{"万用表的量程选择，万用表量程与表笔配合使用。"}
	first, err := keyword.NewLocalTokenizer().Tokenize(context.Background(), texts)
	if err != nil {
		t.Fatalf("分词失败: %v", err)
	}
	for i := 0; i < 5; i++ {
		next, err := keyword.NewLocalTokenizer().Tokenize(context.Background(), texts)
		if err != nil {
			t.Fatalf("分词失败: %v", err)
		}
		if strings.Join(first[0], ",") != strings.Join(next[0], ",") {
			t.Fatalf("第 %d 次结果不一致:\n%v\n%v", i, first[0], next[0])
		}
	}
}

// TestLocalTokenizer_ExtractorIntegration 覆盖与抽取器的组合：
// 分词器给的候选词要能被 TF-IDF 排序成「每段最多 N 个」的关键词。
func TestLocalTokenizer_ExtractorIntegration(t *testing.T) {
	texts := []string{
		"数字万用表的量程选择方法：先估计被测电压范围，再选择合适量程。",
		"万用表的电池更换：拆下后盖，注意电池正负极方向，更换后确认量程。",
		"万用表的表笔插孔说明：COM 为公共端，VΩ 为电压电阻测量端。",
	}
	extractor := keyword.NewTFIDF(keyword.NewLocalTokenizer())
	result, err := extractor.Extract(context.Background(), texts, 5)
	if err != nil {
		t.Fatalf("抽取失败: %v", err)
	}
	if len(result) != len(texts) {
		t.Fatalf("返回 %d 组关键词，期望 %d 组", len(result), len(texts))
	}
	for i, words := range result {
		if len(words) == 0 {
			t.Fatalf("第 %d 段没有抽出任何关键词", i+1)
		}
		if len(words) > 5 {
			t.Fatalf("第 %d 段关键词超过上限：%v", i+1, words)
		}
	}
}
