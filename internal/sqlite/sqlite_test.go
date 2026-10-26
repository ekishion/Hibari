package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func openTest(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestOpenAppliesMigrations(t *testing.T) {
	db := openTest(t)

	for _, table := range []string{"users", "sessions", "shares", "audit"} {
		var name string
		err := db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
		if err != nil {
			t.Errorf("表 %s 不存在: %v", table, err)
		}
	}

	var version int
	if err := db.QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&version); err != nil {
		t.Fatalf("读取版本: %v", err)
	}
	if version != 1 {
		t.Errorf("schema 版本 = %d，期望 1", version)
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	for i := 0; i < 2; i++ {
		db, err := Open(path)
		if err != nil {
			t.Fatalf("第 %d 次 Open: %v", i+1, err)
		}
		db.Close()
	}
}

func TestOpenCreatesParentDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "deep", "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
}

func TestCleanupRemovesExpiredRows(t *testing.T) {
	db := openTest(t)
	ctx := context.Background()

	now := time.Now().UTC()
	past := now.Add(-time.Hour).Format(time.RFC3339)
	future := now.Add(time.Hour).Format(time.RFC3339)
	old := now.Add(-48 * time.Hour).Format(time.RFC3339)

	if _, err := db.ExecContext(ctx,
		`INSERT INTO users (id, username, password_hash, role, disabled, created_at)
		 VALUES ('u1', 'a', 'x', 'admin', 0, ?)`, past); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct{ hash, expires string }{
		{"h-old", past}, {"h-new", future},
	} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO sessions (token_hash, user_id, user_agent, client_ip, expires_at, created_at)
			 VALUES (?, 'u1', '', '', ?, ?)`, row.hash, row.expires, past); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []struct{ token, expires string }{
		{"t-old", past}, {"t-new", future},
	} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO shares (token, kind, path, name, expires_at, created_at)
			 VALUES (?, 'file', '/a', 'a', ?, ?)`, row.token, row.expires, past); err != nil {
			t.Fatal(err)
		}
	}
	for _, created := range []string{old, now.Format(time.RFC3339)} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO audit (event, path, client_ip, user_agent, created_at)
			 VALUES ('login', '', '', '', ?)`, created); err != nil {
			t.Fatal(err)
		}
	}

	if err := db.Cleanup(ctx, 24*time.Hour); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}

	for _, q := range []struct {
		name string
		sql  string
		want int
	}{
		{"未过期会话", `SELECT COUNT(*) FROM sessions`, 1},
		{"未过期分享", `SELECT COUNT(*) FROM shares`, 1},
		{"保留期内审计", `SELECT COUNT(*) FROM audit`, 1},
		{"用户不受清理影响", `SELECT COUNT(*) FROM users`, 1},
	} {
		var n int
		if err := db.QueryRow(q.sql).Scan(&n); err != nil {
			t.Fatalf("%s: %v", q.name, err)
		}
		if n != q.want {
			t.Errorf("%s = %d，期望 %d", q.name, n, q.want)
		}
	}
}
