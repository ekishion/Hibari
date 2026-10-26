// Package sqlite 提供数据库连接与迁移。它不知道任何业务表结构之外的东西。
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// DB 包装 *sql.DB，提供本项目需要的少量辅助方法。
type DB struct {
	*sql.DB
}

// Open 打开数据库并应用迁移。DSN 为文件路径；":memory:" 表示内存库。
func Open(dsn string) (*DB, error) {
	if dsn != ":memory:" {
		if dir := filepath.Dir(dsn); dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0o750); err != nil {
				return nil, fmt.Errorf("创建数据目录: %w", err)
			}
		}
	}

	// WAL 提升并发读性能；busy_timeout 避免瞬时锁冲突直接报错。
	sqlDB, err := sql.Open("sqlite", dsn+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("打开数据库: %w", err)
	}
	// SQLite 单写者模型，限制连接数避免写锁竞争。
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetConnMaxLifetime(time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("连接数据库: %w", err)
	}

	db := &DB{DB: sqlDB}
	if err := db.migrate(ctx); err != nil {
		sqlDB.Close()
		return nil, err
	}
	return db, nil
}

// migrate 按版本号顺序应用 schema。每个版本在独立事务中执行，
// 失败即回滚，不会留下半张表。
func (db *DB) migrate(ctx context.Context) error {
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_version (
			version    INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL
		)`); err != nil {
		return fmt.Errorf("创建版本表: %w", err)
	}

	var current int
	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&current); err != nil {
		return fmt.Errorf("读取当前版本: %w", err)
	}

	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("开始迁移 v%d: %w", m.version, err)
		}
		for _, stmt := range m.statements {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				tx.Rollback()
				return fmt.Errorf("迁移 v%d 失败: %w", m.version, err)
			}
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_version (version, applied_at) VALUES (?, ?)`,
			m.version, time.Now().UTC().Format(time.RFC3339)); err != nil {
			tx.Rollback()
			return fmt.Errorf("记录迁移 v%d: %w", m.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("提交迁移 v%d: %w", m.version, err)
		}
	}
	return nil
}

type migration struct {
	version    int
	statements []string
}

// migrations 是 schema 的唯一真相源。只追加，不修改已发布的版本。
var migrations = []migration{
	{
		version: 1,
		statements: []string{
			`CREATE TABLE IF NOT EXISTS users (
				id            TEXT PRIMARY KEY,
				username      TEXT NOT NULL UNIQUE,
				password_hash TEXT NOT NULL,
				role          TEXT NOT NULL DEFAULT 'admin',
				disabled      INTEGER NOT NULL DEFAULT 0,
				created_at    TEXT NOT NULL
			)`,

			`CREATE TABLE IF NOT EXISTS sessions (
				token_hash TEXT PRIMARY KEY,
				user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
				user_agent TEXT NOT NULL DEFAULT '',
				client_ip  TEXT NOT NULL DEFAULT '',
				expires_at TEXT NOT NULL,
				created_at TEXT NOT NULL
			)`,
			`CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id)`,
			`CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at)`,

			`CREATE TABLE IF NOT EXISTS shares (
				token         TEXT PRIMARY KEY,
				kind          TEXT NOT NULL,
				path          TEXT NOT NULL,
				name          TEXT NOT NULL,
				password_hash TEXT NOT NULL DEFAULT '',
				max_uses      INTEGER NOT NULL DEFAULT 0,
				uses          INTEGER NOT NULL DEFAULT 0,
				expires_at    TEXT NOT NULL,
				created_at    TEXT NOT NULL
			)`,
			`CREATE INDEX IF NOT EXISTS idx_shares_expires ON shares(expires_at)`,

			`CREATE TABLE IF NOT EXISTS audit (
				id         INTEGER PRIMARY KEY AUTOINCREMENT,
				event      TEXT NOT NULL,
				path       TEXT NOT NULL DEFAULT '',
				client_ip  TEXT NOT NULL DEFAULT '',
				user_agent TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL
			)`,
			`CREATE INDEX IF NOT EXISTS idx_audit_created ON audit(created_at)`,
		},
	},
}

// Cleanup 删除过期的会话、分享与旧审计记录。
func (db *DB) Cleanup(ctx context.Context, auditRetention time.Duration) error {
	now := time.Now().UTC().Format(time.RFC3339)
	cutoff := time.Now().Add(-auditRetention).UTC().Format(time.RFC3339)

	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`DELETE FROM sessions WHERE expires_at < ?`, []any{now}},
		{`DELETE FROM shares WHERE expires_at < ?`, []any{now}},
		{`DELETE FROM audit WHERE created_at < ?`, []any{cutoff}},
	} {
		if _, err := db.ExecContext(ctx, q.sql, q.args...); err != nil {
			return fmt.Errorf("清理数据: %w", err)
		}
	}
	return nil
}
