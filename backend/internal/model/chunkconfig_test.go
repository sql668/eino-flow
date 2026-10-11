package model_test

import (
	"strings"
	"testing"

	"einoflow-backend/internal/model"
)

// TestNormalizeSeparators_UnescapeAndOrder 覆盖分隔符的规范化：
// 转义还原成真实字符、去重去空、长分隔符排在前面。
func TestNormalizeSeparators_UnescapeAndOrder(t *testing.T) {
	config := model.ChunkConfig{
		Mode: model.ChunkModeSeparator,
		// 顺序刻意写反：短的在前，规范化后应当长的在前。
		Separators: []string{"\\n", "\\n\\n", "  ", "\\n\\n", "###"},
	}.Normalize()

	// 「长在前」按字符数算：### 三个字符，比 \n\n 还长，所以排第一。
	want := []string{"###", "\n\n", "\n"}
	if len(config.Separators) != len(want) {
		t.Fatalf("分隔符 = %q, 期望 %q", config.Separators, want)
	}
	for i := range want {
		if config.Separators[i] != want[i] {
			t.Fatalf("第 %d 个分隔符 = %q, 期望 %q（完整结果 %q）",
				i+1, config.Separators[i], want[i], config.Separators)
		}
	}
	if !config.HasSeparator() {
		t.Error("填了分隔符却判为没有")
	}
}

// TestNormalizeSeparators_LegacySingleField 覆盖老配置：
// legacy 的单值字段要被折叠进列表，且不按逗号拆（老界面里逗号是字面量）。
func TestNormalizeSeparators_LegacySingleField(t *testing.T) {
	config := model.ChunkConfig{Mode: model.ChunkModeSeparator, Separator: "\\n\\n"}.Normalize()
	if len(config.Separators) != 1 || config.Separators[0] != "\n\n" {
		t.Fatalf("老配置折叠结果 = %q, 期望 [\"\\n\\n\"]", config.Separators)
	}
	// 折叠后不再保留单值字段：留着就会出现两个「分隔符」，改一个忘一个。
	if config.Separator != "" {
		t.Errorf("折叠后仍保留 legacy 字段: %q", config.Separator)
	}

	// 老配置里的逗号是字面逗号：不能因为新约定就把它撕成两个分隔符。
	csv := model.ChunkConfig{Mode: model.ChunkModeSeparator, Separator: ","}.Normalize()
	if len(csv.Separators) != 1 || csv.Separators[0] != "," {
		t.Fatalf("老配置用逗号当分隔符被改坏: %q", csv.Separators)
	}
}

// TestNormalizeSeparators_EscapedComma 覆盖「分隔符本身就是逗号」的写法。
func TestNormalizeSeparators_EscapedComma(t *testing.T) {
	config := model.ChunkConfig{
		Mode:       model.ChunkModeSeparator,
		Separators: []string{"\\,", "\\n\\n"},
	}.Normalize()
	found := false
	for _, item := range config.Separators {
		if item == "," {
			found = true
		}
		if strings.Contains(item, "\\") {
			t.Errorf("转义没被还原: %q", item)
		}
	}
	if !found {
		t.Errorf("转义逗号没被还原成字面逗号: %q", config.Separators)
	}
}

// TestNormalizeSeparators_UnknownEscapeKept 覆盖认不出的转义：
// 按原样保留（含反斜杠），否则想用反斜杠当分隔符的人永远配不出来。
func TestNormalizeSeparators_UnknownEscapeKept(t *testing.T) {
	config := model.ChunkConfig{
		Mode:       model.ChunkModeSeparator,
		Separators: []string{`\d+`},
	}.Normalize()
	if len(config.Separators) != 1 || config.Separators[0] != `\d+` {
		t.Fatalf("认不出的转义被改坏: %q", config.Separators)
	}
}

// TestVersion_ChangesWithSeparators 覆盖配置指纹：
// 换分隔符等于换一套切分规则，指纹必须跟着变，否则「这批片段是哪套参数切的」会失真。
func TestVersion_ChangesWithSeparators(t *testing.T) {
	base := model.ChunkConfig{Mode: model.ChunkModeSeparator, Separators: []string{"\n\n"}}.Normalize()
	other := model.ChunkConfig{Mode: model.ChunkModeSeparator, Separators: []string{"###"}}.Normalize()
	if base.Version() == other.Version() {
		t.Error("换了分隔符但指纹没变")
	}
}
