package loader

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// ErrNoLoader 表示没有解析器能处理该文件类型。
var ErrNoLoader = errors.New("没有可用的解析器")

// LoadRequest 是一次解析请求。
type LoadRequest struct {
	// Filename 是原始文件名，用于判断类型，也用于结果里的 SourceName。
	Filename string
	// Content 是文件内容。
	// 以字节而不是路径传入，是为了让调用方自己决定来源（对象存储、HTTP 上传、本地文件），
	// 也便于后续做并发解析与重试。
	Content []byte
	// MIMEType 可选，仅在上游能提供时填写。
	MIMEType string
	// Options 承载解析器特有参数，例如 MinerU 的档位与 OCR 模式。
	//
	// 用 map 而不是把各引擎的参数都堆进这个结构体：通用接口不该被单一实现绑架。
	// 键名由各实现导出常量，避免调用方凭字符串猜。
	Options map[string]string
}

// FileType 返回规范化的文件类型：小写扩展名，不含点。
func (r LoadRequest) FileType() string {
	return strings.ToLower(strings.TrimPrefix(filepath.Ext(r.Filename), "."))
}

// Option 读取一个解析器特有参数，不存在时返回空串。
func (r LoadRequest) Option(key string) string {
	if r.Options == nil {
		return ""
	}
	return r.Options[key]
}

// Loader 是文档解析器的统一抽象。
//
// 目前只实现 MinerU 与纯文本兜底。**这个接口是按这两个实现的实际需要定义的**，
// 等第三个实现出现时再重构：只有一个实现时凭空设计"通用解析器接口"，
// 只会得到一堆描述不存在场景的字段。
type Loader interface {
	// Name 返回解析器标识，会写进 ParsedDocument.Meta.Parser。
	Name() string
	// Supports 判断能否处理该文件类型（小写扩展名，不含点）。
	Supports(fileType string) bool
	// Load 执行解析。
	Load(ctx context.Context, req LoadRequest) (*ParsedDocument, error)
}

// Resolver 按文件类型选择解析器。
type Resolver struct {
	loaders []Loader
}

// NewResolver 构造解析器注册表。
// 按传入顺序匹配，**因此把优先使用的解析器放在前面**。
func NewResolver(loaders ...Loader) *Resolver {
	return &Resolver{loaders: loaders}
}

// Resolve 返回能处理该文件的解析器；都不支持时返回包装了 ErrNoLoader 的错误。
func (r *Resolver) Resolve(filename string) (Loader, error) {
	fileType := strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), "."))
	if fileType == "" {
		return nil, fmt.Errorf("%w: 无法从文件名 %q 判断文件类型", ErrNoLoader, filename)
	}

	for _, item := range r.loaders {
		if item.Supports(fileType) {
			return item, nil
		}
	}

	registered := make([]string, 0, len(r.loaders))
	for _, item := range r.loaders {
		registered = append(registered, item.Name())
	}
	sort.Strings(registered) // 排序只为让错误信息稳定，便于测试与排查
	return nil, fmt.Errorf("%w: 类型 %q 不被任何已注册解析器支持（已注册: %s）",
		ErrNoLoader, fileType, strings.Join(registered, ", "))
}
