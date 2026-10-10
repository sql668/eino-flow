package handler

import (
	"context"
	"log"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// ReadinessChecker 抽象就绪检查能力，具体实现由上层注入。
// handler 只依赖抽象，不直接依赖数据库等基础设施（见根 AGENTS.md「面向接口」）。
type ReadinessChecker interface {
	Check(ctx context.Context) error
}

// registerHealth 注册存活 / 就绪探针。
// 探针属于基础设施路由，不含业务逻辑，因此直接实现在 handler 层。
func registerHealth(h *server.Hertz, checker ReadinessChecker) {
	h.GET("/healthz", live)
	h.GET("/readyz", ready(checker))
}

// live 存活探针：只要进程能响应即视为存活。
// 刻意不检查依赖：依赖抖动不应导致容器被反复重启。
func live(_ context.Context, c *app.RequestContext) {
	c.JSON(consts.StatusOK, OK(nil))
}

// ready 就绪探针：确认服务已可以接收业务流量。
func ready(checker ReadinessChecker) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if err := checker.Check(ctx); err != nil {
			// 该接口会经由前端同源转发暴露到公网，所以只记录详情、对外返回泛化信息，
			// 避免把数据库地址等内部细节泄漏出去。
			log.Printf("就绪检查失败: %v", err)
			c.JSON(consts.StatusServiceUnavailable, Fail(CodeInternal, "服务尚未就绪"))
			return
		}
		c.JSON(consts.StatusOK, OK(map[string]string{"status": "ready"}))
	}
}
