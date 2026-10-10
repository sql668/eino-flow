// Package chunker 把解析出的结构化块切成适合检索的片段。
//
// 这是 RAG 质量最关键的一环，也是解析器明确不做的部分
// （MinerU 只负责输出结构化块，官方声明「不是 RAG 框架」）。
// 切片策略：
//   - 正文块按顺序累积，累计长度超过上限就切下一片；
//   - **标题块开启新片段**，并把标题路径（面包屑）带进片段正文——
//     这样「某个小节里的内容」被单独检索到时仍带着上下文归属；
//   - **表格整块成段，绝不切开**：切开就丢了行列关系，切出来的碎片没有意义；
//   - 页眉 / 页脚 / 页码直接丢弃，它们重复出现且对语义没有贡献。
package chunker

import (
	"strings"

	"einoflow-backend/internal/engine/rag/loader"
)

// 片段主要类型。
const (
	KindText  = "text"
	KindTable = "table"
)

// 默认参数。
//
// 长度按「字符数」而不是 token 数计算：接入具体 embedding 模型前拿不到准确分词，
// 用字符数近似（中文 1 字 ≈ 1 token 量级），等模型定下来再换成真实分词器。
// 这个取舍会体现在 TokenCount 字段上，它会明确标注为估算值。
const (
	DefaultMaxRunes = 800
	DefaultMinRunes = 40
)

// Options 是切片参数。
type Options struct {
	// MaxRunes 是单个片段的目标长度上限（按字符计）。
	MaxRunes int
	// MinRunes 是独立成段的最小长度；短于此长度的正文块会与相邻内容合并，
	// 避免产生大量「半句话」片段污染检索结果。
	MinRunes int
	// KeepNoise 为 true 时保留页眉/页脚/页码。默认丢弃。
	KeepNoise bool
}

// withDefaults 返回补齐默认值后的参数。
func (o Options) withDefaults() Options {
	if o.MaxRunes <= 0 {
		o.MaxRunes = DefaultMaxRunes
	}
	if o.MinRunes <= 0 {
		o.MinRunes = DefaultMinRunes
	}
	return o
}

// Chunk 是一个检索片段。
type Chunk struct {
	// Index 是片段在文档内的顺序号，从 0 开始。
	Index int
	// Content 是用于嵌入与展示的文本，包含标题面包屑前缀。
	Content string
	// Text 是不含面包屑的原始内容。
	Text string
	// TitlePath 是片段所属的标题路径，例如 ["第三章", "3.2 安装"]。
	TitlePath []string
	// Kind 是片段主要类型：text 或 table。
	Kind string
	// Page 是片段起始页码（1 基）。
	Page int
	// Locator 是片段起始块的定位串，用于把答案关联回原文位置。
	Locator string
	// TokenCount 是估算的 token 数（非精确分词，见包注释）。
	TokenCount int
	// BlockCount 是该片段由多少个块合并而成。
	BlockCount int
}

// Split 把解析结果切成片段。
func Split(doc *loader.ParsedDocument, opts Options) []Chunk {
	if doc == nil {
		return nil
	}
	opts = opts.withDefaults()

	builder := &builder{opts: opts}
	for _, block := range doc.Blocks {
		builder.add(block)
	}
	builder.flush()
	return builder.chunks
}

// builder 累积块并产出片段。
type builder struct {
	opts   Options
	chunks []Chunk

	// titles 是当前标题路径的栈，元素为 (层级, 标题文本)。
	titles []titleEntry
	// pending 是尚未成段的正文块。
	pending []loader.Block
}

type titleEntry struct {
	level int
	text  string
}

// add 处理一个块。
func (b *builder) add(block loader.Block) {
	if !b.opts.KeepNoise && block.Type.IsNoise() {
		return
	}

	switch {
	case block.Type.IsTitle():
		// 标题开启新片段：先把已有正文结掉，避免跨章节粘连。
		b.flush()
		b.pushTitle(block.Level, block.Text())

	case block.Type == loader.BlockTable:
		// 表格整块独立成段，不参与正文累积，也不会被切开。
		b.flush()
		b.emit(block, []loader.Block{block}, KindTable)

	default:
		b.pending = append(b.pending, block)
		if b.pendingRunes() >= b.opts.MaxRunes {
			b.flush()
		}
	}
}

// pushTitle 维护标题栈：弹出层级不低于当前的标题，再压入新标题。
// 用「层级比较」而不是「层级减一」做下标，是因为解析器的层级编号不保证从 1 开始连续
// （实测 MinerU 对一级标题给的是 level=2）。
func (b *builder) pushTitle(level int, text string) {
	if text == "" {
		return
	}
	for len(b.titles) > 0 && b.titles[len(b.titles)-1].level >= level {
		b.titles = b.titles[:len(b.titles)-1]
	}
	b.titles = append(b.titles, titleEntry{level: level, text: text})
}

// pendingRunes 返回待成段正文的总字符数。
func (b *builder) pendingRunes() int {
	total := 0
	for _, block := range b.pending {
		total += runeLen(block.Text())
	}
	return total
}

// flush 把待成段的正文块合成一个片段。
func (b *builder) flush() {
	if len(b.pending) == 0 {
		return
	}

	blocks := b.pending
	b.pending = nil

	// 太短的正文且不以标题开头时，不单独成段——半句话的片段会污染检索结果。
	// 但若它就是文档最后一段，仍要保留，否则内容会丢。
	text := joinBlocks(blocks)
	if runeLen(text) < b.opts.MinRunes && len(b.chunks) > 0 && len(blocks) == 1 {
		b.appendToPrevious(text)
		return
	}
	b.emit(blocks[0], blocks, KindText)
}

// appendToPrevious 把过短的尾部内容并入上一片段。
func (b *builder) appendToPrevious(text string) {
	last := &b.chunks[len(b.chunks)-1]
	last.Text = last.Text + "\n\n" + text
	last.Content = composeContent(last.TitlePath, last.Text)
	last.TokenCount = estimateTokens(last.Content)
	last.BlockCount++
}

// emit 生成一个片段。
func (b *builder) emit(first loader.Block, blocks []loader.Block, kind string) {
	text := joinBlocks(blocks)
	if strings.TrimSpace(text) == "" {
		return
	}

	titlePath := b.currentTitlePath()
	content := composeContent(titlePath, text)

	b.chunks = append(b.chunks, Chunk{
		Index:      len(b.chunks),
		Content:    content,
		Text:       text,
		TitlePath:  titlePath,
		Kind:       kind,
		Page:       first.Page,
		Locator:    first.Locator(),
		TokenCount: estimateTokens(content),
		BlockCount: len(blocks),
	})
}

// currentTitlePath 返回标题栈中保存的路径副本。
func (b *builder) currentTitlePath() []string {
	if len(b.titles) == 0 {
		return nil
	}
	path := make([]string, 0, len(b.titles))
	for _, item := range b.titles {
		path = append(path, item.text)
	}
	return path
}

// joinBlocks 把多个块拼成一段文本，块之间用空行分隔。
func joinBlocks(blocks []loader.Block) string {
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if text := strings.TrimSpace(block.Text()); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n\n")
}

// composeContent 把标题路径拼成面包屑前缀，附在正文之前。
// 目的是让脱离章节上下文后仍能被检索到并保留归属信息。
func composeContent(titlePath []string, text string) string {
	if len(titlePath) == 0 {
		return text
	}
	return "【" + strings.Join(titlePath, " > ") + "】\n" + text
}

// estimateTokens 估算 token 数。
// 中文按 1 字符 ≈ 1 token 估算，英文会偏保守（偏大）。
// 接入 embedding 模型后应替换为真实分词器，这里的值仅用于切片长度控制。
func estimateTokens(text string) int {
	return runeLen(text)
}

// runeLen 返回字符数（而非字节数），避免中文被按字节计算导致片段过短。
func runeLen(text string) int {
	return len([]rune(text))
}
