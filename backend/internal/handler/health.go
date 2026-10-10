package handler

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// registerHealth 注册存活 / 就绪探针。
// 探针属于基础设施路由，不含业务逻辑，因此直接实现在 handler 层。
func registerHealth(h *server.Hertz) {
	h.GET("/healthz", live)
	h.GET("/readyz", ready)
}

// live 存活探针：只要进程能响应即视为存活。
func live(_ context.Context, c *app.RequestContext) {
	c.JSON(consts.StatusOK, OK(nil))
}

// ready 就绪探针：确认服务已可以接收业务流量。
//
// TODO(V0.1): 接入 MySQL / Redis 连通性检查后再返回就绪状态。
func ready(_ context.Context, c *app.RequestContext) {
	c.JSON(consts.StatusOK, OK(map[string]string{"status": "ready"}))
}
