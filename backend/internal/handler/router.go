package handler

import "github.com/cloudwego/hertz/pkg/app/server"

// Register 把全部 HTTP 路由注册到 Hertz server。
//
// V0.1 阶段只暴露探针；业务路由（应用 / 知识库 / 对话调试）后续统一挂在 /api/v1 下。
func Register(h *server.Hertz) {
	registerHealth(h)

	// TODO(V0.1): apiV1 := h.Group("/api/v1")，再挂载应用、知识库、对话路由。
}
