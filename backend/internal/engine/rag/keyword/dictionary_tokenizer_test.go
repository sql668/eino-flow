package keyword

import (
	"context"
	"strings"
	"testing"
)

func newDictionaryTokenizer(t *testing.T) *DictionaryTokenizer {
	t.Helper()
	tokenizer, err := NewDictionaryTokenizer()
	if err != nil {
		t.Fatalf("初始化词典分词器失败: %v", err)
	}
	return tokenizer
}

func TestDictionaryTokenizer_CompleteWords(t *testing.T) {
	tokenizer := newDictionaryTokenizer(t)
	groups, err := tokenizer.Tokenize(context.Background(), []string{
		"更换电池前请先断开电源，再取下电池盖。",
	})
	if err != nil {
		t.Fatalf("分词失败: %v", err)
	}
	joined := strings.Join(groups[0], " ")
	for _, want := range []string{"断开电源", "电池"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("应抽到完整词 %q，实际: %v", want, groups[0])
		}
	}
	for _, token := range groups[0] {
		if token == "断开电" {
			t.Fatalf("不应出现截断片段「断开电」: %v", groups[0])
		}
	}
}

func TestDictionaryTokenizer_NoLongTruncatedFragments(t *testing.T) {
	tokenizer := newDictionaryTokenizer(t)
	groups, err := tokenizer.Tokenize(context.Background(), []string{
		"交流电压测量前请先断开电源，再打开电池后盖。",
	})
	if err != nil {
		t.Fatalf("分词失败: %v", err)
	}
	for _, token := range groups[0] {
		switch token {
		case "交流电压测", "开电池后盖", "断开电":
			t.Fatalf("不应出现截断片段 %q: %v", token, groups[0])
		}
	}
}

func TestDictionaryTokenizer_ASCIIBoundaryUnchanged(t *testing.T) {
	tokenizer := newDictionaryTokenizer(t)
	groups, err := tokenizer.Tokenize(context.Background(), []string{"型号 RS-12 的万用表"})
	if err != nil {
		t.Fatalf("分词失败: %v", err)
	}
	joined := strings.Join(groups[0], " ")
	// 与旧实现一致：RS-12 按非字母数字切开，得到 rs / 12。
	if !strings.Contains(joined, "rs") || !strings.Contains(joined, "12") {
		t.Fatalf("ASCII 型号边界应保持不变，实际: %v", groups[0])
	}
	if !strings.Contains(joined, "万用表") {
		t.Fatalf("领域词应被抽出，实际: %v", groups[0])
	}
}

func TestDictionaryTokenizer_CustomDomainWords(t *testing.T) {
	tokenizer, err := NewDictionaryTokenizer("超级示波器")
	if err != nil {
		t.Fatalf("初始化失败: %v", err)
	}
	groups, err := tokenizer.Tokenize(context.Background(),
		[]string{"超级示波器可以观察波形，示波器也能观察。"})
	if err != nil {
		t.Fatalf("分词失败: %v", err)
	}
	joined := strings.Join(groups[0], " ")
	if !strings.Contains(joined, "超级示波器") || !strings.Contains(joined, "示波器") {
		t.Fatalf("自定义词与内置领域词都应被抽出: %v", groups[0])
	}
}

func TestDictionaryTokenizer_EmptyAndPunctuation(t *testing.T) {
	tokenizer := newDictionaryTokenizer(t)
	groups, err := tokenizer.Tokenize(context.Background(), []string{"", "，。！？——"})
	if err != nil {
		t.Fatalf("空文本不应报错: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("结果条数 = %d, 期望 2", len(groups))
	}
	for _, group := range groups {
		if len(group) != 0 {
			t.Fatalf("空文本 / 纯符号不应产出 token: %v", group)
		}
	}
}

func TestSuppressCoveredTokens_DropsTruncatedFragment(t *testing.T) {
	groups := [][]string{{"断开电", "断开电源"}}
	out := suppressCoveredTokens(groups)
	for _, token := range out[0] {
		if token == "断开电" {
			t.Fatalf("截断片段应被更长词抑制: %v", out[0])
		}
	}
	if len(out[0]) != 1 || out[0][0] != "断开电源" {
		t.Fatalf("完整词应保留: %v", out[0])
	}
}

func TestNewTokenizer_SelectsImplementation(t *testing.T) {
	texts := []string{"断开电源再操作。断开电源再操作。断开电源再操作。"}
	local, err := NewTokenizer(TokenizerLocal)
	if err != nil {
		t.Fatalf("构造无词典分词器失败: %v", err)
	}
	dictionary, err := NewTokenizer(TokenizerDictionary)
	if err != nil {
		t.Fatalf("构造词典分词器失败: %v", err)
	}

	localTokens, err := local.Tokenize(context.Background(), texts)
	if err != nil {
		t.Fatalf("无词典分词失败: %v", err)
	}
	dictionaryTokens, err := dictionary.Tokenize(context.Background(), texts)
	if err != nil {
		t.Fatalf("词典分词失败: %v", err)
	}
	if strings.Join(localTokens[0], " ") == strings.Join(dictionaryTokens[0], " ") {
		t.Fatalf("切换实现后结果应不同，实际都为: %v", dictionaryTokens[0])
	}
	if _, err := NewTokenizer("unknown"); err == nil {
		t.Fatal("未知分词器应报错，不能静默回退")
	}
}
