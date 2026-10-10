package repository

import "context"

// HealthRepository 描述依赖组件的连通性检查能力。
//
// 之所以定义为接口：service 层只依赖抽象，替换存储实现（或单测打桩）时不用改调用方。
type HealthRepository interface {
	// Ping 检查依赖是否可用。
	Ping(ctx context.Context) error
}
