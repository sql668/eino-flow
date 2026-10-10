// Package handler 是 HTTP 接入层。
//
// 约定（见 backend/AGENTS.md）：handler 只做参数解析、校验、响应组装与路由注册，
// 业务逻辑必须下沉到 service，AI 编排必须下沉到 engine。
package handler

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
