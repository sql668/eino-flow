package postgres

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	// 迁移走原生 database/sql：DDL 不需要 ORM，也让迁移逻辑与业务查询解耦。
	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// migrationsTable 记录已应用的迁移版本。
const migrationsTable = "schema_migrations"

// Migrate 按文件名顺序执行 migrations 目录下尚未应用的脚本，返回本次实际应用的脚本数量。
func Migrate(ctx context.Context, dsn string) (int, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return 0, fmt.Errorf("打开迁移连接失败: %w", err)
	}
	defer func() { _ = db.Close() }()

	if err := ensureMigrationsTable(ctx, db); err != nil {
		return 0, err
	}

	applied, err := appliedVersions(ctx, db)
	if err != nil {
		return 0, err
	}

	files, err := migrationFiles()
	if err != nil {
		return 0, err
	}

	count := 0
	for _, name := range files {
		if applied[name] {
			continue
		}

		content, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return count, fmt.Errorf("读取迁移脚本 %s 失败: %w", name, err)
		}
		if err := applyMigration(ctx, db, string(content)); err != nil {
			return count, fmt.Errorf("执行迁移脚本 %s 失败: %w", name, err)
		}
		if _, err := db.ExecContext(
			ctx,
			fmt.Sprintf("INSERT INTO %s (version, applied_at) VALUES ($1, $2)", migrationsTable),
			name, time.Now(),
		); err != nil {
			return count, fmt.Errorf("记录迁移版本 %s 失败: %w", name, err)
		}
		count++
	}
	return count, nil
}

// applyMigration 在一个事务里执行整个脚本。
//
// PostgreSQL 支持事务性 DDL，所以一个脚本要么全部生效、要么全部回滚。
// 这是相比 MySQL 的一处实际收益：MySQL 的 DDL 会隐式提交，脚本中途失败会留下半成品结构。
func applyMigration(ctx context.Context, db *sql.DB, script string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}

	for _, statement := range splitStatements(script) {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("执行语句失败: %w\n出错语句: %s", err, summarizeStatement(statement))
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}
	return nil
}

// splitStatements 把脚本按分号拆成独立语句。
//
// 之所以不「一次 Exec 多条语句」：PostgreSQL 的扩展协议不接受多语句，MySQL 则需要额外的
// multiStatements 参数。按语句执行对两边都成立，失败时也能直接定位到是哪一句出错。
//
// 限制：脚本里不要出现含分号的字符串字面量，也不要写 $$ ... $$ 函数体。
// 确实要写函数或触发器时，单独放一个只含该语句的迁移文件。
func splitStatements(script string) []string {
	var (
		statements    []string
		current       strings.Builder
		inLineComment bool
	)

	for i := 0; i < len(script); i++ {
		ch := script[i]
		switch {
		case inLineComment:
			// 行注释整段丢弃，遇到换行结束。
			if ch == '\n' {
				inLineComment = false
			}
		case ch == '-' && i+1 < len(script) && script[i+1] == '-':
			inLineComment = true
			i++
		case ch == ';':
			if statement := strings.TrimSpace(current.String()); statement != "" {
				statements = append(statements, statement)
			}
			current.Reset()
		default:
			current.WriteByte(ch)
		}
	}

	if statement := strings.TrimSpace(current.String()); statement != "" {
		statements = append(statements, statement)
	}
	return statements
}

// summarizeStatement 把出错语句压成单行短串，便于日志阅读。
func summarizeStatement(statement string) string {
	flat := strings.Join(strings.Fields(statement), " ")
	const maxRunes = 120
	if len([]rune(flat)) > maxRunes {
		return string([]rune(flat)[:maxRunes]) + "..."
	}
	return flat
}

// ensureMigrationsTable 创建版本记录表（幂等）。
func ensureMigrationsTable(ctx context.Context, db *sql.DB) error {
	stmt := fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
  version    VARCHAR(255) NOT NULL PRIMARY KEY,
  applied_at TIMESTAMPTZ  NOT NULL
)`, migrationsTable)
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("创建 %s 表失败: %w", migrationsTable, err)
	}
	return nil
}

// appliedVersions 返回已应用的迁移版本集合。
func appliedVersions(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("SELECT version FROM %s", migrationsTable))
	if err != nil {
		return nil, fmt.Errorf("查询迁移版本失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	applied := make(map[string]bool)
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("读取迁移版本失败: %w", err)
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历迁移版本失败: %w", err)
	}
	return applied, nil
}

// migrationFiles 返回按文件名排序的迁移脚本名，保证执行顺序稳定。
func migrationFiles() ([]string, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("读取 migrations 目录失败: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}
