package server

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
)

// immutablePrefix 带内容哈希的构建产物前缀：可永久缓存。
const immutablePrefix = "_nuxt/"

// staticContentTypes 静态资源的 Content-Type 映射。
var staticContentTypes = map[string]string{
	".js":    "application/javascript; charset=utf-8",
	".mjs":   "application/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".html":  "text/html; charset=utf-8",
	".svg":   "image/svg+xml",
	".json":  "application/json; charset=utf-8",
	".png":   "image/png",
	".jpg":   "image/jpeg",
	".jpeg":  "image/jpeg",
	".webp":  "image/webp",
	".gif":   "image/gif",
	".ico":   "image/x-icon",
	".woff":  "font/woff",
	".woff2": "font/woff2",
	".txt":   "text/plain; charset=utf-8",
}

// gzipTypes 需要预压缩的文本资源扩展名。
var gzipTypes = map[string]bool{
	".js": true, ".mjs": true, ".css": true, ".html": true,
	".svg": true, ".json": true, ".txt": true, ".map": true,
}

// gzipMinBytes 小于该值的文件不值得压缩。
const gzipMinBytes = 256

type gzipCacheEntry struct {
	data []byte
	ok   bool
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	// 1. 首页直接输出预渲染好的单包 HTML
	if r.URL.Path == "/" || r.URL.Path == "/index.html" {
		w.Header().Set("Cache-Control", "no-store, max-age=0")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if len(s.renderedIndexHTML) > 0 {
			_, _ = w.Write(s.renderedIndexHTML)
			return
		}
		if raw, err := fs.ReadFile(s.staticRoot, "index.html"); err == nil {
			_, _ = w.Write(injectScriptNonce(raw, s.cspScriptNonce))
			return
		}
		return
	}

	// 2. 解析目标文件：构建产物目录（_nuxt/）或根级单文件（favicon.ico、robots.txt 等）
	clean := path.Clean(strings.TrimPrefix(r.URL.Path, "/"))
	targetPath := ""
	switch {
	case strings.HasPrefix(clean, immutablePrefix):
		targetPath = clean
	case !strings.Contains(clean, "/") && clean != "index.html":
		// 根级单文件；嵌套目录（源码、模板等）不暴露
		targetPath = clean
	}
	if targetPath == "" {
		http.NotFound(w, r)
		return
	}

	data, err := fs.ReadFile(s.staticRoot, targetPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	etag := staticETag(data)
	w.Header().Set("ETag", etag)
	if strings.HasPrefix(targetPath, immutablePrefix) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		// 内容不可变但无哈希文件名：强制再验证，命中 ETag 时 304 省流量
		w.Header().Set("Cache-Control", "no-cache")
	}
	if contentType, ok := staticContentTypes[strings.ToLower(path.Ext(targetPath))]; ok {
		w.Header().Set("Content-Type", contentType)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}

	if matchesETag(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	if body, ok := s.gzipBody(targetPath, data); ok && acceptsGzip(r) {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Vary", "Accept-Encoding")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		if r.Method != http.MethodHead {
			_, _ = w.Write(body)
		}
		return
	}

	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

// staticETag 计算内容的强 ETag（内容嵌入二进制不可变，适合精确比较）。
func staticETag(data []byte) string {
	sum := sha256.Sum256(data)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}

// matchesETag 按RFC 7232 处理 If-None-Match（含 * 与逗号分隔列表）。
func matchesETag(header, etag string) bool {
	if header == "" {
		return false
	}
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || candidate == etag || candidate == "W/"+etag {
			return true
		}
	}
	return false
}

// acceptsGzip 判断客户端是否接受 gzip 编码。
func acceptsGzip(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept-Encoding"), "gzip")
}

// gzipBody 惰性压缩文本资源并缓存（embed 内容不可变，压缩结果可复用）。
func (s *Server) gzipBody(targetPath string, data []byte) ([]byte, bool) {
	if !gzipTypes[strings.ToLower(path.Ext(targetPath))] || len(data) < gzipMinBytes {
		return nil, false
	}
	if cached, ok := s.gzipCache.Load(targetPath); ok {
		entry := cached.(gzipCacheEntry)
		return entry.data, entry.ok
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		s.gzipCache.Store(targetPath, gzipCacheEntry{ok: false})
		return nil, false
	}
	if err := zw.Close(); err != nil {
		s.gzipCache.Store(targetPath, gzipCacheEntry{ok: false})
		return nil, false
	}
	out := buf.Bytes()
	ok := len(out) < len(data)
	if ok {
		s.gzipCache.Store(targetPath, gzipCacheEntry{data: out, ok: true})
		return out, true
	}
	s.gzipCache.Store(targetPath, gzipCacheEntry{ok: false})
	return nil, false
}
