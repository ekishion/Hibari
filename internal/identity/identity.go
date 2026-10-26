// Package identity 负责用户、密码与会话。它是唯一决定「谁在访问」的地方。
package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

// 领域错误。
var (
	ErrInvalidCredentials = errors.New("用户名或密码错误")
	ErrDisabled           = errors.New("账号已禁用")
	ErrWeakPassword       = errors.New("密码长度必须在 8 到 128 个字符之间")
	ErrSessionExpired     = errors.New("会话已过期")
	ErrNoUser             = errors.New("用户不存在")
	ErrAlreadySetup       = errors.New("系统已完成初始化")
)

// User 是一个可登录的账号。当前产品只支持单个管理员，
// 但表结构保留了多用户扩展空间。
type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	Role         string    `json:"role"`
	Disabled     bool      `json:"disabled"`
	CreatedAt    time.Time `json:"created_at"`
}

// RoleAdmin 是当前唯一的角色。访客不走角色体系，走分享链接。
const RoleAdmin = "admin"

// Store 是持久化契约，由消费方（本包）声明，由 sqlite 实现。
type Store interface {
	// CreateFirstAdmin 仅在 users 表为空时插入，返回是否插入成功。
	// 「检查为空」与「插入」必须在同一条语句内完成，否则并发初始化会创建多个管理员。
	CreateFirstAdmin(ctx context.Context, u User) (bool, error)
	UserByName(ctx context.Context, username string) (User, error)
	UserByID(ctx context.Context, id string) (User, error)
	UpdateUserPassword(ctx context.Context, id, hash string) error
	CountUsers(ctx context.Context) (int, error)

	CreateSession(ctx context.Context, s Session) error
	SessionByHash(ctx context.Context, hash string) (Session, error)
	DeleteSession(ctx context.Context, hash string) error
	DeleteUserSessions(ctx context.Context, userID string) error
}

// Session 是一次登录产生的服务端记录。客户端只持有不透明令牌。
type Session struct {
	TokenHash string
	UserID    string
	UserAgent string
	ClientIP  string
	ExpiresAt time.Time
	CreatedAt time.Time
}

// Service 提供认证与用户管理用例。
type Service struct {
	store Store
	ttl   time.Duration
}

func New(store Store, sessionTTL time.Duration) *Service {
	if sessionTTL <= 0 {
		sessionTTL = 7 * 24 * time.Hour
	}
	return &Service{store: store, ttl: sessionTTL}
}

// NeedsSetup 报告系统是否尚未创建任何账号。
func (s *Service) NeedsSetup(ctx context.Context) (bool, error) {
	n, err := s.store.CountUsers(ctx)
	if err != nil {
		return false, err
	}
	return n == 0, nil
}

// CreateAdmin 创建唯一的管理员账号。系统已有账号时返回 ErrAlreadySetup。
func (s *Service) CreateAdmin(ctx context.Context, username, password string) (User, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return User{}, fmt.Errorf("用户名不能为空")
	}
	if err := ValidatePassword(password); err != nil {
		return User{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	u := User{
		ID:           "u-" + randomHex(8),
		Username:     username,
		PasswordHash: hash,
		Role:         RoleAdmin,
		CreatedAt:    time.Now().UTC(),
	}
	ok, err := s.store.CreateFirstAdmin(ctx, u)
	if err != nil {
		return User{}, err
	}
	if !ok {
		return User{}, ErrAlreadySetup
	}
	return u, nil
}

// Login 校验凭据并创建会话，返回明文令牌与用户。
// 令牌只在此处出现一次，服务端仅存哈希。
func (s *Service) Login(ctx context.Context, username, password, ua, ip string) (string, User, error) {
	u, err := s.verifyCredentials(ctx, username, password)
	if err != nil {
		return "", User{}, err
	}

	token := randomHex(32)
	now := time.Now().UTC()
	sess := Session{
		TokenHash: hashToken(token),
		UserID:    u.ID,
		UserAgent: ua,
		ClientIP:  ip,
		ExpiresAt: now.Add(s.ttl),
		CreatedAt: now,
	}
	if err := s.store.CreateSession(ctx, sess); err != nil {
		return "", User{}, err
	}
	return token, u, nil
}

// VerifyCredentials 只校验凭据，不创建会话。用于改密等需要重认证的场景。
func (s *Service) VerifyCredentials(ctx context.Context, username, password string) (User, error) {
	return s.verifyCredentials(ctx, username, password)
}

func (s *Service) verifyCredentials(ctx context.Context, username, password string) (User, error) {
	u, err := s.store.UserByName(ctx, strings.TrimSpace(username))
	if err != nil {
		if errors.Is(err, ErrNoUser) {
			// 与密码错误返回同一个错误，避免用户名枚举。
			return User{}, ErrInvalidCredentials
		}
		return User{}, err
	}
	if u.Disabled {
		return User{}, ErrDisabled
	}
	ok, err := VerifyPassword(password, u.PasswordHash)
	if err != nil || !ok {
		return User{}, ErrInvalidCredentials
	}
	return u, nil
}

// Authenticate 用明文令牌换回用户。令牌无效或过期时返回 ErrSessionExpired。
func (s *Service) Authenticate(ctx context.Context, token string) (User, error) {
	if token == "" {
		return User{}, ErrSessionExpired
	}
	sess, err := s.store.SessionByHash(ctx, hashToken(token))
	if err != nil {
		if errors.Is(err, ErrSessionExpired) || errors.Is(err, ErrNoUser) {
			return User{}, ErrSessionExpired
		}
		return User{}, err
	}
	if !time.Now().UTC().Before(sess.ExpiresAt) {
		_ = s.store.DeleteSession(ctx, sess.TokenHash)
		return User{}, ErrSessionExpired
	}
	u, err := s.store.UserByID(ctx, sess.UserID)
	if err != nil {
		if errors.Is(err, ErrNoUser) {
			return User{}, ErrSessionExpired
		}
		return User{}, err
	}
	if u.Disabled {
		return User{}, ErrDisabled
	}
	return u, nil
}

// Logout 删除会话记录，令牌立即失效。
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.store.DeleteSession(ctx, hashToken(token))
}

// ChangePassword 更新密码并吊销该用户的全部会话。
func (s *Service) ChangePassword(ctx context.Context, userID, newPassword string) error {
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	if err := s.store.UpdateUserPassword(ctx, userID, hash); err != nil {
		return err
	}
	return s.store.DeleteUserSessions(ctx, userID)
}

// ---- 密码散列 ----

// Argon2id 参数取 OWASP 推荐值。
const (
	argonTime    = 3
	argonMemory  = 64 * 1024
	argonThreads = 2
	argonKeyLen  = 32
	argonSaltLen = 16
)

func ValidatePassword(pw string) error {
	n := len([]rune(pw))
	if n < 8 || n > 128 {
		return ErrWeakPassword
	}
	return nil
}

// HashPassword 返回 PHC 格式的 Argon2id 散列。
func HashPassword(pw string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("生成盐: %w", err)
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword 解析 PHC 串并做恒定时间比较。
func VerifyPassword(pw, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, fmt.Errorf("散列格式无法识别")
	}
	var mem, iter uint32
	var par uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &iter, &par); err != nil {
		return false, fmt.Errorf("解析散列参数: %w", err)
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("解码盐: %w", err)
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, fmt.Errorf("解码散列: %w", err)
	}
	got := argon2.IDKey([]byte(pw), salt, iter, mem, par, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// hashToken 对会话令牌做 SHA-256。令牌本身是高熵随机串，
// 无需加盐或慢哈希。
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand 失败意味着系统熵源不可用，此时无法安全运行。
		panic("crypto/rand 不可用: " + err.Error())
	}
	return hex.EncodeToString(buf)
}
