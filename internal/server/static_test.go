package server

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ekishion/jikeqingpan/internal/config"
	"github.com/ekishion/jikeqingpan/web"
)

// mustDistFS 返回嵌入的前端构建产物根（web/dist）。
func mustDistFS(t *testing.T) fs.FS {
	t.Helper()
	sub, err := fs.Sub(web.FS, "dist")
	if err != nil {
		t.Fatalf("fs.Sub dist failed: %v", err)
	}
	return sub
}

// distSampleAsset 返回构建产物中任意一个 hash 命名的 JS 资产路径（_nuxt/xxx.js）。
func distSampleAsset(t *testing.T) string {
	t.Helper()
	matches, err := fs.Glob(mustDistFS(t), "_nuxt/*.js")
	if err != nil || len(matches) == 0 {
		t.Fatalf("dist/_nuxt/*.js not found (frontend not built?)")
	}
	return matches[0]
}

// TestRenderIndexTemplate_RealFS 验证真实嵌入 FS 的模板预渲染输出 SPA index。
func TestRenderIndexTemplate_RealFS(t *testing.T) {
	out, err := renderIndexTemplate(mustDistFS(t))
	if err != nil {
		t.Fatalf("renderIndexTemplate failed: %v", err)
	}
	html := string(out)
	if strings.Contains(html, "{{template") {
		t.Fatalf("rendered output still contains raw template directives")
	}
	for _, marker := range []string{`id="__nuxt"`, `/_nuxt/`} {
		if !strings.Contains(html, marker) {
			t.Errorf("rendered index missing %q", marker)
		}
	}
}

// TestHandleStatic_Routes 校验静态路由：首页、构建产物哈希资产、缓存头与 ETag/304/gzip。
func TestHandleStatic_Routes(t *testing.T) {
	cfg := &config.Config{StateBackend: "none"}
	s := NewServer(cfg, web.FS, "test")
	defer s.Close()

	asset := distSampleAsset(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	s.handleStatic(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `id="__nuxt"`) {
		t.Fatalf("/ should serve SPA index, got %d", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Fatalf("index should be no-store, got %q", cc)
	}

	req = httptest.NewRequest(http.MethodGet, "/"+asset, nil)
	rec = httptest.NewRecorder()
	s.handleStatic(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("hashed asset should serve, got %d", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("hashed asset should be immutable, got %q", cc)
	}
	if rec.Header().Get("ETag") == "" {
		t.Fatalf("hashed asset should carry ETag")
	}
	etag := rec.Header().Get("ETag")
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Fatalf("asset content-type should be javascript, got %q", ct)
	}

	// ETag 命中 → 304
	req = httptest.NewRequest(http.MethodGet, "/"+asset, nil)
	req.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	s.handleStatic(rec, req)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("If-None-Match should yield 304, got %d", rec.Code)
	}

	// Accept-Encoding: gzip 的文本资产 → 压缩响应
	req = httptest.NewRequest(http.MethodGet, "/"+asset, nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec = httptest.NewRecorder()
	s.handleStatic(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("expected gzip response, got %d encoding=%q", rec.Code, rec.Header().Get("Content-Encoding"))
	}

	// 不存在的路径 → 404
	req = httptest.NewRequest(http.MethodGet, "/_nuxt/does-not-exist.js", nil)
	rec = httptest.NewRecorder()
	s.handleStatic(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// TestRoutes_Gating 验证按配置门控：关闭的功能路由不注册（代码路径不可达）。
func TestRoutes_Gating(t *testing.T) {
	cfg := &config.Config{StateBackend: "none"}
	s := NewServer(cfg, web.FS, "test")
	defer s.Close()

	// upload 关闭：POST /api/upload 落到静态兜底路由（仅 GET/HEAD）→ 405
	post := httptest.NewRequest(http.MethodPost, "/api/upload", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, post)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("upload disabled should not register route (fallback 405), got %d", rec.Code)
	}
	// quota 同样不注册
	get := httptest.NewRequest(http.MethodGet, "/api/quota", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, get)
	if rec.Code != http.StatusOK && rec.Code != http.StatusNotFound {
		t.Fatalf("quota disabled should fall through to static (200/404), got %d", rec.Code)
	}

	// readme 关闭（显式 false）：POST /api/readme 走静态兜底 → 405
	cfgReadmeOff := &config.Config{StateBackend: "none", ShowReadme: &[]bool{false}[0]}
	sReadmeOff := NewServer(cfgReadmeOff, web.FS, "test")
	defer sReadmeOff.Close()
	post = httptest.NewRequest(http.MethodPost, "/api/readme", strings.NewReader(`{}`))
	rec = httptest.NewRecorder()
	sReadmeOff.Handler().ServeHTTP(rec, post)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("readme disabled should not register route (fallback 405), got %d", rec.Code)
	}

	// 鉴权关闭：/api/login 不注册 → POST 走静态兜底 → 405；auth/status 始终保留
	post = httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{}`))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, post)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("auth disabled should not register login route (fallback 405), got %d", rec.Code)
	}
	get = httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, get)
	if rec.Code != http.StatusOK {
		t.Fatalf("auth/status should always exist, got %d", rec.Code)
	}
}
