// Package postgres 提供基于 PostgreSQL 的数据访问实现。
//
// 约定（见 backend/AGENTS.md）：
//   - 本包只做数据存取，不含业务语义；
//   - 对外实现 internal/repository 中定义的接口，service 及以上层不感知 GORM 的存在，
//     `*gorm.DB` 不允许出现在本包之外；
//   - 表结构由 migrations 下的 SQL 脚本维护，不使用 GORM AutoMigrate。
package postgres

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 连接池默认值。仅在配置未提供时兜底，正常情况由 configs/config.yaml 决定。
const (
	defaultMaxOpenConns    = 20
	defaultMaxIdleConns    = 5
	defaultConnMaxLifetime = time.Hour
)

// PoolConfig 是连接池参数，由上层从配置注入，避免在代码里硬编码。
type PoolConfig struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// Open 建立 GORM 连接并做一次连通性探测，尽早暴露配置错误。
func Open(ctx context.Context, dsn string, pool PoolConfig) (*gorm.DB, error) {
	db, err := gorm.Open(gormpostgres.Open(dsn), &gorm.Config{
		// 关掉 GORM 给每条写操作默认套上的事务：单条写入不需要额外开销，
		// 真正需要事务时由 service 层通过 repository.TxManager 显式划定。
		SkipDefaultTransaction: true,
		// 日志级别调到 Warn 并忽略「记录不存在」：
		// 业务上大量存在「查了没有就是没有」的正常查询（比如解析模型时逐级找绑定），
		// 默认配置会把它们全部当错误打出来，真正的错误反而被淹没。
		Logger: logger.New(log.New(os.Stdout, "", log.LstdFlags), logger.Config{
			SlowThreshold:             500 * time.Millisecond,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
		}),
	})
	if err != nil {
		return nil, fmt.Errorf("打开 PostgreSQL 连接失败: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("获取底层连接池失败: %w", err)
	}

	sqlDB.SetMaxOpenConns(orDefault(pool.MaxOpenConns, defaultMaxOpenConns))
	sqlDB.SetMaxIdleConns(orDefault(pool.MaxIdleConns, defaultMaxIdleConns))
	if pool.ConnMaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(pool.ConnMaxLifetime)
	} else {
		sqlDB.SetConnMaxLifetime(defaultConnMaxLifetime)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("连接 PostgreSQL 失败: %w", err)
	}
	return db, nil
}

// orDefault 返回 value，为 0 时返回 fallback。
func orDefault(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}
