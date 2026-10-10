package service

import (
	"fmt"
	"strings"
)

// validateRequiredName 校验必填名称：去空白、非空、限长。
//
// 按「字符数」而不是字节数限长，否则中文名会被误判超长。
// 抽成公共函数是因为应用、知识库等实体的名称规则一致，避免各写一份。
func validateRequiredName(raw, field string, maxRunes int) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", fmt.Errorf("%w: %s不能为空", ErrInvalidInput, field)
	}
	if len([]rune(name)) > maxRunes {
		return "", fmt.Errorf("%w: %s不能超过 %d 个字符", ErrInvalidInput, field, maxRunes)
	}
	return name, nil
}
