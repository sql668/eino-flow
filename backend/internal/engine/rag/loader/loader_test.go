package loader_test

import (
	"context"
	"errors"
	"testing"

	"einoflow-backend/internal/engine/rag/loader"
)

// stubLoader 是测试用的假解析器，用于验证按类型选择的逻辑。
type stubLoader struct {
	name string
	exts []string
}

func (s stubLoader) Name() string { return s.name }

func (s stubLoader) Supports(fileType string) bool {
	for _, ext := range s.exts {
		if ext == fileType {
			return true
		}
	}
	return false
}

func (s stubLoader) Load(context.Context, loader.LoadRequest) (*loader.ParsedDocument, error) {
	return &loader.ParsedDocument{Meta: loader.DocumentMeta{Parser: s.name}}, nil
}

func TestResolve(t *testing.T) {
	primary := stubLoader{name: "mineru", exts: []string{"pdf", "docx"}}
	fallback := stubLoader{name: "plaintext", exts: []string{"txt", "md"}}
	resolver := loader.NewResolver(primary, fallback)

	cases := []struct {
		name     string
		filename string
		want     string
	}{
		{"pdf 命中主解析器", "手册.pdf", "mineru"},
		{"docx 命中主解析器", "报告.DOCX", "mineru"}, // 扩展名大小写不应影响匹配
		{"txt 命中兜底解析器", "说明.txt", "plaintext"},
		{"带路径的文件名", "/tmp/a/b/readme.md", "plaintext"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolver.Resolve(tc.filename)
			if err != nil {
				t.Fatalf("Resolve(%q) 返回错误: %v", tc.filename, err)
			}
			if got.Name() != tc.want {
				t.Fatalf("Resolve(%q) = %s, 期望 %s", tc.filename, got.Name(), tc.want)
			}
		})
	}
}

func TestResolve_NoLoader(t *testing.T) {
	resolver := loader.NewResolver(stubLoader{name: "plaintext", exts: []string{"txt"}})

	for _, filename := range []string{"未知.xyz", "无扩展名"} {
		_, err := resolver.Resolve(filename)
		if !errors.Is(err, loader.ErrNoLoader) {
			t.Fatalf("Resolve(%q) 期望 ErrNoLoader, 实际 %v", filename, err)
		}
	}
}

func TestResolve_PreferEarlierLoader(t *testing.T) {
	// 两个解析器都支持 txt 时，应当选注册在前面的那个。
	resolver := loader.NewResolver(
		stubLoader{name: "first", exts: []string{"txt"}},
		stubLoader{name: "second", exts: []string{"txt"}},
	)
	got, err := resolver.Resolve("a.txt")
	if err != nil {
		t.Fatalf("Resolve 返回错误: %v", err)
	}
	if got.Name() != "first" {
		t.Fatalf("期望优先选 first, 实际 %s", got.Name())
	}
}

func TestLoadRequest_FileType(t *testing.T) {
	cases := map[string]string{
		"a.PDF":         "pdf",
		"/x/y/手册.Docx":  "docx",
		"noext":         "",
		"double.tar.gz": "gz",
	}
	for filename, want := range cases {
		req := loader.LoadRequest{Filename: filename}
		if got := req.FileType(); got != want {
			t.Errorf("FileType(%q) = %q, 期望 %q", filename, got, want)
		}
	}
}

func TestLoadRequest_Option(t *testing.T) {
	var empty loader.LoadRequest
	if got := empty.Option("tier"); got != "" {
		t.Errorf("Options 为 nil 时应返回空串, 实际 %q", got)
	}

	req := loader.LoadRequest{Options: map[string]string{"tier": "basic"}}
	if got := req.Option("tier"); got != "basic" {
		t.Errorf("Option(tier) = %q, 期望 basic", got)
	}
}

func TestBlock_Text(t *testing.T) {
	block := loader.Block{
		Inlines: []loader.Inline{
			{Type: loader.InlineText, Text: "第一段"},
			{Type: loader.InlineTableBody, Text: "<table></table>"},
			{Type: loader.InlineImageBody, Text: ""}, // 空内容不产生多余换行
		},
	}
	want := "第一段\n<table></table>"
	if got := block.Text(); got != want {
		t.Fatalf("Text() = %q, 期望 %q", got, want)
	}
	if block.IsEmpty() {
		t.Fatal("有内容的块不应判定为空")
	}
}

func TestBlock_Locator(t *testing.T) {
	block := loader.Block{Page: 3, Index: 12}
	if got := block.Locator(); got != "page:3/block:12" {
		t.Fatalf("Locator() = %q", got)
	}
}

func TestBlockType_Classification(t *testing.T) {
	if !loader.BlockParagraphTitle.IsTitle() {
		t.Error("paragraph_title 应判定为标题")
	}
	if !loader.BlockHeader.IsNoise() || !loader.BlockFooter.IsNoise() || !loader.BlockPageNumber.IsNoise() {
		t.Error("页眉/页脚/页码应判定为噪声")
	}
	if loader.BlockText.IsNoise() || loader.BlockTable.IsNoise() {
		t.Error("正文与表格不应判定为噪声")
	}
	if !loader.BlockEquation.IsKnown() {
		t.Error("equation 已在常量中定义，IsKnown 应为 true")
	}
	// 上游可能新增我们还不认识的块类型（例如 chart）。这类值必须落入「未知」，
	// 而不是被误判成已知类型——否则解析器升级带来的变化会被静默吞掉。
	if loader.BlockType("chart").IsKnown() {
		t.Error("未定义的块类型 IsKnown 应为 false")
	}
}
