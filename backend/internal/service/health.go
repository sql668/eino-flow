package service

import (
	"context"
	"errors"
	"fmt"

	"einoflow-backend/internal/repository"
)

// HealthService 汇总各依赖组件的连通性，供就绪探针使用。
type HealthService struct {
	dependencies []repository.Dependency
}

// NewHealthService 构造健康检查服务。
//
// 依赖以切片传入而不是逐个字段：每加一个依赖都要改构造函数签名的话，
// 调用方与测试都会被迫跟着改，而这里真正需要的只是「一个可遍历的清单」。
func NewHealthService(dependencies ...repository.Dependency) *HealthService {
	return &HealthService{dependencies: dependencies}
}

// Check 检查所有依赖；任一不可用即视为未就绪。
//
// 全部检查完再返回（而不是遇到第一个错误就中断）：依赖挂了通常不是挂一个，
// 一次把问题列全，省去「修一个再发现下一个」的往返。
// 返回值用 errors.Join 聚合，因此错误信息里能同时看到多个依赖。
func (s *HealthService) Check(ctx context.Context) error {
	var errs []error
	for _, dependency := range s.dependencies {
		if err := dependency.Ping(ctx); err != nil {
			errs = append(errs, fmt.Errorf("%s 不可用: %w", dependency.Name(), err))
		}
	}
	return errors.Join(errs...)
}
