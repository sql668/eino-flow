package keyword

import (
	"fmt"
	"strings"
)

// 分词器实现标识（配置项取值）。
const (
	TokenizerLocal      = "local"
	TokenizerDictionary = "dictionary"
)

// NewTokenizer 按配置构造分词器。
//
// 工厂只负责"选实现"：初始化在应用启动时做一次，
// 请求路径上不再构造（词典加载需要约 2 秒）。
func NewTokenizer(kind string) (Tokenizer, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", TokenizerLocal:
		return NewLocalTokenizer(), nil
	case TokenizerDictionary:
		return NewDictionaryTokenizer()
	default:
		return nil, fmt.Errorf("未知的分词器 %q（可选：%s / %s）",
			kind, TokenizerLocal, TokenizerDictionary)
	}
}
