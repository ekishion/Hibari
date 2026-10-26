package identity

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

// CreateFirstAdmin 把「表为空」与「插入」合成一条语句，
// 并发初始化时只有一个请求能拿到 RowsAffected=1。
func (s *SQLStore) CreateFirstAdmin(ctx context.Context, u User) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (id, username, password_hash, role, disabled, created_at)
		 SELECT ?, ?, ?, ?, ?, ?
		 WHERE NOT EXISTS (SELECT 1 FROM users)`,
		u.ID, u.Username, u.PasswordHash, u.Role, boolToInt(u.Disabled), u.CreatedAt.Format(time.RFC3339))
	if err != nil {
		return false, fmt.Errorf("创建管理员: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("创建管理员: %w", err)
	}
	return n > 0, nil
}

func (s *SQLStore) UserByName(ctx context.Context, username string) (User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, role, disabled, created_at
		 FROM users WHERE username = ?`, username))
}

func (s *SQLStore) UserByID(ctx context.Context, id string) (User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, role, disabled, created_at
		 FROM users WHERE id = ?`, id))
}

func (s *SQLStore) scanUser(row *sql.Row) (User, error) {
	var u User
	var disabled int
	var created string
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &disabled, &created)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrNoUser
		}
		return User{}, fmt.Errorf("查询用户: %w", err)
	}
	u.Disabled = disabled != 0
	u.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return u, nil
}

func (s *SQLStore) UpdateUserPassword(ctx context.Context, id, hash string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET password_hash = ? WHERE id = ?`, hash, id)
	if err != nil {
		return fmt.Errorf("更新密码: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNoUser
	}
	return nil
}

func (s *SQLStore) CountUsers(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return 0, fmt.Errorf("统计用户: %w", err)
	}
	return n, nil
}

func (s *SQLStore) CreateSession(ctx context.Context, sess Session) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, user_agent, client_ip, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		sess.TokenHash, sess.UserID, sess.UserAgent, sess.ClientIP,
		sess.ExpiresAt.Format(time.RFC3339), sess.CreatedAt.Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("创建会话: %w", err)
	}
	return nil
}

func (s *SQLStore) SessionByHash(ctx context.Context, hash string) (Session, error) {
	var sess Session
	var expires, created string
	err := s.db.QueryRowContext(ctx,
		`SELECT token_hash, user_id, user_agent, client_ip, expires_at, created_at
		 FROM sessions WHERE token_hash = ?`, hash).
		Scan(&sess.TokenHash, &sess.UserID, &sess.UserAgent, &sess.ClientIP, &expires, &created)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Session{}, ErrSessionExpired
		}
		return Session{}, fmt.Errorf("查询会话: %w", err)
	}
	sess.ExpiresAt, _ = time.Parse(time.RFC3339, expires)
	sess.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return sess, nil
}

func (s *SQLStore) DeleteSession(ctx context.Context, hash string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hash); err != nil {
		return fmt.Errorf("删除会话: %w", err)
	}
	return nil
}

func (s *SQLStore) DeleteUserSessions(ctx context.Context, userID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("删除用户会话: %w", err)
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

var _ Store = (*SQLStore)(nil)
