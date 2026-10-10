package plaintext_test

import (
	"context"
	"testing"

	"einoflow-backend/internal/engine/rag/loader"
	"einoflow-backend/internal/engine/rag/loader/plaintext"
)

func TestSupports(t *testing.T) {
	item := plaintext.New()
	for _, fileType := range []string{"txt", "md", "markdown", "log"} {
		if !item.Supports(fileType) {
			t.Errorf("应支持 %s", fileType)
		}
	}
	for _, fileType := range []string{"pdf", "docx", "png"} {
		if item.Supports(fileType) {
			t.Errorf("不应支持 %s", fileType)
		}
	}
}

func TestLoad_MarkdownStructure(t *testing.T) {
	content := "# 一级标题\n\n正文第一段\n跨行内容\n\n## 二级标题\n\n正文第二段\n"
	doc, err := plaintext.New().Load(context.Background(), loader.LoadRequest{
		Filename: "手册.md",
		Content:  []byte(content),
	})
	if err != nil {
		t.Fatalf("Load 返回错误: %v", err)
	}

	type want struct {
		blockType loader.BlockType
		level     int
		text      string
	}
	expects := []want{
		{loader.BlockParagraphTitle, 1, "一级标题"},
		{loader.BlockText, 0, "正文第一段\n跨行内容"},
		{loader.BlockParagraphTitle, 2, "二级标题"},
		{loader.BlockText, 0, "正文第二段"},
	}

	if len(doc.Blocks) != len(expects) {
		t.Fatalf("块数 = %d, 期望 %d（实际: %+v）", len(doc.Blocks), len(expects), doc.Blocks)
	}
	for i, expect := range expects {
		got := doc.Blocks[i]
		if got.Type != expect.blockType || got.Level != expect.level || got.Text() != expect.text {
			t.Errorf("第 %d 块 = {type:%s level:%d text:%q}, 期望 {type:%s level:%d text:%q}",
				i, got.Type, got.Level, got.Text(), expect.blockType, expect.level, expect.text)
		}
		if got.Index != i {
			t.Errorf("第 %d 块的 Index = %d, 期望按顺序为 %d", i, got.Index, i)
		}
		if got.Page != 1 {
			t.Errorf("纯文本应记录为第 1 页, 实际 %d", got.Page)
		}
	}
}

func TestLoad_Meta(t *testing.T) {
	doc, err := plaintext.New().Load(context.Background(), loader.LoadRequest{
		Filename: "/tmp/说明.TXT",
		Content:  []byte("内容"),
	})
	if err != nil {
		t.Fatalf("Load 返回错误: %v", err)
	}
	if doc.Meta.Parser != "plaintext" {
		t.Errorf("Meta.Parser = %q", doc.Meta.Parser)
	}
	if doc.Meta.FileType != "txt" {
		t.Errorf("Meta.FileType = %q, 期望小写 txt", doc.Meta.FileType)
	}
	if doc.Meta.SourceName != "/tmp/说明.TXT" {
		t.Errorf("Meta.SourceName = %q", doc.Meta.SourceName)
	}
}

func TestLoad_EdgeCases(t *testing.T) {
	cases := []struct {
		name        string
		content     string
		wantBlocks  int
		expectFirst loader.BlockType
	}{
		{"空内容", "", 0, ""},
		{"只有空白", " \n\n\t\n", 0, ""},
		{"CRLF 换行", "# 标题\r\n\r\n正文\r\n", 2, loader.BlockParagraphTitle},
		{"无空行的连续文本", "第一行\n第二行\n第三行", 1, loader.BlockText},
		{"正文中的井号不是标题", "价格 #1 说明\n继续", 1, loader.BlockText},
		{"七个井号不是标题", "####### 太深了", 1, loader.BlockText},
		// 单独的 # 没有标题文本，不成标题，按普通正文处理；
		// 它后面的空行会把它与下一段分开，因此是两块。
		{"井号后无标题文本按正文处理", "#\n\n正文", 2, loader.BlockText},
		// 标题行紧跟在正文之后（中间没有空行）时，应当切断正文并另起标题块。
		{"正文后紧跟标题不需要空行", "正文\n# 标题", 2, loader.BlockText},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := plaintext.New().Load(context.Background(), loader.LoadRequest{
				Filename: "a.txt",
				Content:  []byte(tc.content),
			})
			if err != nil {
				t.Fatalf("Load 返回错误: %v", err)
			}
			if len(doc.Blocks) != tc.wantBlocks {
				t.Fatalf("块数 = %d, 期望 %d（实际: %+v）", len(doc.Blocks), tc.wantBlocks, doc.Blocks)
			}
			if tc.wantBlocks > 0 && doc.Blocks[0].Type != tc.expectFirst {
				t.Errorf("首块类型 = %s, 期望 %s", doc.Blocks[0].Type, tc.expectFirst)
			}
		})
	}
}

func TestLoad_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := plaintext.New().Load(ctx, loader.LoadRequest{Filename: "a.txt", Content: []byte("x")}); err == nil {
		t.Fatal("已取消的 context 应返回错误")
	}
}
