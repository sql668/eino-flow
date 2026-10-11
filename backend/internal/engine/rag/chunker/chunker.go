// Package chunker 把解析出的结构化块切成适合检索的片段。
//
// 这是 RAG 质量最关键的一环，也是解析器明确不做的部分
// （MinerU 只负责输出结构化块，官方声明「不是 RAG 框架」）。
// 三种分段模式（由知识库配置决定，工作台上可反复试验）：
//   - title（默认）：**标题开启新片段**，并把标题路径当面包屑带进正文，
//     超长再按长度切开。适合手册、论文这类有层级结构的文档；
//   - length：忽略标题，纯按长度切。适合聊天导出、日志这类无结构文本；
//   - separator：先按自定义分隔符拆，再按长度切。适合「一段一条记录」的文本。
//
// 另外两条不变的规则：
//   - **表格整块成段，绝不切开**：切开就丢了行列关系，碎片没有意义；
//   - 页眉 / 页脚 / 页码默认丢弃（可通过 KeepNoise 保留）。
//
// 窗口重叠（OverlapRunes）：只在「同一章节内被长度切开的相邻片段」之间生效。
// 标题切换处不重叠——语义单元已经变了，重叠只会把上一节的内容带进下一节。
package chunker

import (
	"errors"
	"fmt"
	"strings"

	"einoflow-backend/internal/engine/rag/loader"
	"einoflow-backend/internal/model"
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

// ErrModeNotImplemented 表示所选分段模式尚未实现。
//
// 刻意返回错误而不是退化成默认模式：使用者选了「父子分段」却得到普通分段，
// 会让人以为功能生效了，这类静默降级比直接报错难查得多。
var ErrModeNotImplemented = errors.New("分段模式尚未实现")

// Options 是切片参数。
type Options struct {
	// Mode 是分段模式；空值视为 title。
	Mode model.ChunkMode
	// Separator 仅在 Mode=separator 时使用（例如 "\n\n"）。
	Separator string
	// MaxRunes 是单个片段的目标长度上限（按字符计）。
	MaxRunes int
	// OverlapRunes 是相邻片段的重叠窗口长度（字符数）。
	OverlapRunes int
	// MinRunes 是独立成段的最小长度；短于此长度的正文块会与相邻内容合并，
	// 避免产生大量「半句话」片段污染检索结果。
	MinRunes int
	// KeepNoise 为 true 时保留页眉/页脚/页码。默认丢弃。
	KeepNoise bool
}

// Stats 是切片结果的统计，供分段工作台做健康度提示。
type Stats struct {
	Total         int
	TotalRunes    int
	AvgRunes      int
	MaxChunkRunes int
	// AtLimit 是达到长度上限被强制切开的片段数。
	// 数量多说明上限偏小（正文被硬切开），界面上会提示调大或改用标题分段。
	AtLimit int
	// TooShort 是合并后仍短于最小长度的片段数（多为表格或孤立短行）。
	TooShort int
	Tables   int
	// OverlapRunes 是重叠引入的重复字符总量：让使用者看得见「重叠的代价」。
	OverlapRunes int
	// DroppedNoise 是被丢弃的噪声块数（页眉/页脚/页码）。
	DroppedNoise int
}

// Result 是一次切片的结果。
type Result struct {
	Chunks []Chunk
	Stats  Stats
	// Hints 是给使用者的可执行建议（例如「8 个片段贴着上限被切开」）。
	Hints []string
}

// withDefaults 返回补齐默认值后的参数。
//
// 与 model.ChunkConfig.Normalize 保持同一套边界：配置从界面来，
// 两边各写一份默认值迟早会漂移，这里只是兜底（正常路径上配置已规范化）。
func (o Options) withDefaults() Options {
	if o.Mode == "" {
		o.Mode = model.ChunkModeTitle
	}
	if o.MaxRunes <= 0 {
		o.MaxRunes = DefaultMaxRunes
	}
	if o.MinRunes <= 0 {
		o.MinRunes = DefaultMinRunes
	}
	if o.OverlapRunes < 0 {
		o.OverlapRunes = 0
	}
	if maxOverlap := o.MaxRunes / 2; o.OverlapRunes > maxOverlap {
		o.OverlapRunes = maxOverlap
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
	// CharCount 是片段字符数（不含标题面包屑），用于长度统计与健康度提示。
	CharCount int
	// OverlapRunes 是片段开头与前一片段重叠的字符数（0 表示无重叠）。
	OverlapRunes int
	// BlockCount 是该片段由多少个块合并而成。
	BlockCount int
}

// Split 把解析结果切成片段。
func Split(doc *loader.ParsedDocument, opts Options) (Result, error) {
	if doc == nil {
		return Result{}, nil
	}
	opts = opts.withDefaults()
	if opts.Mode == model.ChunkModeParentChild {
		return Result{}, fmt.Errorf("%w: 父子分段", ErrModeNotImplemented)
	}

	blocks := doc.Blocks
	if opts.Mode == model.ChunkModeSeparator {
		blocks = expandSeparator(blocks, opts.Separator)
	}

	builder := &builder{opts: opts}
	for _, block := range blocks {
		builder.add(block)
	}
	builder.flush(flushByEnd)

	stats, hints := summarize(builder.chunks, opts, builder.droppedNoise)
	return Result{Chunks: builder.chunks, Stats: stats, Hints: hints}, nil
}

// expandSeparator 按分隔符把块拆成更小的块。
//
// 拆的时候保留原块的位置信息（页码/定位串）：片段溯源仍然指向原文位置，
// 否则按分隔符切完之后就无法回答「这段来自哪一页」。
func expandSeparator(blocks []loader.Block, separator string) []loader.Block {
	if separator == "" || len(blocks) == 0 {
		return blocks
	}
	out := make([]loader.Block, 0, len(blocks))
	for _, block := range blocks {
		if block.Type != loader.BlockText {
			out = append(out, block)
			continue
		}
		parts := strings.Split(block.Text(), separator)
		if len(parts) <= 1 {
			out = append(out, block)
			continue
		}
		for _, part := range parts {
			if strings.TrimSpace(part) == "" {
				continue
			}
			piece := loader.Block{
				Type:    block.Type,
				Inlines: []loader.Inline{{Type: loader.InlineText, Text: strings.TrimSpace(part)}},
				Level:   block.Level,
				Page:    block.Page,
				Index:   block.Index,
				BBox:    block.BBox,
			}
			out = append(out, piece)
		}
	}
	return out
}

// builder 累积块并产出片段。
type builder struct {
	opts   Options
	chunks []Chunk

	// titles 是当前标题路径的栈，元素为 (层级, 标题文本)。
	titles []titleEntry
	// pending 是尚未成段的正文块。
	pending []loader.Block
	// droppedNoise 是被丢弃的噪声块数，计入统计供界面展示。
	droppedNoise int
	// afterLengthCut 表示「上一次成段是因为长度切开的」。
	// 下一个片段才需要带重叠窗口——重叠只发生在同一章节内被长度切开的相邻片段之间，
	// 标题切换与表格之后都不重叠（语义单元已经变了）。
	//
	// 用「上一次的切分原因」而不是「下一次是否要重叠」：后者会在第一次成段时
	// 被无前一条可重叠的情况吃掉标志，导致第二段该重叠时反而没有。
	afterLengthCut bool
}

// flushReason 说明这次成段的原因，重叠逻辑依赖它。
type flushReason int

const (
	flushByTitle  flushReason = iota // 标题边界：不重叠
	flushByTable                     // 表格块：不重叠
	flushByLength                    // 长度切分：下一段要重叠
	flushByEnd                       // 文档结束：沿用上一次的原因
)

type titleEntry struct {
	level int
	text  string
}

// add 处理一个块。
func (b *builder) add(block loader.Block) {
	if !b.opts.KeepNoise && block.Type.IsNoise() {
		b.droppedNoise++
		return
	}

	// length 模式忽略标题语义：标题当普通正文处理，这样「按固定长度切」的结果
	// 不会被标题频繁打断（选这个模式的场景本来就是文档没有可靠层级）。
	if b.opts.Mode == model.ChunkModeLength {
		if block.Type.IsTitle() {
			block.Type = loader.BlockText
		}
	}

	switch {
	case block.Type.IsTitle():
		// 标题开启新片段：先把已有正文结掉，避免跨章节粘连。
		// 章节边界不重叠（见包注释）。
		b.flush(flushByTitle)
		b.pushTitle(block.Level, block.Text())

	case block.Type == loader.BlockTable:
		// 表格整块独立成段，不参与正文累积，也不会被切开；表格同样不重叠。
		b.flush(flushByTable)
		b.emit(block, []loader.Block{block}, KindTable)

	default:
		text := block.Text()
		// 单个块本身超长：必须硬切开。
		// 不切的话，一段 3000 字的正文会变成一个 3000 字的片段——
		// 远超出配置的长度上限，界面上调参数就完全看不出效果。
		if runeLen(text) > b.opts.MaxRunes {
			b.flush(flushByTitle)
			b.emitOversized(block)
			return
		}

		// 先看「加上这一块会不会超限」，会超就先结掉上一片。
		//
		// 原来的做法是「加进来之后再判断是否超限」，结果是片段可以长到接近两倍上限
		// （最后一块总是白搭进去）；工作台上显示的长度与配置对不上，很容易被当成 bug。
		if len(b.pending) > 0 && b.pendingRunes()+runeLen(text) > b.opts.MaxRunes {
			b.flush(flushByLength)
		}
		b.pending = append(b.pending, block)
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
func (b *builder) flush(reason flushReason) {
	if len(b.pending) == 0 {
		return
	}

	blocks := b.pending
	b.pending = nil

	// 太短的正文不单独成段——半句话的片段会污染检索结果。
	// 但若它就是文档最后一段，仍要保留，否则内容会丢。
	text := joinBlocks(blocks)
	if runeLen(text) < b.opts.MinRunes && len(b.chunks) > 0 && len(blocks) == 1 {
		b.appendToPrevious(text)
		return
	}
	b.emit(blocks[0], blocks, KindText)
	// 记录本次切分原因：决定下一个片段是否带重叠窗口。
	// flushByEnd 不在其中——走到文档末尾时 pending 早就该按长度切完了。
	switch reason {
	case flushByLength:
		b.afterLengthCut = true
	case flushByTitle, flushByTable:
		b.afterLengthCut = false
	}
}

// appendToPrevious 把过短的尾部内容并入上一片段。
func (b *builder) appendToPrevious(text string) {
	last := &b.chunks[len(b.chunks)-1]
	last.Text = last.Text + "\n\n" + text
	last.Content = composeContent(last.TitlePath, last.Text)
	last.TokenCount = estimateTokens(last.Content)
	last.CharCount = runeLen(last.Text) - last.OverlapRunes
	last.BlockCount++
}

// emit 生成一个片段。
func (b *builder) emit(first loader.Block, blocks []loader.Block, kind string) {
	text := joinBlocks(blocks)
	if strings.TrimSpace(text) == "" {
		return
	}

	titlePath := b.currentTitlePath()

	// 窗口重叠：把上一片段末尾的 N 个字符放到本片段开头。
	// 三个条件缺一不可：正文片段、上一次是因为长度切开、且确实有上一条。
	overlapRunes := 0
	if kind == KindText && b.afterLengthCut && b.opts.OverlapRunes > 0 && len(b.chunks) > 0 {
		previous := b.chunks[len(b.chunks)-1]
		if overlap := tailRunes(previous.Text, b.opts.OverlapRunes); overlap != "" {
			text = overlap + "\n" + text
			overlapRunes = runeLen(overlap)
		}
	}
	b.afterLengthCut = false

	content := composeContent(titlePath, text)

	b.chunks = append(b.chunks, Chunk{
		Index:        len(b.chunks),
		Content:      content,
		Text:         text,
		TitlePath:    titlePath,
		Kind:         kind,
		Page:         first.Page,
		Locator:      first.Locator(),
		TokenCount:   estimateTokens(content),
		CharCount:    runeLen(text) - overlapRunes,
		OverlapRunes: overlapRunes,
		BlockCount:   len(blocks),
	})
}

// emitOversized 把一个超长块按上限硬切成多个片段。
//
// 切口只能落在字符边界上（按 rune 切）；理想情况下应该落在句子边界，
// 但解析结果里没有可靠的句子信息（PDF 的换行不等于句末），
// 强行按标点找切口反而可能切出更碎的片段。这里选择「按长度等分」，
// 并在统计里把这类片段标为 AtLimit，让使用者在界面上看到并调大上限。
func (b *builder) emitOversized(block loader.Block) {
	runes := []rune(block.Text())
	for start := 0; start < len(runes); start += b.opts.MaxRunes {
		end := min(start+b.opts.MaxRunes, len(runes))
		piece := loader.Block{
			Type:    block.Type,
			Inlines: []loader.Inline{{Type: loader.InlineText, Text: string(runes[start:end])}},
			Level:   block.Level,
			Page:    block.Page,
			Index:   block.Index,
			BBox:    block.BBox,
		}
		// 同一块内切开的相邻片段之间同样要重叠（它们属于同一个语义单元）。
		b.afterLengthCut = start > 0
		b.emit(piece, []loader.Block{piece}, KindText)
	}
}

// tailRunes 取字符串末尾的 n 个字符（按字符而不是字节，避免切断中文）。
func tailRunes(text string, n int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= n {
		return string(runes)
	}
	return string(runes[len(runes)-n:])
}

// summarize 汇总统计并生成可执行建议。
//
// 建议要具体到「调哪个参数」：只说「分段质量可能不好」对使用者没有帮助。
func summarize(chunks []Chunk, opts Options, droppedNoise int) (Stats, []string) {
	stats := Stats{Total: len(chunks), DroppedNoise: droppedNoise}
	if len(chunks) == 0 {
		return stats, []string{"没有产出任何片段：内容可能全被清洗规则或噪声过滤删掉了"}
	}

	for _, chunk := range chunks {
		stats.TotalRunes += chunk.CharCount
		stats.OverlapRunes += chunk.OverlapRunes
		if chunk.CharCount > stats.MaxChunkRunes {
			stats.MaxChunkRunes = chunk.CharCount
		}
		if chunk.Kind == KindTable {
			stats.Tables++
			// 表格整块不切是有意为之，因此不计入「贴着上限被切开」：
			// 否则一张长表格会触发「建议调大长度上限」这种错误建议。
			continue
		}
		if chunk.CharCount >= opts.MaxRunes {
			stats.AtLimit++
		}
		if chunk.CharCount < opts.MinRunes {
			stats.TooShort++
		}
	}
	stats.AvgRunes = stats.TotalRunes / len(chunks)

	hints := make([]string, 0, 3)
	if stats.AtLimit > 0 {
		hints = append(hints, fmt.Sprintf(
			"%d 个片段贴着长度上限（%d 字）被切开；可以调大上限，或改用按标题分段让切口落在章节边界上",
			stats.AtLimit, opts.MaxRunes))
	}
	if stats.TooShort > 0 {
		hints = append(hints, fmt.Sprintf(
			"%d 个片段短于最小长度（%d 字），多为表格或孤立短行；如果检索结果碎片化，可以调大最小长度",
			stats.TooShort, opts.MinRunes))
	}
	if opts.OverlapRunes > 0 && stats.OverlapRunes > 0 {
		hints = append(hints, fmt.Sprintf(
			"窗口重叠共引入 %d 字重复内容（每条片段开头带前一条结尾的 %d 字），检索时可能出现相邻片段同时命中",
			stats.OverlapRunes, opts.OverlapRunes))
	}
	if droppedNoise > 0 {
		hints = append(hints, fmt.Sprintf("已丢弃 %d 个页眉/页脚/页码块", droppedNoise))
	}
	return stats, hints
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
