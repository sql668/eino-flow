// Package loader 定义文档解析层的抽象与中间表示（IR）。
//
// 设计原则（依据 2026-10 对 MinerU 4.0.11 实际输出的实测）：
//   - **保留结构，不拍平**：解析结果是一组带类型的块，不是一个字符串。
//     标题层级、表格行列、页码都要留下来，否则切片时只能按固定长度切，语义会被切断。
//   - **对未知类型开放**：上游解析器会新增块类型（MinerU 已出现 text / paragraph_title /
//     header / footer / page_number / image / table，文档里还提到 equation / chart / caption）。
//     这里用开放的字符串类型 + 判定函数，遇到不认识的类型透传而不是报错，
//     避免上游一升级我们就整个解析失败。
//   - **不泄漏具体解析器的字段**：本包的类型是项目自己的契约，MinerU 的输出由
//     loader/mineru 负责映射进来。将来换解析器时，只有那个映射需要改。
package loader

import "strconv"

// BlockType 是块的语义类型。
// 用字符串而不是封闭枚举：未知类型要能透传，见包注释。
type BlockType string

const (
	BlockText           BlockType = "text"            // 正文段落
	BlockParagraphTitle BlockType = "paragraph_title" // 段落级标题，带层级
	BlockTable          BlockType = "table"           // 表格
	BlockImage          BlockType = "image"           // 图片
	BlockEquation       BlockType = "equation"        // 公式
	BlockHeader         BlockType = "header"          // 页眉
	BlockFooter         BlockType = "footer"          // 页脚
	BlockPageNumber     BlockType = "page_number"     // 页码
)

// IsKnown 判断是否为已识别的块类型。
// 未识别的类型不丢弃、不报错，只是无法参与依赖语义的处理（例如按标题切分）。
func (t BlockType) IsKnown() bool {
	switch t {
	case BlockText, BlockParagraphTitle, BlockTable, BlockImage,
		BlockEquation, BlockHeader, BlockFooter, BlockPageNumber:
		return true
	default:
		return false
	}
}

// IsTitle 表示该块是标题，切片时可以据此划分章节边界。
func (t BlockType) IsTitle() bool { return t == BlockParagraphTitle }

// IsNoise 表示页眉、页脚、页码这类重复出现、对语义贡献很低的块。
// 是否丢弃由切片策略决定，解析层只负责标注。
func (t BlockType) IsNoise() bool {
	return t == BlockHeader || t == BlockFooter || t == BlockPageNumber
}

// InlineType 是块内联元素的类型。
// MinerU 的块是两层结构：块（位置+语义）里含若干内联元素，因此这里保留这一层。
type InlineType string

const (
	InlineText          InlineType = "text"           // 纯文本
	InlineTableBody     InlineType = "table_body"     // 表格主体，内容是 HTML
	InlineTableFootnote InlineType = "table_footnote" // 表注
	InlineImageBody     InlineType = "image_body"     // 图片主体，字节不在 JSON 里
	InlineImageFootnote InlineType = "image_footnote" // 图注
	InlineHyperlink     InlineType = "hyperlink"      // 超链接
)

// IsKnown 判断是否为已识别的内联类型。未识别的类型同样透传，只做提示。
func (t InlineType) IsKnown() bool {
	switch t {
	case InlineText, InlineTableBody, InlineTableFootnote,
		InlineImageBody, InlineImageFootnote, InlineHyperlink:
		return true
	default:
		return false
	}
}

// Inline 是块内的一个内联元素。
type Inline struct {
	Type InlineType
	// Text 是元素内容。文本类元素是纯文本；表格主体是 HTML（保留行列结构）；
	// 图片主体为空——图片字节需要另外获取，本层不做下载。
	Text string
	// BBox 是归一化坐标 [x0, y0, x1, y1]，取值 0~1，脱离页面分辨率也能对应回原图。
	BBox []float64
	// Index 是元素在块内的序号。
	Index int
}

// Block 是一个语义块。
type Block struct {
	Type BlockType
	// Inlines 保留块内结构。反序列化或构造时若为空，视为无内容块。
	Inlines []Inline
	// Level 是标题层级（仅标题块有意义）：1 为最高级。
	Level int
	// Page 是所在页码，从 1 开始（对外友好；解析器内部的 0 基页码在映射时转换）。
	Page int
	// Index 是块在页内的序号，与 Page 一起构成可回溯的定位。
	Index int
	// BBox 是归一化坐标 [x0, y0, x1, y1]，用于溯源时高亮原文位置。
	BBox []float64
}

// Text 返回块内所有内联元素的文本拼接结果。
// 表格会返回 HTML，需要纯文本请用 StripHTML 处理后再用。
func (b Block) Text() string {
	var builder []byte
	for _, inline := range b.Inlines {
		if inline.Text == "" {
			continue
		}
		if len(builder) > 0 {
			builder = append(builder, '\n')
		}
		builder = append(builder, inline.Text...)
	}
	return string(builder)
}

// IsEmpty 表示块内没有任何文本内容（例如只有图片）。
func (b Block) IsEmpty() bool { return b.Text() == "" }

// Locator 返回块的定位串，用于把切片结果关联回原文位置。
// 格式为 page:{页码}/block:{块号}，与页码一起存进切片表即可支持引用溯源。
func (b Block) Locator() string {
	return "page:" + strconv.Itoa(b.Page) + "/block:" + strconv.Itoa(b.Index)
}

// DocumentMeta 是解析元信息。
//
// 这几个字段要随切片结果一起落库：更换解析器或升级解析器版本会改变切片结果，
// 必须能据此判断是否需要重建索引（与 embedding_model 同理）。
type DocumentMeta struct {
	// Parser 是解析器标识，例如 "mineru" 或 "plaintext"。
	Parser string
	// ParserVersion 是解析器版本，例如 "4.0.11"。
	ParserVersion string
	// Tier 是解析档位，例如 "basic"；纯文本解析器为空。
	Tier string
	// ParseMode 是实际生效的解析模式，例如 "txt"（有文本层）或 "ocr"（扫描件走 OCR）。
	ParseMode string
	// PageCount 是文档总页数。
	PageCount int
	// SourceName 是原始文件名。
	SourceName string
	// FileType 是原始文件类型（扩展名，小写，不含点）。
	FileType string
}

// ParsedDocument 是解析结果，即本项目内部的文档中间表示。
type ParsedDocument struct {
	// Blocks 按阅读顺序排列（跨页连续）。
	Blocks []Block
	Meta   DocumentMeta
	// Warnings 记录非致命问题（例如某些页解析失败、图片未取到）。
	// 有警告但仍有内容时应当继续入库，而不是整篇失败——上游的这类降级信息要保留下来。
	Warnings []string
}

// Text 返回整篇文档的纯文本拼接，便于快速预览或调试。
func (d *ParsedDocument) Text() string {
	var builder []byte
	for _, block := range d.Blocks {
		text := block.Text()
		if text == "" {
			continue
		}
		if len(builder) > 0 {
			builder = append(builder, '\n', '\n')
		}
		builder = append(builder, text...)
	}
	return string(builder)
}
