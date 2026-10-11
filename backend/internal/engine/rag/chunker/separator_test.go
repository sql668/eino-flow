package chunker_test

import (
	"strings"
	"testing"

	"einoflow-backend/internal/engine/rag/chunker"
	"einoflow-backend/internal/engine/rag/loader"
	"einoflow-backend/internal/model"
)

// 说明：分隔符模式是「先按分隔符拆，再按长度打包」（见包注释），
// 所以这些用例把 MaxRunes 调小，让「一条记录一段」这个结果真的显现出来；
// 用默认的 800 时，几条几十字的记录会被打包进同一个片段，切分效果看不出来。

// TestSplit_MultipleSeparators 覆盖多个分隔符：
// 任意一个出现都要切开——两条记录拼起来必须超过上限，因此每条记录自成一段。
func TestSplit_MultipleSeparators(t *testing.T) {
	doc := docOf(block(loader.BlockText,
		"第一条记录：电压测量的操作步骤与注意事项说明，这一段刻意写长一些，确保它超过最小长度限制并独立成段。\n"+
			"第二条记录：电流测量的操作步骤与注意事项说明，这一段同样写长一些，确保它也与前一条分开成段。\n----\n"+
			"第三条记录：电阻测量的操作步骤与注意事项说明，这一段继续写长一些，确保它不被合并到前面去。", 1, 0))

	res, err := chunker.Split(doc, chunker.Options{
		Mode:       model.ChunkModeSeparator,
		Separators: []string{"\n----\n", "\n"},
		MaxRunes:   70,
	})
	if err != nil {
		t.Fatalf("切分失败: %v", err)
	}
	if len(res.Chunks) != 3 {
		t.Fatalf("片段数 = %d, 期望 3（题目里给了两个分隔符）", len(res.Chunks))
	}
	if !strings.HasPrefix(res.Chunks[2].Content, "第三条记录") {
		t.Errorf("第三个片段 = %q, 期望以「第三条记录」开头", res.Chunks[2].Content)
	}
	// 分隔线所在的片段不该把分隔线本身当内容带进来。
	for _, chunk := range res.Chunks {
		if strings.Contains(chunk.Content, "----") {
			t.Errorf("片段里残留了分隔符: %q", chunk.Content)
		}
	}
}

// TestSplit_SeparatorsDoNotCutInsideRecord 覆盖「不该切的地方不切」：
// 记录内部的单个换行不是分隔符时，内容要保持完整。
func TestSplit_SeparatorsDoNotCutInsideRecord(t *testing.T) {
	doc := docOf(block(loader.BlockText,
		"第一段正文：这一段用来验证按空行切分，所以刻意写长一些以确保它能独立成段而不被合并掉。\n\n"+
			"第二段正文：这段内部有换行\n但不该被切开，因为它不在分隔符里，所以整段必须完整保留下来。\n\n"+
			"第三段正文：这一段用来占位，保证前两段不会因为太短而被合并进来，也不至于被长度规则切开。", 1, 0))

	res, err := chunker.Split(doc, chunker.Options{
		Mode:       model.ChunkModeSeparator,
		Separators: []string{"\n\n"},
		MaxRunes:   70,
	})
	if err != nil {
		t.Fatalf("切分失败: %v", err)
	}
	if len(res.Chunks) != 3 {
		t.Fatalf("片段数 = %d, 期望 3", len(res.Chunks))
	}
	if !strings.Contains(res.Chunks[1].Content, "但不该被切开") {
		t.Errorf("第二段 = %q, 期望内部的单换行不被当作分隔符", res.Chunks[1].Content)
	}
}

// TestSplit_SeparatorKeepsLocation 覆盖溯源：
// 按分隔符拆出来的片段仍要保留页码，否则「这段来自哪一页」就答不上来。
func TestSplit_SeparatorKeepsLocation(t *testing.T) {
	doc := docOf(block(loader.BlockText,
		"记录一：这里是足够长的一段内容，用来保证它不会被最小长度规则合并掉，所以写得长一点。\n"+
			"记录二：这里同样是一段够长的内容，用来验证页码会被保留下来，所以也写得足够长一些。", 7, 3))

	res, err := chunker.Split(doc, chunker.Options{
		Mode:       model.ChunkModeSeparator,
		Separators: []string{"\n"},
		MaxRunes:   70,
	})
	if err != nil {
		t.Fatalf("切分失败: %v", err)
	}
	if len(res.Chunks) != 2 {
		t.Fatalf("片段数 = %d, 期望 2", len(res.Chunks))
	}
	for _, chunk := range res.Chunks {
		if chunk.Page != 7 {
			t.Errorf("片段页码 = %d, 期望 7", chunk.Page)
		}
	}
}
