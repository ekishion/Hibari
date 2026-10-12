package storage

import (
	"bytes"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"
)

const DefaultBaiduUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"

func NormalizeDownloadUA(ua string) string {
	ua = strings.TrimSpace(ua)
	if ua == "" {
		return DefaultBaiduUA
	}
	return ua
}

// FileMeta 文件元数据，包含下载签名计算所需的 id 和 md5
type FileMeta struct {
	FsID          int64     `json:"fs_id"`
	MD5           string    `json:"md5"`
	Size          int64     `json:"size"`
	IsDir         int       `json:"isdir"`
	DLink         string    `json:"dlink"`
	DLinkUA       string    `json:"-"` // 直链与申请时的 UA 绑定，避免跨 UA 复用失效
	DLinkCachedAt time.Time `json:"-"`
	CachedAt      time.Time `json:"-"`
	LastAccessAt  time.Time `json:"-"`
}

// DirCacheEntry 缓存目录列表响应
type DirCacheEntry struct {
	body     []byte
	cachedAt time.Time
}

// ReadmeCacheEntry 缓存已读取的 README 内容
type ReadmeCacheEntry struct {
	name     string
	content  string
	cachedAt time.Time
}

// FileListCache 缓存文件列表中每个文件的元数据与目录列表，按路径索引
type FileListCache struct {
	mu           sync.RWMutex
	filesByPath  map[string]FileMeta         // 文件路径 -> 元数据
	dirsByPath   map[string]DirCacheEntry    // 目录路径 -> 目录列表 JSON
	readmeByPath map[string]ReadmeCacheEntry // 文件路径 -> README
	updatedAt    time.Time
	maxEntries   int
	maxDirs      int
	ttl          time.Duration
	dlinkTTL     time.Duration
}

const CachedDLinkTTL = 5 * time.Minute
const FileMetaTTL = 15 * time.Minute
const MaxCachedFiles = 10000
const MaxCachedDirs = 500
const MaxCachedReadmes = 200

func NewFileListCache() *FileListCache {
	return NewFileListCacheWithLimits(MaxCachedFiles, FileMetaTTL, CachedDLinkTTL)
}

func NewFileListCacheWithLimits(maxEntries int, ttl, dlinkTTL time.Duration) *FileListCache {
	if maxEntries < 1 {
		maxEntries = MaxCachedFiles
	}
	if ttl <= 0 {
		ttl = FileMetaTTL
	}
	if dlinkTTL <= 0 {
		dlinkTTL = CachedDLinkTTL
	}
	return &FileListCache{
		filesByPath:  make(map[string]FileMeta),
		dirsByPath:   make(map[string]DirCacheEntry),
		readmeByPath: make(map[string]ReadmeCacheEntry),
		maxEntries:   maxEntries,
		maxDirs:      MaxCachedDirs,
		ttl:          ttl,
		dlinkTTL:     dlinkTTL,
	}
}

// Update 解析 Baidu 列表响应并更新文件元数据索引
func (c *FileListCache) Update(listJSON []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.updateLocked(listJSON, time.Now())
}

func (c *FileListCache) updateLocked(listJSON []byte, now time.Time) {
	var resp struct {
		Errno int `json:"errno"`
		List  []struct {
			Path  string `json:"path"`
			FsID  int64  `json:"fs_id"`
			MD5   string `json:"md5"`
			Size  int64  `json:"size"`
			IsDir int    `json:"isdir"`
			DLink string `json:"dlink"`
		} `json:"list"`
	}
	if err := json.Unmarshal(listJSON, &resp); err != nil {
		log.Printf("[WARN] 解析文件列表失败: %v", err)
		return
	}
	if resp.Errno != 0 {
		log.Printf("[WARN] 百度API返回错误 errno=%d", resp.Errno)
		return
	}
	if c.filesByPath == nil {
		c.filesByPath = make(map[string]FileMeta)
	}
	c.cleanupExpiredLocked(now)
	for _, f := range resp.List {
		if f.Path != "" && f.FsID != 0 {
			dlink := f.DLink
			dlinkUA := ""
			dlinkCachedAt := time.Time{}
			if dlink != "" {
				dlinkUA = DefaultBaiduUA
				dlinkCachedAt = now
			}
			c.filesByPath[f.Path] = FileMeta{
				FsID:          f.FsID,
				MD5:           f.MD5,
				Size:          f.Size,
				IsDir:         f.IsDir,
				DLink:         dlink,
				DLinkUA:       dlinkUA,
				DLinkCachedAt: dlinkCachedAt,
				CachedAt:      now,
				LastAccessAt:  now,
			}
		}
	}
	c.enforceLimitLocked()
	c.updatedAt = time.Now()
	log.Printf("[缓存] 更新完成：共 %d 个文件，当前总共已缓存 %d 个元数据", len(resp.List), len(c.filesByPath))
}

// GetFileMeta 根据路径获取文件元数据
func (c *FileListCache) GetFileMeta(path string) (FileMeta, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	meta, ok := c.filesByPath[path]
	if ok && !meta.CachedAt.IsZero() && time.Since(meta.CachedAt) >= c.ttl {
		delete(c.filesByPath, path)
		return FileMeta{}, false
	}
	if ok {
		meta.LastAccessAt = time.Now()
		c.filesByPath[path] = meta
	}
	return meta, ok
}

func (c *FileListCache) GetCachedDLink(filePath, ua string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	meta, ok := c.filesByPath[filePath]
	if ok && !meta.CachedAt.IsZero() && time.Since(meta.CachedAt) >= c.ttl {
		delete(c.filesByPath, filePath)
		return "", false
	}
	ua = NormalizeDownloadUA(ua)
	if !ok || meta.DLink == "" || meta.DLinkUA == "" || meta.DLinkUA != ua ||
		meta.DLinkCachedAt.IsZero() || time.Since(meta.DLinkCachedAt) >= c.dlinkTTL {
		return "", false
	}
	meta.LastAccessAt = time.Now()
	c.filesByPath[filePath] = meta
	return meta.DLink, true
}

func (c *FileListCache) SetDLink(filePath, dlink, ua string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	meta, ok := c.filesByPath[filePath]
	if !ok {
		return
	}
	meta.DLink = dlink
	meta.DLinkUA = NormalizeDownloadUA(ua)
	meta.DLinkCachedAt = time.Now()
	meta.LastAccessAt = meta.DLinkCachedAt
	c.filesByPath[filePath] = meta
}

// GetDirList 根据目录路径获取缓存的列表响应
func (c *FileListCache) GetDirList(dir string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.dirsByPath[dir]
	if !ok {
		return nil, false
	}
	if !entry.cachedAt.IsZero() && time.Since(entry.cachedAt) >= c.ttl {
		delete(c.dirsByPath, dir)
		return nil, false
	}
	return entry.body, true
}

// SetDirList 缓存目录列表响应
func (c *FileListCache) SetDirList(dir string, body []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dirsByPath == nil {
		c.dirsByPath = make(map[string]DirCacheEntry)
	}
	c.dirsByPath[dir] = DirCacheEntry{
		body:     bytes.Clone(body),
		cachedAt: time.Now(),
	}
	for len(c.dirsByPath) > c.maxDirs {
		evicted := EvictOldestSampled(c.dirsByPath, func(e DirCacheEntry) time.Time {
			return e.cachedAt
		}, EvictionSampleSize)
		if !evicted {
			break
		}
	}
}

// InvalidateDir 删除指定目录的列表缓存及相关的 README 缓存
func (c *FileListCache) InvalidateDir(dir string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.dirsByPath, dir)
	cleanDir := strings.TrimSuffix(dir, "/")
	for p := range c.readmeByPath {
		if p == cleanDir || strings.HasPrefix(p, cleanDir+"/") {
			delete(c.readmeByPath, p)
		}
	}
}

// GetReadme 获取缓存的 README 内容
func (c *FileListCache) GetReadme(path string) (string, string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.readmeByPath[path]
	if !ok {
		return "", "", false
	}
	if !entry.cachedAt.IsZero() && time.Since(entry.cachedAt) >= c.ttl {
		delete(c.readmeByPath, path)
		return "", "", false
	}
	return entry.name, entry.content, true
}

// SetReadme 缓存 README 内容
func (c *FileListCache) SetReadme(path, name, content string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.readmeByPath == nil {
		c.readmeByPath = make(map[string]ReadmeCacheEntry)
	}
	c.readmeByPath[path] = ReadmeCacheEntry{
		name:     name,
		content:  content,
		cachedAt: time.Now(),
	}
	for len(c.readmeByPath) > MaxCachedReadmes {
		evicted := EvictOldestSampled(c.readmeByPath, func(e ReadmeCacheEntry) time.Time {
			return e.cachedAt
		}, EvictionSampleSize)
		if !evicted {
			break
		}
	}
}

// ExportDirCache 导出有效的目录缓存用于持久化
func (c *FileListCache) ExportDirCache() []PersistedDirCache {
	c.mu.RLock()
	defer c.mu.RUnlock()
	now := time.Now()
	var list []PersistedDirCache
	for dir, entry := range c.dirsByPath {
		if !entry.cachedAt.IsZero() && now.Sub(entry.cachedAt) >= c.ttl {
			continue
		}
		list = append(list, PersistedDirCache{
			Dir:      dir,
			Body:     json.RawMessage(entry.body),
			CachedAt: entry.cachedAt.Unix(),
		})
	}
	return list
}

// RestoreDirCache 从持久化状态恢复目录缓存并预热文件元数据
func (c *FileListCache) RestoreDirCache(entries []PersistedDirCache) {
	if len(entries) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dirsByPath == nil {
		c.dirsByPath = make(map[string]DirCacheEntry)
	}
	now := time.Now()
	for _, entry := range entries {
		if entry.Dir == "" || len(entry.Body) == 0 {
			continue
		}
		cachedAt := time.Unix(entry.CachedAt, 0)
		if entry.CachedAt > 0 && now.Sub(cachedAt) >= c.ttl {
			continue
		}
		if entry.CachedAt == 0 {
			cachedAt = now
		}
		c.dirsByPath[entry.Dir] = DirCacheEntry{
			body:     bytes.Clone(entry.Body),
			cachedAt: cachedAt,
		}
		c.updateLocked(entry.Body, cachedAt)
	}
}

func (c *FileListCache) cleanupExpiredLocked(now time.Time) {
	for filePath, meta := range c.filesByPath {
		if !meta.CachedAt.IsZero() && now.Sub(meta.CachedAt) >= c.ttl {
			delete(c.filesByPath, filePath)
		}
	}
	for dir, entry := range c.dirsByPath {
		if !entry.cachedAt.IsZero() && now.Sub(entry.cachedAt) >= c.ttl {
			delete(c.dirsByPath, dir)
		}
	}
	for p, entry := range c.readmeByPath {
		if !entry.cachedAt.IsZero() && now.Sub(entry.cachedAt) >= c.ttl {
			delete(c.readmeByPath, p)
		}
	}
}

func (c *FileListCache) enforceLimitLocked() {
	for len(c.filesByPath) > c.maxEntries {
		evicted := EvictOldestSampled(c.filesByPath, func(meta FileMeta) time.Time {
			if meta.LastAccessAt.IsZero() {
				return meta.CachedAt
			}
			return meta.LastAccessAt
		}, EvictionSampleSize)
		if !evicted {
			return
		}
	}
}
