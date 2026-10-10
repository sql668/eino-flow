package chunker_test

import (
	"strings"
	"testing"

	"einoflow-backend/internal/engine/rag/chunker"
	"einoflow-backend/internal/engine/rag/loader"
)

// block 构造一个文本块。
func block(blockType loader.BlockType, text string, page, index int) loader.Block {
	return loader.Block{
		Type:  blockType,
		Page:  page,
		Index: index,
		Inlines: []loader.Inline{
			{Type: loader.InlineText, Text: text},
		},
	}
}

// title 构造一个标题块。
func title(text string, level, page, index int) loader.Block {
	item := block(loader.BlockParagraphTitle, text, page, index)
	item.Level = level
	return item
}

// table 构造一个表格块（内容是 HTML）。
func table(html string, page, index int) loader.Block {
	return loader.Block{
		Type:  loader.BlockTable,
		Page:  page,
		Index: index,
		Inlines: []loader.Inline{
			{Type: loader.InlineTableBody, Text: html},
		},
	}
}

func docOf(blocks ...loader.Block) *loader.ParsedDocument {
	return &loader.ParsedDocument{
		Blocks: blocks,
		Meta:   loader.DocumentMeta{Parser: "mineru", PageCount: 1},
	}
}

func TestSplit_DropsNoiseBlocks(t *testing.T) {
	doc := docOf(
		block(loader.BlockHeader, "页眉内容", 1, 0),
		block(loader.BlockText, "正文内容足够长，应当被保留下来形成片段。", 1, 1),
		block(loader.BlockFooter, "页脚内容", 1, 2),
		block(loader.BlockPageNumber, "1", 1, 3),
	)
	chunks := chunker.Split(doc, chunker.Options{MaxRunes: 100})
	if len(chunks) != 1 {
		t.Fatalf("片段数 = %d, 期望 1（页眉/页脚/页码应被丢弃）", len(chunks))
	}
	if strings.Contains(chunks[0].Text, "页眉") || strings.Contains(chunks[0].Text, "页脚") {
		t.Errorf("噪声块未被丢弃: %q", chunks[0].Text)
	}
}

func TestSplit_KeepNoiseOption(t *testing.T) {
	doc := docOf(
		block(loader.BlockHeader, "页眉", 1, 0),
		block(loader.BlockText, "正文内容。", 1, 1),
	)
	chunks := chunker.Split(doc, chunker.Options{KeepNoise: true, MinRunes: 1})
	if len(chunks) != 1 {
		t.Fatalf("片段数 = %d, 期望 1", len(chunks))
	}
	if !strings.Contains(chunks[0].Text, "页眉") {
		t.Errorf("KeepNoise 为 true 时应保留页眉: %q", chunks[0].Text)
	}
}

func TestSplit_TitleStartsNewChunkAndAddsBreadcrumb(t *testing.T) {
	longText := strings.Repeat("内容", 60) // 120 字符，超过 MaxRunes 会分片
	doc := docOf(
		title("第一章", 1, 1, 0),
		block(loader.BlockText, longText, 1, 1),
		title("1.1 概述", 2, 1, 2),
		block(loader.BlockText, longText, 1, 3),
	)
	chunks := chunker.Split(doc, chunker.Options{MaxRunes: 2000})
	if len(chunks) != 2 {
		t.Fatalf("片段数 = %d, 期望 2（每个标题开启新片段）", len(chunks))
	}

	if got := chunks[0].TitlePath; len(got) != 1 || got[0] != "第一章" {
		t.Errorf("第 1 片标题路径 = %v", got)
	}
	if got := chunks[1].TitlePath; len(got) != 2 || got[1] != "1.1 概述" {
		t.Errorf("第 2 片标题路径 = %v, 期望两级", got)
	}

	// 面包屑必须出现在 Content（用于嵌入）里，便于脱离上下文后仍保留归属
	if !strings.Contains(chunks[1].Content, "第一章 > 1.1 概述") {
		t.Errorf("Content 缺少标题面包屑: %q", chunks[1].Content)
	}
	// Text 保持原始内容，不带面包屑
	if strings.Contains(chunks[1].Text, "【") {
		t.Errorf("Text 不应包含面包屑: %q", chunks[1].Text)
	}
	if chunks[1].Index != 1 {
		t.Errorf("片段序号 = %d, 期望 1", chunks[1].Index)
	}
}

func TestSplit_TitleLevelsNeedNotBeContiguous(t *testing.T) {
	// 实测 MinerU 对一级标题给的是 level=2，因此不能假设层级从 1 开始连续。
	doc := docOf(
		title("顶层标题", 2, 1, 0),
		block(loader.BlockText, "顶层正文。", 1, 1),
		title("子标题", 3, 1, 2),
		block(loader.BlockText, "子级正文。", 1, 3),
		title("另一个顶层标题", 2, 1, 4),
		block(loader.BlockText, "另一段正文。", 1, 5),
	)
	chunks := chunker.Split(doc, chunker.Options{MaxRunes: 2000, MinRunes: 1})
	if len(chunks) != 3 {
		t.Fatalf("片段数 = %d, 期望 3", len(chunks))
	}
	if got := chunks[1].TitlePath; len(got) != 2 || got[1] != "子标题" {
		t.Errorf("第 2 片标题路径 = %v, 期望包含两级", got)
	}
	// 回到同层级标题时，子标题必须被弹出
	if got := chunks[2].TitlePath; len(got) != 1 || got[0] != "另一个顶层标题" {
		t.Errorf("第 3 片标题路径 = %v, 期望只剩一级", got)
	}
}

func TestSplit_TableKeptWhole(t *testing.T) {
	html := "<table><tr><td>" + strings.Repeat("单元格", 400) + "</td></tr></table>"
	doc := docOf(
		block(loader.BlockText, "表格前的说明文字，长度足够形成片段。", 1, 0),
		table(html, 1, 1),
	)
	chunks := chunker.Split(doc, chunker.Options{MaxRunes: 100})
	if len(chunks) != 2 {
		t.Fatalf("片段数 = %d, 期望 2", len(chunks))
	}

	var tableChunk *chunker.Chunk
	for i := range chunks {
		if chunks[i].Kind == chunker.KindTable {
			tableChunk = &chunks[i]
		}
	}
	if tableChunk == nil {
		t.Fatal("未产出表格片段")
	}
	// 即便远超 MaxRunes，表格也必须完整保留，不能被切开
	if tableChunk.Text != html {
		t.Errorf("表格被改动或切开了（长度 %d, 期望 %d）", len(tableChunk.Text), len(html))
	}
}

func TestSplit_SplitsByMaxRunes(t *testing.T) {
	doc := docOf(
		block(loader.BlockText, strings.Repeat("甲", 100), 1, 0),
		block(loader.BlockText, strings.Repeat("乙", 100), 1, 1),
		block(loader.BlockText, strings.Repeat("丙", 100), 1, 2),
		block(loader.BlockText, strings.Repeat("丁", 100), 1, 3),
	)
	chunks := chunker.Split(doc, chunker.Options{MaxRunes: 250, MinRunes: 1})
	if len(chunks) < 2 {
		t.Fatalf("片段数 = %d, 超过 MaxRunes 应被切开", len(chunks))
	}
	// 序号必须连续，便于定位与重建
	for i, item := range chunks {
		if item.Index != i {
			t.Errorf("第 %d 片序号 = %d", i, item.Index)
		}
	}
}

func TestSplit_MergesShortTail(t *testing.T) {
	doc := docOf(
		block(loader.BlockText, strings.Repeat("正文", 100), 1, 0),
		block(loader.BlockText, "很短。", 1, 1),
	)
	chunks := chunker.Split(doc, chunker.Options{MaxRunes: 5000, MinRunes: 40})
	if len(chunks) != 1 {
		t.Fatalf("片段数 = %d, 过短的尾部应并入上一片段", len(chunks))
	}
	if !strings.Contains(chunks[0].Text, "很短。") {
		t.Errorf("尾部内容被丢弃: %q", chunks[0].Text)
	}
	if chunks[0].BlockCount != 2 {
		t.Errorf("BlockCount = %d, 期望 2", chunks[0].BlockCount)
	}
}

func TestSplit_KeepsFirstChunkEvenIfShort(t *testing.T) {
	// 文档首段很短时不能丢弃，否则内容会丢。
	doc := docOf(block(loader.BlockText, "短。", 1, 0))
	chunks := chunker.Split(doc, chunker.Options{MaxRunes: 5000, MinRunes: 40})
	if len(chunks) != 1 {
		t.Fatalf("片段数 = %d, 首个片段不应被丢弃", len(chunks))
	}
}

func TestSplit_RecordsPageAndLocator(t *testing.T) {
	doc := docOf(
		block(loader.BlockText, "第二页的正文内容，长度足够。", 2, 5),
	)
	chunks := chunker.Split(doc, chunker.Options{MaxRunes: 5000, MinRunes: 1})
	if len(chunks) != 1 {
		t.Fatalf("片段数 = %d", len(chunks))
	}
	if chunks[0].Page != 2 {
		t.Errorf("Page = %d, 期望 2", chunks[0].Page)
	}
	if chunks[0].Locator != "page:2/block:5" {
		t.Errorf("Locator = %q", chunks[0].Locator)
	}
}

func TestSplit_EmptyInputs(t *testing.T) {
	if got := chunker.Split(nil, chunker.Options{}); got != nil {
		t.Errorf("nil 文档应返回 nil，实际 %v", got)
	}
	if got := chunker.Split(docOf(), chunker.Options{}); len(got) != 0 {
		t.Errorf("空文档应返回空片段，实际 %v", got)
	}
	// 全是噪声块的文档不应产出片段
	doc := docOf(block(loader.BlockHeader, "页眉", 1, 0), block(loader.BlockFooter, "页脚", 1, 1))
	if got := chunker.Split(doc, chunker.Options{}); len(got) != 0 {
		t.Errorf("仅含噪声时不应产出片段，实际 %v", got)
	}
}
