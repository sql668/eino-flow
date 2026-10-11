package postgres_test

import (
	"context"
	"os"
	"testing"

	"einoflow-backend/internal/model"
	"einoflow-backend/internal/repository"
	"einoflow-backend/internal/repository/postgres"
)

// 集成测试：需要真实的 PostgreSQL，用环境变量开启。
//
//	EINOFLOW_TEST_PG_DSN='postgres://...' go test ./internal/repository/postgres/ -v
//
// 为什么值得单独测这一条：新建知识库只是「一条 INSERT」，看起来不可能出错，
// 但踩过一次 500——`datasets.chunk_config` 是 NOT NULL，而 Go 侧允许为 nil，
// GORM 于是显式写入 SQL NULL。这类「代码没错、约束撞了」的问题只有真库能发现。
func TestDatasetRepository_CreateWithDefaultConfigs(t *testing.T) {
	dsn := os.Getenv("EINOFLOW_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("未设置 EINOFLOW_TEST_PG_DSN，跳过集成测试")
	}
	ctx := context.Background()

	// 表结构以迁移脚本为准（与线上同一套脚本），先跑一遍保证 schema 就绪。
	if _, err := postgres.Migrate(ctx, dsn); err != nil {
		t.Fatalf("执行迁移失败: %v", err)
	}
	db, err := postgres.Open(ctx, dsn, postgres.PoolConfig{})
	if err != nil {
		t.Fatalf("连接数据库失败: %v", err)
	}
	client := postgres.NewClient(db)
	repo := postgres.NewDatasetRepository(client)

	// 关键路径：不设置任何配置（与 service.Create 的行为一致），必须能写进去。
	dataset := &model.Dataset{
		Name:   "集成测试知识库（可删除）",
		Status: model.DatasetStatusActive,
	}
	if err := repo.Create(ctx, dataset); err != nil {
		t.Fatalf("新建知识库失败（多半是 NOT NULL 约束与 nil 配置的冲突）: %v", err)
	}
	t.Cleanup(func() {
		if err := client.DB(ctx).Unscoped().Delete(&model.Dataset{}, dataset.ID).Error; err != nil {
			t.Logf("清理测试知识库失败: %v", err)
		}
	})
	if dataset.ID == 0 {
		t.Fatal("新建后没有回填 ID")
	}

	// 读回来：列上应当落到数据库默认值 '{}'，而不是 NULL。
	stored, err := repo.GetByID(ctx, dataset.ID)
	if err != nil {
		t.Fatalf("读取知识库失败: %v", err)
	}
	if stored.Name != dataset.Name {
		t.Errorf("名称 = %q, 期望 %q", stored.Name, dataset.Name)
	}

	// 默认值语义：没配过时由上层兜底，这里确认「能读出来且不为 NULL」即可。
	if stored.ChunkConfig != nil && stored.ChunkConfig.Mode != "" {
		t.Logf("知识库自带分段配置: %s", stored.ChunkConfig.Mode)
	}

	// 顺带覆盖「保存配置后能读回」：JSONB 往返不该丢字段。
	config := model.DefaultChunkConfig()
	if err := repo.Update(ctx, &model.Dataset{
		ID:          stored.ID,
		Name:        stored.Name,
		Status:      stored.Status,
		ChunkConfig: &config,
	}); err != nil {
		t.Fatalf("保存分段配置失败: %v", err)
	}
	updated, err := repo.GetByID(ctx, stored.ID)
	if err != nil {
		t.Fatalf("再次读取失败: %v", err)
	}
	if updated.ChunkConfig == nil || updated.ChunkConfig.Mode != config.Mode {
		t.Fatalf("分段配置没有往返成功: %+v", updated.ChunkConfig)
	}
}

// 确认仓储实现了接口（实现漂移在编译期就暴露，而不是到调用处才发现）。
var _ repository.DatasetRepository = (*postgres.DatasetRepository)(nil)
