// Package sharing 负责分享短链：创建、校验、撤销与提取码。
package sharing

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Kind 区分单文件分享与目录分享。两者语义不同：
// 文件分享一次性定位到一个文件；目录分享会建立浏览沙盒。
type Kind string

const (
	KindFile Kind = "file"
	KindDir  Kind = "dir"
)

// 领域错误。
var (
	ErrNotFound       = errors.New("分享不存在或已失效")
	ErrPasswordNeeded = errors.New("需要提取码")
	ErrPasswordWrong  = errors.New("提取码错误")
	ErrExhausted      = errors.New("分享已达使用上限")
)

// Share 是一条分享记录。PasswordHash 为空表示无提取码。
type Share struct {
	Token        string    `json:"token"`
	Kind         Kind      `json:"kind"`
	Path         string    `json:"path"`
	Name         string    `json:"name"`
	PasswordHash string    `json:"-"`
	MaxUses      int       `json:"max_uses"`
	Uses         int       `json:"uses"`
	ExpiresAt    time.Time `json:"expires_at"`
	CreatedAt    time.Time `json:"created_at"`
}

// HasPassword 报告是否需要提取码。
func (s Share) HasPassword() bool { return s.PasswordHash != "" }

// Expired 报告是否已过期。
func (s Share) Expired(now time.Time) bool { return !now.Before(s.ExpiresAt) }

// Store 是持久化契约，由本包声明。
type Store interface {
	CreateShare(ctx context.Context, s Share) error
	ShareByToken(ctx context.Context, token string) (Share, error)
	// ConsumeShare 原子地校验并扣减使用次数，返回扣减后的记录。
	ConsumeShare(ctx context.Context, token string) (Share, error)
	ListShares(ctx context.Context) ([]Share, error)
	DeleteShare(ctx context.Context, token string) error
}

// Service 提供分享用例。
type Service struct {
	store Store
	now   func() time.Time
}

func New(store Store) *Service {
	return &Service{store: store, now: time.Now}
}

// CreateOptions 是创建分享的参数。
type CreateOptions struct {
	Kind     Kind
	Path     string
	Name     string
	Password string
	TTL      time.Duration
	MaxUses  int
}

// Create 生成短链。令牌为 32 位十六进制随机串。
func (s *Service) Create(ctx context.Context, opts CreateOptions) (Share, error) {
	if opts.Kind != KindFile && opts.Kind != KindDir {
		return Share{}, fmt.Errorf("未知分享类型: %s", opts.Kind)
	}
	if opts.TTL <= 0 {
		opts.TTL = 24 * time.Hour
	}
	now := s.now().UTC()

	sh := Share{
		Token:     randomToken(),
		Kind:      opts.Kind,
		Path:      opts.Path,
		Name:      opts.Name,
		MaxUses:   opts.MaxUses,
		ExpiresAt: now.Add(opts.TTL),
		CreatedAt: now,
	}
	if pw := strings.TrimSpace(opts.Password); pw != "" {
		sh.PasswordHash = hashPassword(sh.Token, pw)
	}
	if err := s.store.CreateShare(ctx, sh); err != nil {
		return Share{}, err
	}
	return sh, nil
}

// Resolve 校验提取码并返回分享，不改变任何状态。
// 需要计次的调用方在范围校验全部通过后再显式调用 Consume。
func (s *Service) Resolve(ctx context.Context, token, password string) (Share, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return Share{}, ErrNotFound
	}
	sh, err := s.store.ShareByToken(ctx, token)
	if err != nil {
		return Share{}, err
	}
	if sh.Expired(s.now()) {
		return Share{}, ErrNotFound
	}
	if sh.MaxUses > 0 && sh.Uses >= sh.MaxUses {
		return Share{}, ErrExhausted
	}
	if sh.HasPassword() {
		if password == "" {
			return Share{}, ErrPasswordNeeded
		}
		if !verifyPassword(token, password, sh.PasswordHash) {
			return Share{}, ErrPasswordWrong
		}
	}
	return sh, nil
}

// Consume 扣减一次使用次数并返回扣减后的分享。
// 底层是一条带条件的 UPDATE，并发请求不会超发。
func (s *Service) Consume(ctx context.Context, token string) (Share, error) {
	return s.store.ConsumeShare(ctx, token)
}

// List 返回全部未过期分享。
func (s *Service) List(ctx context.Context) ([]Share, error) {
	all, err := s.store.ListShares(ctx)
	if err != nil {
		return nil, err
	}
	now := s.now()
	alive := make([]Share, 0, len(all))
	for _, sh := range all {
		if !sh.Expired(now) {
			alive = append(alive, sh)
		}
	}
	return alive, nil
}

// Revoke 删除分享。
func (s *Service) Revoke(ctx context.Context, token string) error {
	return s.store.DeleteShare(ctx, token)
}

func randomToken() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		panic("crypto/rand 不可用: " + err.Error())
	}
	return hex.EncodeToString(buf)
}

// hashPassword 把提取码与令牌绑定，防止同一提取码在不同分享间复用散列。
func hashPassword(token, password string) string {
	sum := sha256.Sum256([]byte(token + ":" + strings.ToLower(strings.TrimSpace(password))))
	return hex.EncodeToString(sum[:])
}

func verifyPassword(token, password, want string) bool {
	got := hashPassword(token, password)
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}
