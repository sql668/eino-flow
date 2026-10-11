package handler

import (
	"github.com/cloudwego/hertz/pkg/app/server"

	"einoflow-backend/internal/service"
)

// Deps 汇总 handler 层依赖的服务。
// 用结构体而不是位置参数：每加一个模块都要改 Register 签名会很难受。
type Deps struct {
	Health        *service.HealthService
	Apps          *service.AppService
	Datasets      *service.DatasetService
	Conversations *service.ConversationService
	Documents     *service.DocumentService
	Searches      *service.RetrievalService
	ModelConfig   *service.ModelConfigService
	Chats         *service.ChatService
	// MaxUploadBytes 是单个上传文档的大小上限（0 表示不限制）。
	MaxUploadBytes int64
}

// Register 把全部 HTTP 路由注册到 Hertz server。
func Register(h *server.Hertz, deps Deps) {
	registerHealth(h, deps.Health)

	apiV1 := h.Group("/api/v1")
	registerAppRoutes(apiV1, deps.Apps)
	registerDatasetRoutes(apiV1, deps.Datasets)
	registerConversationRoutes(apiV1, deps.Conversations)
	registerDocumentRoutes(apiV1, deps.Documents, deps.MaxUploadBytes)
	registerSearchRoutes(apiV1, deps.Searches)
	registerModelConfigRoutes(apiV1, deps.ModelConfig)
	registerChatRoutes(apiV1, deps.Chats)
	registerChunkWorkbenchRoutes(apiV1, deps.Documents)
	// TODO(V0.1): 继续挂载流式对话调试路由。
}
