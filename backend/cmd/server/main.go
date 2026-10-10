// EinoFlow 后端服务入口。
//
// 启动流程：加载配置 → 构造 Hertz server → 注册路由 → 阻塞运行（内部处理退出信号与优雅关闭）。
package main

import (
	"flag"
	"log"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"

	"einoflow-backend/internal/config"
	"einoflow-backend/internal/handler"
)

func main() {
	configPath := flag.String("config", "", "配置文件路径（默认读取环境变量 EINOFLOW_CONFIG 或 configs/config.yaml）")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	h := server.New(
		server.WithHostPorts(cfg.Server.Addr()),
		// 收到退出信号后等待在途请求处理完再关闭，避免流式响应被硬切断。
		server.WithExitWaitTime(10*time.Second),
	)

	handler.Register(h)

	log.Printf("EinoFlow 后端启动中: env=%s addr=%s", cfg.App.Env, cfg.Server.Addr())
	// Spin 内部处理 SIGINT / SIGTERM，并在退出时优雅关闭 server。
	h.Spin()
}
