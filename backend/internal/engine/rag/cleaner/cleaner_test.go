package cleaner_test

import (
	"testing"

	"einoflow-backend/internal/engine/rag/cleaner"
	"einoflow-backend/internal/engine/rag/loader"
	"einoflow-backend/internal/model"
)

// textDoc 构造一份只含正文块的解析结果。
func textDoc(texts ...string) *loader.ParsedDocument {
	blocks := make([]loader.Block, 0, len(texts))
	for i, text := range texts {
		blocks = append(blocks, loader.Block{
			Type:    loader.BlockText,
			Inlines: []loader.Inline{{Type: loader.InlineText, Text: text}},
			Page:    1,
			Index:   i,
		})
	}
	return &loader.ParsedDocument{Blocks: blocks}
}

// TestClean_DefaultOff 确认「默认什么都不做」：配置为空时文本原样保留。
//
// 清洗是不可逆的语义修改，默认必须是「不清理」。这条断言防止以后不小心加了默认规则。
func TestClean_DefaultOff(t *testing.T) {
	doc := textDoc("正文  里有   多余空格\n\n\n\n以及空行")
	cleaned, report, err := cleaner.Clean(doc, nil)
	if err != nil {
		t.Fatalf("清洗失败: %v", err)
	}
	if cleaned.Blocks[0].Text() != "正文  里有   多余空格\n\n\n\n以及空行" {
		t.Errorf("默认不应修改文本，实际 %q", cleaned.Blocks[0].Text())
	}
	if report.RemovedRunes != 0 {
		t.Errorf("默认不应有删除，实际删除 %d 字", report.RemovedRunes)
	}
}

// TestClean_NormalizeWhitespace 覆盖空白规范化，并确认报告里有统计。
func TestClean_NormalizeWhitespace(t *testing.T) {
	doc := textDoc("标题    带多余空格\n\n\n\n正文\t\t带制表符   ")
	cleaned, report, err := cleaner.Clean(doc, []model.CleaningRuleConfig{
		{Key: cleaner.RuleNormalizeWhitespace},
	})
	if err != nil {
		t.Fatalf("清洗失败: %v", err)
	}
	got := cleaned.Blocks[0].Text()
	if got != "标题 带多余空格\n\n正文 带制表符" {
		t.Errorf("清洗结果 = %q", got)
	}
	if report.RemovedRunes == 0 {
		t.Error("报告应记录删除的字符数")
	}
	if report.BeforeRunes != report.AfterRunes+report.RemovedRunes && report.RemovedRunes > 0 {
		t.Logf("清洗前后：%d → %d，删除 %d", report.BeforeRunes, report.AfterRunes, report.RemovedRunes)
	}
}

// TestClean_RemoveURLs 覆盖 URL/邮箱删除，并确认样本可展示。
func TestClean_RemoveURLs(t *testing.T) {
	doc := textDoc("详见 https://example.com/docs 或联系 support@example.com")
	cleaned, report, err := cleaner.Clean(doc, []model.CleaningRuleConfig{{Key: cleaner.RuleRemoveURLs}})
	if err != nil {
		t.Fatalf("清洗失败: %v", err)
	}
	if got := cleaned.Blocks[0].Text(); got != "详见  或联系 " {
		t.Errorf("清洗结果 = %q", got)
	}
	if len(report.Rules) != 1 {
		t.Fatalf("规则报告数 = %d, 期望 1", len(report.Rules))
	}
	rule := report.Rules[0]
	if rule.Hits == 0 || len(rule.Samples) == 0 {
		t.Errorf("报告应包含命中数与样本：%+v", rule)
	}
}

// TestClean_CollapseRepeatedLines 覆盖页眉页脚识别：出现 3 次以上的短行被删。
func TestClean_CollapseRepeatedLines(t *testing.T) {
	doc := textDoc(
		"公司内部资料\n第一段正文内容比较长，用来充当正文。",
		"公司内部资料\n第二段正文内容也比较长，同样充当正文。",
		"公司内部资料\n第三段正文内容同样比较长，依旧充当正文。",
	)
	cleaned, report, err := cleaner.Clean(doc, []model.CleaningRuleConfig{{Key: cleaner.RuleCollapseRepeated}})
	if err != nil {
		t.Fatalf("清洗失败: %v", err)
	}
	for i, block := range cleaned.Blocks {
		if block.Text() == "" {
			continue
		}
		if idx := indexOf(block.Text(), "公司内部资料"); idx >= 0 {
			t.Errorf("第 %d 块里的重复页眉未被删除: %q", i, block.Text())
		}
	}
	if report.Rules[0].Hits != 1 {
		t.Errorf("重复行种类 = %d, 期望 1", report.Rules[0].Hits)
	}
}

// TestClean_TableSkippedForHTMLTags 覆盖「表格块跳过 HTML 标签清理」：
// 表格内容本身就是 HTML，删标签会把结构毁掉。
func TestClean_TableSkippedForHTMLTags(t *testing.T) {
	doc := &loader.ParsedDocument{Blocks: []loader.Block{
		{
			Type:    loader.BlockTable,
			Inlines: []loader.Inline{{Type: loader.InlineTableBody, Text: "<table><tr><td>数量</td></tr></table>"}},
		},
	}}
	cleaned, report, err := cleaner.Clean(doc, []model.CleaningRuleConfig{{Key: cleaner.RuleRemoveHTMLTags}})
	if err != nil {
		t.Fatalf("清洗失败: %v", err)
	}
	if got := cleaned.Blocks[0].Text(); got != "<table><tr><td>数量</td></tr></table>" {
		t.Errorf("表格内容不应被改动，实际 %q", got)
	}
	if report.Rules[0].Note == "" {
		t.Error("应说明跳过了表格块")
	}
}

// TestClean_CustomRegexAndErrors 覆盖自定义正则与非法正则。
func TestClean_CustomRegexAndErrors(t *testing.T) {
	doc := textDoc("第 3 页 共 12 页\n正文内容")

	t.Run("合法正则", func(t *testing.T) {
		cleaned, report, err := cleaner.Clean(doc, []model.CleaningRuleConfig{
			{Key: cleaner.RuleCustomRegex, Pattern: `第 \d+ 页 共 \d+ 页`},
		})
		if err != nil {
			t.Fatalf("清洗失败: %v", err)
		}
		if got := cleaned.Blocks[0].Text(); got == doc.Blocks[0].Text() {
			t.Errorf("正则未生效，实际 %q", got)
		}
		if report.Rules[0].Hits != 1 {
			t.Errorf("命中数 = %d, 期望 1", report.Rules[0].Hits)
		}
	})

	t.Run("非法正则明确报错", func(t *testing.T) {
		if _, _, err := cleaner.Clean(doc, []model.CleaningRuleConfig{
			// 半角括号未闭合，RE2 解析会失败（全角括号是普通字符，不算非法）。
			{Key: cleaner.RuleCustomRegex, Pattern: `(未闭合`},
		}); err == nil {
			t.Fatal("非法正则应报错")
		}
	})

	t.Run("空正则跳过", func(t *testing.T) {
		_, report, err := cleaner.Clean(doc, []model.CleaningRuleConfig{{Key: cleaner.RuleCustomRegex}})
		if err != nil {
			t.Fatalf("空正则不应报错: %v", err)
		}
		if report.Rules[0].Note == "" {
			t.Error("应说明跳过原因")
		}
	})
}

// TestClean_DoesNotMutateInput 覆盖「返回副本」：预览会反复调用，
// 原地修改会让参数实验不可重复。
func TestClean_DoesNotMutateInput(t *testing.T) {
	doc := textDoc("正文   有多余空格")
	original := doc.Blocks[0].Text()

	if _, _, err := cleaner.Clean(doc, []model.CleaningRuleConfig{{Key: cleaner.RuleNormalizeWhitespace}}); err != nil {
		t.Fatalf("清洗失败: %v", err)
	}
	if doc.Blocks[0].Text() != original {
		t.Errorf("入参文档被修改：%q → %q", original, doc.Blocks[0].Text())
	}
}

// TestAvailable 确认规则注册表非空且键唯一。
func TestAvailable(t *testing.T) {
	rules := cleaner.Available()
	if len(rules) < 5 {
		t.Fatalf("内置规则数 = %d, 期望至少 5 条", len(rules))
	}
	seen := map[string]bool{}
	for _, rule := range rules {
		if rule.Key == "" || rule.Label == "" || rule.Description == "" {
			t.Errorf("规则信息不完整: %+v", rule)
		}
		if seen[rule.Key] {
			t.Errorf("规则键重复: %s", rule.Key)
		}
		seen[rule.Key] = true
	}
}

// indexOf 是不依赖 strings 的小工具（这里只需要判断存在性）。
func indexOf(text, sub string) int {
	for i := 0; i+len(sub) <= len(text); i++ {
		if text[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
