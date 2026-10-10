package repository

import "context"

// Dependency 是一个可做连通性探测的外部依赖（数据库、对象存储、向量库…）。
//
// 之所以定义为接口：service 层只依赖抽象，替换存储实现（或单测打桩）时不用改调用方。
// 带 Name 是为了让探针能说清「是谁不可用」——只说「依赖不可用」等于没说。
type Dependency interface {
	// Name 返回依赖名称，用于探针输出。
	Name() string
	// Ping 检查依赖是否可用。
	Ping(ctx context.Context) error
}
