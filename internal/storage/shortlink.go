package storage

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"
)

var ShortLinkTokenRe = regexp.MustCompile(`^[a-f0-9]{32}$`)

func HashSharePassword(token, password string) string {
	password = strings.ToLower(strings.TrimSpace(password))
	if password == "" {
		return ""
	}
	h := sha256.Sum256([]byte(token + ":" + password))
	return hex.EncodeToString(h[:])
}

func VerifySharePassword(token, password, expectedHash string) bool {
	if expectedHash == "" {
		return true
	}
	password = strings.ToLower(strings.TrimSpace(password))
	if password == "" {
		return false
	}
	actualHash := HashSharePassword(token, password)
	return subtle.ConstantTimeCompare([]byte(actualHash), []byte(expectedHash)) == 1
}

type ShortLink struct {
	FilePath     string
	ExpiresAt    time.Time
	CreatedAt    time.Time
	MaxUses      int // 0 = unlimited
	Uses         int
	PasswordHash string
}

type ShortLinkStore struct {
	mu         sync.Mutex
	links      map[string]ShortLink
	ttl        time.Duration
	maxEntries int
	maxUses    int
}

const maxShortLinks = 10000

func NewShortLinkStore(ttl time.Duration, maxUses int) *ShortLinkStore {
	return NewShortLinkStoreWithLimits(ttl, maxShortLinks, maxUses)
}

func NewShortLinkStoreWithLimits(ttl time.Duration, maxEntries, maxUses int) *ShortLinkStore {
	if ttl <= 0 {
		ttl = time.Hour
	}
	if maxEntries < 1 {
		maxEntries = maxShortLinks
	}
	if maxUses < 0 {
		maxUses = 0
	}
	return &ShortLinkStore{
		links:      make(map[string]ShortLink),
		ttl:        ttl,
		maxEntries: maxEntries,
		maxUses:    maxUses,
	}
}

func (s *ShortLinkStore) Create(filePath string) (string, error) {
	return s.CreateWithOptions(filePath, "", 0, -1)
}

func (s *ShortLinkStore) CreateWithOptions(filePath string, password string, customTTL time.Duration, customMaxUses int) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for token, link := range s.links {
		if !now.Before(link.ExpiresAt) {
			delete(s.links, token)
		}
	}
	for len(s.links) >= s.maxEntries {
		evicted := EvictOldestSampled(s.links, func(link ShortLink) time.Time {
			return link.CreatedAt
		}, EvictionSampleSize)
		if !evicted {
			break
		}
	}

	ttl := s.ttl
	if customTTL > 0 {
		ttl = customTTL
	}
	maxUses := s.maxUses
	if customMaxUses >= 0 {
		maxUses = customMaxUses
	}

	for i := 0; i < 3; i++ {
		buf := make([]byte, 16)
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("生成短链接令牌失败: %w", err)
		}
		token := hex.EncodeToString(buf)
		if _, exists := s.links[token]; exists {
			continue
		}
		s.links[token] = ShortLink{
			FilePath:     filePath,
			ExpiresAt:    now.Add(ttl),
			CreatedAt:    now,
			MaxUses:      maxUses,
			PasswordHash: HashSharePassword(token, password),
		}
		return token, nil
	}
	return "", fmt.Errorf("生成短链接令牌失败: 随机令牌冲突")
}

// Resolve 校验并返回文件路径（仅适用于无密码短链）。consume=true 时计入使用次数。
func (s *ShortLinkStore) Resolve(token string, consume bool) (string, bool) {
	path, ok, _ := s.ResolveWithPassword(token, "", consume)
	return path, ok
}

// ResolveWithPassword 校验并返回文件路径。返回 (filePath, ok, passwordRequired)。
// 若短链需要密码且未提供或密码错误，返回 ("", false, true)。
func (s *ShortLinkStore) ResolveWithPassword(token string, password string, consume bool) (string, bool, bool) {
	if !ShortLinkTokenRe.MatchString(token) {
		return "", false, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	link, ok := s.links[token]
	if !ok {
		return "", false, false
	}
	if !time.Now().Before(link.ExpiresAt) {
		delete(s.links, token)
		return "", false, false
	}
	if link.MaxUses > 0 && link.Uses >= link.MaxUses {
		delete(s.links, token)
		return "", false, false
	}

	if link.PasswordHash != "" {
		if password == "" || !VerifySharePassword(token, password, link.PasswordHash) {
			return "", false, true
		}
	}

	if consume {
		link.Uses++
		if link.MaxUses > 0 && link.Uses >= link.MaxUses {
			delete(s.links, token)
		} else {
			s.links[token] = link
		}
	}
	return link.FilePath, true, false
}

// ShortLinkMeta 短链接对外元数据（用于单文件落地页展示）
type ShortLinkMeta struct {
	Token       string    `json:"token"`
	FilePath    string    `json:"file_path"`
	FileName    string    `json:"file_name"`
	HasPassword bool      `json:"has_password"`
	ExpiresAt   time.Time `json:"expires_at"`
	CreatedAt   time.Time `json:"created_at"`
	MaxUses     int       `json:"max_uses"`
	Uses        int       `json:"uses"`
}

// GetMeta 获取短链元数据（不校验密码、不消耗次数），若短链不存在或已过期/超次返回 false。
func (s *ShortLinkStore) GetMeta(token string) (ShortLinkMeta, bool) {
	if !ShortLinkTokenRe.MatchString(token) {
		return ShortLinkMeta{}, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	link, ok := s.links[token]
	if !ok {
		return ShortLinkMeta{}, false
	}
	if !time.Now().Before(link.ExpiresAt) {
		delete(s.links, token)
		return ShortLinkMeta{}, false
	}
	if link.MaxUses > 0 && link.Uses >= link.MaxUses {
		delete(s.links, token)
		return ShortLinkMeta{}, false
	}

	return ShortLinkMeta{
		Token:       token,
		FilePath:    link.FilePath,
		FileName:    path.Base(link.FilePath),
		HasPassword: link.PasswordHash != "",
		ExpiresAt:   link.ExpiresAt,
		CreatedAt:   link.CreatedAt,
		MaxUses:     link.MaxUses,
		Uses:        link.Uses,
	}, true
}

// Revoke 撤销指定短链接。若令牌存在并被成功撤销返回 true，否则返回 false。
func (s *ShortLinkStore) Revoke(token string) bool {
	if !ShortLinkTokenRe.MatchString(token) {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.links[token]; ok {
		delete(s.links, token)
		return true
	}
	return false
}

// List 列出当前所有未过期的活跃短链接元数据。
func (s *ShortLinkStore) List() []ShortLinkMeta {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	out := make([]ShortLinkMeta, 0, len(s.links))
	for token, link := range s.links {
		if !now.Before(link.ExpiresAt) {
			delete(s.links, token)
			continue
		}
		if link.MaxUses > 0 && link.Uses >= link.MaxUses {
			delete(s.links, token)
			continue
		}
		out = append(out, ShortLinkMeta{
			Token:       token,
			FilePath:    link.FilePath,
			FileName:    path.Base(link.FilePath),
			HasPassword: link.PasswordHash != "",
			ExpiresAt:   link.ExpiresAt,
			CreatedAt:   link.CreatedAt,
			MaxUses:     link.MaxUses,
			Uses:        link.Uses,
		})
	}
	return out
}

// Snapshot 导出未过期且仍有剩余次数的条目用于落盘。
func (s *ShortLinkStore) Snapshot() []PersistedLink {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	out := make([]PersistedLink, 0, len(s.links))
	for token, link := range s.links {
		if !now.Before(link.ExpiresAt) {
			continue
		}
		if link.MaxUses > 0 && link.Uses >= link.MaxUses {
			continue
		}
		out = append(out, PersistedLink{
			Token:        token,
			Path:         link.FilePath,
			ExpiresAt:    link.ExpiresAt.UnixNano(),
			CreatedAt:    link.CreatedAt.UnixNano(),
			Uses:         link.Uses,
			PasswordHash: link.PasswordHash,
			MaxUses:      link.MaxUses,
		})
	}
	return out
}

// Restore 从落盘快照恢复；过期、超次或格式非法的条目直接丢弃。
func (s *ShortLinkStore) Restore(links []PersistedLink) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for _, l := range links {
		if !ShortLinkTokenRe.MatchString(l.Token) {
			continue
		}
		expiresAt := time.Unix(0, l.ExpiresAt)
		if !now.Before(expiresAt) {
			continue
		}
		maxUses := s.maxUses
		if l.MaxUses > 0 {
			maxUses = l.MaxUses
		}
		if maxUses > 0 && l.Uses >= maxUses {
			continue
		}
		s.links[l.Token] = ShortLink{
			FilePath:     l.Path,
			ExpiresAt:    expiresAt,
			CreatedAt:    time.Unix(0, l.CreatedAt),
			MaxUses:      maxUses,
			Uses:         l.Uses,
			PasswordHash: l.PasswordHash,
		}
	}
}
