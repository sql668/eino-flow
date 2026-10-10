package mineru

import (
	"strings"
	"testing"

	"einoflow-backend/internal/engine/rag/loader"
)

// fixture 是一份精简的 middle_json，字段取自 MinerU 4.0.11 的真实输出结构。
// 刻意写小：真实产物有几千行，测试只需要覆盖映射规则。
const fixture = `{
  "schema": "docvortex.middle",
  "schema_version": "2.0",
  "is_full_document": true,
  "metadata": {
    "file_suffix": "pdf",
    "producer": {"name": "mineru", "version": "4.0.11"},
    "document": {"page_count": 2}
  },
  "extensions": {"mineru": {"tier": "basic", "parse_mode": "ocr"}},
  "pages": [
    {
      "page_idx": 0,
      "blocks": [
        {"type": "paragraph_title", "index": 0, "level": 2, "bbox": [0.09, 0.09, 0.18, 0.11],
         "content": [{"type": "text", "index": 0, "bbox": [0.09, 0.09, 0.18, 0.11], "content": "个人概况"}]},
        {"type": "text", "index": 1, "bbox": [0.09, 0.2, 0.9, 0.3],
         "content": [{"type": "text", "index": 0, "bbox": [], "content": "第一段正文"}]},
        {"type": "image", "index": 2, "bbox": [0.5, 0.5, 0.9, 0.9],
         "content": [{"type": "image_body", "index": 0, "bbox": [], "content": ""}]}
      ]
    },
    {
      "page_idx": 1,
      "blocks": [
        {"type": "table", "index": 0, "bbox": [0.1, 0.1, 0.9, 0.4],
         "content": [
           {"type": "table_body", "index": 0, "bbox": [], "content": "<table><tr><td>a</td></tr></table>"},
           {"type": "table_footnote", "index": 1, "bbox": [],
            "content": [{"type": "text", "content": "注：单位伏特"}]}
         ]}
      ]
    }
  ]
}`

func TestToDocument_Mapping(t *testing.T) {
	doc, err := toDocument(
		&ParseResult{MiddleJSON: []byte(fixture), Tier: "basic", ParserVersion: "4.0.11"},
		loadRequestInfo{Filename: "手册.pdf", FileType: "pdf"},
	)
	if err != nil {
		t.Fatalf("toDocument 返回错误: %v", err)
	}

	if len(doc.Blocks) != 4 {
		t.Fatalf("块数 = %d, 期望 4", len(doc.Blocks))
	}

	// 元信息
	if doc.Meta.Parser != "mineru" || doc.Meta.ParserVersion != "4.0.11" {
		t.Errorf("解析器元信息错误: %+v", doc.Meta)
	}
	if doc.Meta.Tier != "basic" || doc.Meta.ParseMode != "ocr" {
		t.Errorf("档位/解析模式错误: tier=%q mode=%q", doc.Meta.Tier, doc.Meta.ParseMode)
	}
	if doc.Meta.PageCount != 2 || doc.Meta.FileType != "pdf" || doc.Meta.SourceName != "手册.pdf" {
		t.Errorf("文档元信息错误: %+v", doc.Meta)
	}
	if len(doc.Warnings) != 0 {
		t.Errorf("标准产物不应产生警告, 实际: %v", doc.Warnings)
	}

	// 页码：MinerU 的 page_idx 是 0 基，对外应为 1 基
	if doc.Blocks[0].Page != 1 || doc.Blocks[1].Page != 1 {
		t.Errorf("第 1 页的块页码应为 1, 实际 %d / %d", doc.Blocks[0].Page, doc.Blocks[1].Page)
	}
	if doc.Blocks[3].Page != 2 {
		t.Errorf("第 2 页的块页码应为 2, 实际 %d", doc.Blocks[3].Page)
	}

	// 标题与层级
	if doc.Blocks[0].Type != loader.BlockParagraphTitle || doc.Blocks[0].Level != 2 {
		t.Errorf("标题块映射错误: %+v", doc.Blocks[0])
	}
	if doc.Blocks[0].Text() != "个人概况" {
		t.Errorf("标题文本 = %q", doc.Blocks[0].Text())
	}

	// 表格：表格主体是 HTML，必须原样保留（切片时靠它保持行列结构）
	table := doc.Blocks[3]
	if table.Type != loader.BlockTable || len(table.Inlines) != 2 {
		t.Fatalf("表格块映射错误: %+v", table)
	}
	if !strings.Contains(table.Inlines[0].Text, "<table>") {
		t.Errorf("表格主体应为 HTML, 实际 %q", table.Inlines[0].Text)
	}
	if table.Inlines[1].Type != loader.InlineTableFootnote {
		t.Errorf("表注类型 = %s", table.Inlines[1].Type)
	}
	// 表注的 content 在真实产物里是数组形态，必须被扁平化成文本而不是丢掉
	if got := table.Inlines[1].Text; got != "注：单位伏特" {
		t.Errorf("表注文本 = %q, 期望扁平化后的 注：单位伏特", got)
	}

	// 空内容块（图片）必须保留：它带着页码与坐标，是「这里有图」的唯一线索
	image := doc.Blocks[2]
	if image.Type != loader.BlockImage || !image.IsEmpty() {
		t.Errorf("图片块应保留且内容为空: %+v", image)
	}

	// 定位串可用于溯源
	if got := doc.Blocks[3].Locator(); got != "page:2/block:0" {
		t.Errorf("Locator() = %q", got)
	}
}

func TestToDocument_SchemaMismatch(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"schema 不匹配", strings.Replace(fixture, "docvortex.middle", "something.else", 1)},
		{"主版本不兼容", strings.Replace(fixture, `"schema_version": "2.0"`, `"schema_version": "3.0"`, 1)},
		{"版本号无法解析", strings.Replace(fixture, `"schema_version": "2.0"`, `"schema_version": "abc"`, 1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := toDocument(
				&ParseResult{MiddleJSON: []byte(tc.body)},
				loadRequestInfo{Filename: "a.pdf", FileType: "pdf"},
			)
			if err == nil {
				t.Fatal("协议不匹配时应返回错误，而不是猜着解析")
			}
		})
	}
}

func TestToDocument_MinorVersionAndPartialDocWarnings(t *testing.T) {
	body := strings.Replace(fixture, `"schema_version": "2.0"`, `"schema_version": "2.7"`, 1)
	body = strings.Replace(body, `"is_full_document": true`, `"is_full_document": false`, 1)

	doc, err := toDocument(&ParseResult{MiddleJSON: []byte(body)},
		loadRequestInfo{Filename: "a.pdf", FileType: "pdf"})
	if err != nil {
		t.Fatalf("小版本差异不应导致失败: %v", err)
	}
	joined := strings.Join(doc.Warnings, " | ")
	if !strings.Contains(joined, "小版本") {
		t.Errorf("应提示小版本差异, 实际: %v", doc.Warnings)
	}
	if !strings.Contains(joined, "部分页") {
		t.Errorf("应提示只解析了部分页, 实际: %v", doc.Warnings)
	}
}

func TestToDocument_UnknownTypesPassThrough(t *testing.T) {
	body := strings.Replace(fixture, `"type": "image"`, `"type": "chart"`, 1)
	body = strings.Replace(body, `"type": "table_footnote"`, `"type": "future_inline"`, 1)

	doc, err := toDocument(&ParseResult{MiddleJSON: []byte(body)},
		loadRequestInfo{Filename: "a.pdf", FileType: "pdf"})
	if err != nil {
		t.Fatalf("未知类型不应导致失败: %v", err)
	}

	// 未知类型要原样透传，不能被丢弃或改名
	if doc.Blocks[2].Type != loader.BlockType("chart") {
		t.Errorf("未知块类型应原样保留, 实际 %q", doc.Blocks[2].Type)
	}
	if doc.Blocks[2].Type.IsKnown() {
		t.Error("chart 不应被判定为已知类型")
	}
	if got := doc.Blocks[3].Inlines[1].Type; got != loader.InlineType("future_inline") {
		t.Errorf("未知内联类型应原样保留, 实际 %q", got)
	}

	joined := strings.Join(doc.Warnings, " | ")
	if !strings.Contains(joined, "chart") {
		t.Errorf("应提示未识别的块类型, 实际: %v", doc.Warnings)
	}
	if !strings.Contains(joined, "future_inline") {
		t.Errorf("应提示未识别的内联类型, 实际: %v", doc.Warnings)
	}
}

func TestToDocument_NoBlocks(t *testing.T) {
	empty := `{"schema":"docvortex.middle","schema_version":"2.0","pages":[]}`
	if _, err := toDocument(&ParseResult{MiddleJSON: []byte(empty)},
		loadRequestInfo{Filename: "a.pdf", FileType: "pdf"}); err == nil {
		t.Fatal("没有任何内容块时应返回错误，避免写入空文档")
	}
}

func TestToDocument_InvalidJSON(t *testing.T) {
	if _, err := toDocument(&ParseResult{MiddleJSON: []byte("not json")},
		loadRequestInfo{Filename: "a.pdf", FileType: "pdf"}); err == nil {
		t.Fatal("非法 JSON 应返回错误")
	}
}

// TestTextContent_Shapes 覆盖内联 content 字段的多种真实形态。
// 这些形态是在对真实服务跑集成测试时才暴露出来的：fixture 里写成字符串会掩盖问题。
func TestTextContent_Shapes(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"字符串", `"普通文本"`, "普通文本"},
		{"空字符串", `""`, ""},
		{"null", `null`, ""},
		// table_footnote / image_footnote / hyperlink 在真实产物里都是这种数组形态
		{"单元素数组", `[{"type":"text","content":"图注内容"}]`, "图注内容"},
		{"多元素数组拼接", `[{"type":"text","content":"A"},{"type":"text","content":"B"}]`, "AB"},
		{"数组元素带 styles", `[{"type":"text","content":"链接","styles":["bold"]}]`, "链接"},
		{"嵌套数组", `[{"type":"text","content":[{"type":"text","content":"深层"}]}]`, "深层"},
		{"空数组", `[]`, ""},
		{"数组内空文本被忽略", `[{"type":"text","content":"  "},{"type":"text","content":"有效"}]`, "有效"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got textContent
			if err := got.UnmarshalJSON([]byte(tc.raw)); err != nil {
				t.Fatalf("UnmarshalJSON(%s) 返回错误: %v", tc.raw, err)
			}
			if got.Text != tc.want {
				t.Errorf("Text = %q, 期望 %q", got.Text, tc.want)
			}
		})
	}
}
