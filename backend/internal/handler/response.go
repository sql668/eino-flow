// Package handler 是 HTTP 接入层。
//
// 约定（见 backend/AGENTS.md）：handler 只做参数解析、校验、响应组装与路由注册，
// 业务逻辑必须下沉到 service，AI 编排必须下沉到 engine。
package handler

import "time"

// 业务响应码。0 表示成功，其余按「HTTP 状态 * 100 + 细分」约定。
const (
	CodeOK           = 0
	CodeInvalidParam = 40000
	CodeUnauthorized = 40100
	CodeNotFound     = 40400
	CodeInternal     = 50000
)

// Response 是 API 统一响应结构，前端按 code / message / data 解析。
type Response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// OK 构造成功响应。
func OK(data interface{}) Response {
	return Response{Code: CodeOK, Message: "ok", Data: data}
}

// Fail 构造失败响应。
func Fail(code int, message string) Response {
	return Response{Code: code, Message: message}
}

// formatTime 统一时间输出格式：RFC3339（UTC）。
//
// 库里统一存 UTC，转成本地时区展示是前端的职责。
// 这里额外做一次 UTC 归一化，是因为 GORM 写入时会把 Go 的 time.Now()（本地时区）
// 直接填进内存对象返回，而回读的值来自数据库——不归一化的话，
// 「刚创建的」会输出 +08:00、「再查一次的」会输出 Z，同一个字段两种格式。
func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}
