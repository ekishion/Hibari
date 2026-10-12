package storage

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path"
	"regexp"
	"sync"
	"time"
)

// DirLink 目录短链存储条目
type DirLink struct {
	Dir          string
	ExpiresAt    time.Time
	CreatedAt    time.Time
	PasswordHash string
}

type DirLinkStore struct {
	mu         sync.Mutex
	links      map[string]DirLink
	ttl        time.Duration
	maxEntries int
}

var DirLinkTokenRe = regexp.MustCompile(`^[a-f0-9]{32}$`)

const maxDirLinks = 10000

func NewDirLinkStore(ttl time.Duration) *DirLinkStore {
	return NewDirLinkStoreWithLimits(ttl, maxDirLinks)
}

func NewDirLinkStoreWithLimits(ttl time.Duration, maxEntries int) *DirLinkStore {
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	if maxEntries < 1 {
		maxEntries = maxDirLinks
	}
	return &DirLinkStore{
		links:      make(map[string]DirLink),
		ttl:        ttl,
		maxEntries: maxEntries,
	}
}

// Create 为目录生成短链令牌；同目录重复创建会产生新令牌。
func (s *DirLinkStore) Create(dir string) (string, error) {
	return s.CreateWithOptions(dir, "", 0)
}

// CreateWithOptions 支持自定义提取码与有效期创建目录短链。
func (s *DirLinkStore) CreateWithOptions(dir string, password string, customTTL time.Duration) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for token, link := range s.links {
		if !now.Before(link.ExpiresAt) {
			delete(s.links, token)
		}
	}
	for len(s.links) >= s.maxEntries {
		evicted := EvictOldestSampled(s.links, func(link DirLink) time.Time {
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

	for i := 0; i < 3; i++ {
		buf := make([]byte, 16)
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("生成目录短链令牌失败: %w", err)
		}
		token := hex.EncodeToString(buf)
		if _, exists := s.links[token]; exists {
			continue
		}
		s.links[token] = DirLink{
			Dir:          dir,
			ExpiresAt:    now.Add(ttl),
			CreatedAt:    now,
			PasswordHash: HashSharePassword(token, password),
		}
		return token, nil
	}
	return "", fmt.Errorf("生成目录短链令牌失败: 随机令牌冲突")
}

// Resolve 返回令牌对应的目录（仅适用于无密码短链）；不校验路径权限。
func (s *DirLinkStore) Resolve(token string) (string, bool) {
	dir, ok, _ := s.ResolveWithPassword(token, "")
	return dir, ok
}

// ResolveWithPassword 校验并返回目录。返回 (dir, ok, passwordRequired)。
// 若短链需要提取码且密码不匹配，返回 ("", false, true)。
func (s *DirLinkStore) ResolveWithPassword(token string, password string) (string, bool, bool) {
	if !DirLinkTokenRe.MatchString(token) {
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

	if link.PasswordHash != "" {
		if password == "" || !VerifySharePassword(token, password, link.PasswordHash) {
			return "", false, true
		}
	}

	return link.Dir, true, false
}

// DirLinkMeta 目录短链接对外元数据（用于分享管理展示）
type DirLinkMeta struct {
	Token       string    `json:"token"`
	Dir         string    `json:"dir"`
	DirName     string    `json:"dir_name"`
	HasPassword bool      `json:"has_password"`
	ExpiresAt   time.Time `json:"expires_at"`
	CreatedAt   time.Time `json:"created_at"`
}

// Revoke 撤销指定目录短链接。若令牌存在并被成功撤销返回 true，否则返回 false。
func (s *DirLinkStore) Revoke(token string) bool {
	if !DirLinkTokenRe.MatchString(token) {
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

// List 列出当前所有未过期的活跃目录短链接元数据。
func (s *DirLinkStore) List() []DirLinkMeta {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	out := make([]DirLinkMeta, 0, len(s.links))
	for token, link := range s.links {
		if !now.Before(link.ExpiresAt) {
			delete(s.links, token)
			continue
		}
		dirName := path.Base(link.Dir)
		if dirName == "" || dirName == "/" || dirName == "." {
			dirName = "根目录"
		}
		out = append(out, DirLinkMeta{
			Token:       token,
			Dir:         link.Dir,
			DirName:     dirName,
			HasPassword: link.PasswordHash != "",
			ExpiresAt:   link.ExpiresAt,
			CreatedAt:   link.CreatedAt,
		})
	}
	return out
}

// Snapshot 导出未过期的条目用于落盘。
func (s *DirLinkStore) Snapshot() []PersistedLink {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	out := make([]PersistedLink, 0, len(s.links))
	for token, link := range s.links {
		if !now.Before(link.ExpiresAt) {
			continue
		}
		out = append(out, PersistedLink{
			Token:        token,
			Path:         link.Dir,
			ExpiresAt:    link.ExpiresAt.UnixNano(),
			CreatedAt:    link.CreatedAt.UnixNano(),
			PasswordHash: link.PasswordHash,
		})
	}
	return out
}

// Restore 从落盘快照恢复；过期或格式非法的条目直接丢弃。
func (s *DirLinkStore) Restore(links []PersistedLink) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for _, l := range links {
		if !DirLinkTokenRe.MatchString(l.Token) {
			continue
		}
		expiresAt := time.Unix(0, l.ExpiresAt)
		if !now.Before(expiresAt) {
			continue
		}
		s.links[l.Token] = DirLink{
			Dir:          l.Path,
			ExpiresAt:    expiresAt,
			CreatedAt:    time.Unix(0, l.CreatedAt),
			PasswordHash: l.PasswordHash,
		}
	}
}
