package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ekishion/jikeqingpan/internal/config"
	// 纯 Go SQLite 驱动（无 CGO），仅在 state_backend=sqlite 时实际使用。
	_ "modernc.org/sqlite"
)

// PersistedLink 持久化的短链条目（目录短链与下载短链共用结构）。
type PersistedLink struct {
	Token        string `json:"token"`
	Path         string `json:"path"`
	ExpiresAt    int64  `json:"expires_at"` // unix nano
	CreatedAt    int64  `json:"created_at"` // unix nano
	Uses         int    `json:"uses,omitempty"`
	PasswordHash string `json:"password_hash,omitempty"`
	MaxUses      int    `json:"max_uses,omitempty"`
}

// PersistedDirCache 持久化的目录列表缓存条目。
type PersistedDirCache struct {
	Dir      string          `json:"dir"`
	Body     json.RawMessage `json:"body"`
	CachedAt int64           `json:"cached_at"` // unix 秒级时间戳
}

// PersistedState 落盘状态快照。
type PersistedState struct {
	Version    int                 `json:"version"`
	DirLinks   []PersistedLink     `json:"dir_links,omitempty"`
	ShortLinks []PersistedLink     `json:"short_links,omitempty"`
	DirCache   []PersistedDirCache `json:"dir_cache,omitempty"`
}

// StatePersistence 状态持久化后端。实现必须保证 Save 的原子性：
// 要么完整写入旧快照，要么完整写入新快照，不允许半份状态。
type StatePersistence interface {
	Load() (*PersistedState, error)
	Save(*PersistedState) error
	Close() error
}

// NewStatePersistence 按配置构造后端；state_backend=none 时返回 (nil, nil)。
func NewStatePersistence(cfg *config.Config) (StatePersistence, error) {
	if cfg == nil {
		return nil, nil
	}
	switch cfg.StateBackend {
	case "sqlite":
		return NewSQLitePersistence(cfg.StatePath)
	default:
		return nil, nil
	}
}

// SQLitePersistence 以 SQLite 为 durable KV：state 表每行存一段 JSON 快照
// （dir_links / short_links）。单写者场景，WAL + 单连接规避锁竞争。
type SQLitePersistence struct {
	db *sql.DB
}

func NewSQLitePersistence(path string) (*SQLitePersistence, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("初始化 SQLite %q: %w", path, err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("初始化 SQLite %q: %w", path, err)
	}
	if _, err := db.Exec("PRAGMA synchronous=NORMAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("初始化 SQLite %q: %w", path, err)
	}
	if _, err := db.Exec("CREATE TABLE IF NOT EXISTS state (key TEXT PRIMARY KEY, value BLOB NOT NULL, updated_at INTEGER NOT NULL)"); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &SQLitePersistence{db: db}, nil
}

func (p *SQLitePersistence) Load() (*PersistedState, error) {
	st := &PersistedState{Version: 1}

	var dirRaw []byte
	err := p.db.QueryRow("SELECT value FROM state WHERE key = ?", "dir_links").Scan(&dirRaw)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(dirRaw, &st.DirLinks); err != nil {
			return nil, fmt.Errorf("状态行 dir_links 损坏: %w", err)
		}
	}

	var shortRaw []byte
	err = p.db.QueryRow("SELECT value FROM state WHERE key = ?", "short_links").Scan(&shortRaw)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(shortRaw, &st.ShortLinks); err != nil {
			return nil, fmt.Errorf("状态行 short_links 损坏: %w", err)
		}
	}

	var dirCacheRaw []byte
	err = p.db.QueryRow("SELECT value FROM state WHERE key = ?", "dir_cache").Scan(&dirCacheRaw)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(dirCacheRaw, &st.DirCache); err != nil {
			return nil, fmt.Errorf("状态行 dir_cache 损坏: %w", err)
		}
	}
	return st, nil
}

func (p *SQLitePersistence) Save(st *PersistedState) error {
	tx, err := p.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	dirRaw, err := json.Marshal(st.DirLinks)
	if err != nil {
		return err
	}
	shortRaw, err := json.Marshal(st.ShortLinks)
	if err != nil {
		return err
	}
	dirCacheRaw, err := json.Marshal(st.DirCache)
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	if _, err := tx.Exec("INSERT OR REPLACE INTO state (key, value, updated_at) VALUES (?, ?, ?)", "dir_links", dirRaw, now); err != nil {
		return err
	}
	if _, err := tx.Exec("INSERT OR REPLACE INTO state (key, value, updated_at) VALUES (?, ?, ?)", "short_links", shortRaw, now); err != nil {
		return err
	}
	if _, err := tx.Exec("INSERT OR REPLACE INTO state (key, value, updated_at) VALUES (?, ?, ?)", "dir_cache", dirCacheRaw, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (p *SQLitePersistence) Close() error {
	if p == nil || p.db == nil {
		return nil
	}
	return p.db.Close()
}
