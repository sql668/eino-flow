// Package model 存放领域模型。
//
// 约定：
//   - 结构体既描述业务实体，也承载 GORM 的映射信息；列名按 snake_case 约定对应，
//     个别对不上的（如 LatencyMS）用 column 标签显式声明；
//   - **表结构以 internal/repository/postgres/migrations 下的 SQL 迁移脚本为准**，
//     不使用 GORM AutoMigrate。加字段时必须同时补迁移脚本，模型标签只负责读写映射；
//   - service / repository / handler 各层之间传递数据统一使用本包类型，
//     避免同一份数据在各层出现多套相似结构体。
package model
