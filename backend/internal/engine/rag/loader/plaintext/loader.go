// Package plaintext 提供纯文本与 Markdown 的本地解析器。
//
// 它承担两个角色：
//   - 常规路径：txt / md 这类不需要版面分析的文档本地直读最快，也不必占用外部解析服务；
//   - **降级路径**：外部解析服务不可用（会挂、会超时）时，纯文本类文档仍能入库。
//     没有降级路径的话，解析服务一抖动整条入库链就断了。
package plaintext

import (
	"context"
	"strings"

	"einoflow-backend/internal/engine/rag/loader"
)

// Loader 是纯文本 / Markdown 解析器，无外部依赖。
type Loader struct{}

// New 构造解析器。
func New() Loader { return Loader{} }

// Name 实现 loader.Loader。
func (Loader) Name() string { return "plaintext" }

// supportedTypes 是支持的扩展名（小写、不含点）。
var supportedTypes = map[string]bool{
	"txt": true, "text": true, "log": true,
	"md": true, "markdown": true, "mdx": true,
}

// Supports 实现 loader.Loader。
func (Loader) Supports(fileType string) bool { return supportedTypes[fileType] }

// Load 实现 loader.Loader。
//
// 按行扫描而不是整段塞进一个块：
//   - `#`~`######` 开头的独立行视为标题块，层级取井号数量；
//   - 其余连续非空行合并为一个正文块。
//
// 这样纯文本路径也产出结构化块，下游切片逻辑不必为它写特例——
// 它只关心 Block，不关心文档来自哪种解析器。
func (Loader) Load(ctx context.Context, req loader.LoadRequest) (*loader.ParsedDocument, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	doc := &loader.ParsedDocument{
		Meta: loader.DocumentMeta{
			Parser:     "plaintext",
			PageCount:  1, // 纯文本没有页的概念，统一按 1 页记录
			SourceName: req.Filename,
			FileType:   req.FileType(),
		},
	}

	index := 0
	appendBlock := func(blockType loader.BlockType, content string, level int) {
		if strings.TrimSpace(content) == "" {
			return
		}
		doc.Blocks = append(doc.Blocks, loader.Block{
			Type:  blockType,
			Level: level,
			Page:  1,
			Index: index,
			Inlines: []loader.Inline{
				{Type: loader.InlineText, Text: content, Index: 0},
			},
		})
		index++
	}

	text := strings.ReplaceAll(string(req.Content), "\r\n", "\n")
	var paragraph []string
	flushParagraph := func() {
		if len(paragraph) == 0 {
			return
		}
		appendBlock(loader.BlockText, strings.Join(paragraph, "\n"), 0)
		paragraph = paragraph[:0]
	}

	for _, line := range strings.Split(text, "\n") {
		if level, title, ok := parseHeadingLine(line); ok {
			flushParagraph()
			appendBlock(loader.BlockParagraphTitle, title, level)
			continue
		}
		if strings.TrimSpace(line) == "" {
			flushParagraph()
			continue
		}
		paragraph = append(paragraph, line)
	}
	flushParagraph()

	return doc, nil
}

// parseHeadingLine 识别 Markdown 标题行，返回层级（1~6）与标题文本。
// 只接受「整行都是标题」的形式：正文里出现 # 只是普通字符。
func parseHeadingLine(line string) (level int, title string, ok bool) {
	if !strings.HasPrefix(line, "#") {
		return 0, "", false
	}
	for level < len(line) && line[level] == '#' {
		level++
	}
	if level == 0 || level > 6 {
		return 0, "", false
	}
	title = strings.TrimSpace(line[level:])
	if title == "" {
		return 0, "", false
	}
	return level, title, true
}
