package mineru

import (
	"context"
	"fmt"

	"einoflow-backend/internal/engine/rag/loader"
)

// Options 键名（供 loader.LoadRequest.Options 使用）。
// 导出常量而不是让调用方拼字符串，避免拼错后静默失效。
const (
	OptionTier    = "tier"
	OptionOCRMode = "ocr_mode"
)

// 默认档位。
//
// basic 覆盖 OCR、版面分析与表格解析，且**不需要 VLM**（VLM 只在 standard / advanced 使用），
// 因此在 CPU 或容器环境里是质量与成本最平衡的选择。
// 实测 basic 约 0.25 页/秒（扫描件约 0.2 页/秒）。
const defaultTier = "basic"

// supportedTypes 是 MinerU 能处理的扩展名。
// PDF 与图片走版面/OCR 模型；Office 等原生格式由 MinerU 内部直接解析。
var supportedTypes = map[string]bool{
	"pdf":   true,
	"png":   true,
	"jpg":   true,
	"jpeg":  true,
	"bmp":   true,
	"tif":   true,
	"tiff":  true,
	"webp":  true,
	"doc":   true,
	"docx":  true,
	"ppt":   true,
	"pptx":  true,
	"xls":   true,
	"xlsx":  true,
	"rtf":   true,
	"odt":   true,
	"ods":   true,
	"odp":   true,
	"epub":  true,
	"ofd":   true,
	"html":  true,
	"htm":   true,
	"mhtml": true,
	"mht":   true,
	"csv":   true,
	"tsv":   true,
}

// flashOnlyTypes 是 MinerU 固定用 flash 档解析的格式。
// 这些格式没有版面分析或 OCR 需求（原生解析即可），显式声明以免依赖服务端兜底。
var flashOnlyTypes = map[string]bool{
	"doc": true, "docx": true, "ppt": true, "pptx": true, "xls": true, "xlsx": true,
	"rtf": true, "odt": true, "ods": true, "odp": true, "epub": true, "ofd": true,
	"html": true, "htm": true, "mhtml": true, "mht": true, "csv": true, "tsv": true,
}

// Loader 是 MinerU 解析器，实现 loader.Loader。
type Loader struct {
	client      *Client
	defaultTier string
}

// New 构造 MinerU 解析器；tier 为空时使用 basic。
func New(client *Client, tier string) *Loader {
	if tier == "" {
		tier = defaultTier
	}
	return &Loader{client: client, defaultTier: tier}
}

// Name 实现 loader.Loader。
func (l *Loader) Name() string { return "mineru" }

// Supports 实现 loader.Loader。
func (l *Loader) Supports(fileType string) bool { return supportedTypes[fileType] }

// Load 实现 loader.Loader。
func (l *Loader) Load(ctx context.Context, req loader.LoadRequest) (*loader.ParsedDocument, error) {
	fileType := req.FileType()
	if !l.Supports(fileType) {
		return nil, fmt.Errorf("mineru 不支持的文件类型: %q", fileType)
	}

	tier := req.Option(OptionTier)
	if tier == "" {
		tier = l.defaultTier
	}
	if flashOnlyTypes[fileType] {
		tier = "flash"
	}

	result, err := l.client.Parse(ctx, ParseRequest{
		Filename: req.Filename,
		Content:  req.Content,
		MIMEType: req.MIMEType,
		Tier:     tier,
		OCRMode:  req.Option(OptionOCRMode),
	})
	if err != nil {
		return nil, err
	}

	return toDocument(result, loadRequestInfo{Filename: req.Filename, FileType: fileType})
}

// loadRequestInfo 是映射时需要的最小输入。
// 单独定义而不是直接传 loader.LoadRequest，是为了让映射函数与传输层解耦、便于测试。
type loadRequestInfo struct {
	Filename string
	FileType string
}
