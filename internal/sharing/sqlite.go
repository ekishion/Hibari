package sharing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ekishion/hibari/internal/sqlite"
)

// SQLStore 用 SQLite 实现 Store。
type SQLStore struct {
	db *sqlite.DB
}

func NewSQLStore(db *sqlite.DB) *SQLStore {
	return &SQLStore{db: db}
}

const shareColumns = `token, kind, path, name, password_hash, max_uses, uses, expires_at, created_at`

func (s *SQLStore) CreateShare(ctx context.Context, sh Share) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO shares (`+shareColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sh.Token, string(sh.Kind), sh.Path, sh.Name, sh.PasswordHash,
		sh.MaxUses, sh.Uses,
		sh.ExpiresAt.Format(time.RFC3339), sh.CreatedAt.Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("创建分享: %w", err)
	}
	return nil
}

func (s *SQLStore) ShareByToken(ctx context.Context, token string) (Share, error) {
	return scanShare(s.db.QueryRowContext(ctx,
		`SELECT `+shareColumns+` FROM shares WHERE token = ?`, token))
}

// ConsumeShare 在单条 UPDATE 中完成次数校验与扣减，
// 避免「先读后写」的竞态。无上限的分享（max_uses=0）直接加一。
func (s *SQLStore) ConsumeShare(ctx context.Context, token string) (Share, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE shares SET uses = uses + 1
		 WHERE token = ? AND (max_uses = 0 OR uses < max_uses)`, token)
	if err != nil {
		return Share{}, fmt.Errorf("扣减分享次数: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// 可能是 token 不存在，也可能刚好耗尽，交由调用方区分。
		if _, err := s.ShareByToken(ctx, token); err != nil {
			return Share{}, err
		}
		return Share{}, ErrExhausted
	}
	return s.ShareByToken(ctx, token)
}

func (s *SQLStore) ListShares(ctx context.Context) ([]Share, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+shareColumns+` FROM shares ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("列出分享: %w", err)
	}
	defer rows.Close()

	var out []Share
	for rows.Next() {
		sh, err := scanShare(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sh)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历分享: %w", err)
	}
	if out == nil {
		out = []Share{}
	}
	return out, nil
}

func (s *SQLStore) DeleteShare(ctx context.Context, token string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM shares WHERE token = ?`, token)
	if err != nil {
		return fmt.Errorf("删除分享: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// scanner 同时适配 *sql.Row 与 *sql.Rows。
type scanner interface {
	Scan(dest ...any) error
}

func scanShare(row scanner) (Share, error) {
	var sh Share
	var kind, expires, created string
	err := row.Scan(&sh.Token, &kind, &sh.Path, &sh.Name, &sh.PasswordHash,
		&sh.MaxUses, &sh.Uses, &expires, &created)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Share{}, ErrNotFound
		}
		return Share{}, fmt.Errorf("读取分享: %w", err)
	}
	sh.Kind = Kind(kind)
	sh.ExpiresAt, _ = time.Parse(time.RFC3339, expires)
	sh.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return sh, nil
}

var _ Store = (*SQLStore)(nil)
