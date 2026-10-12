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

// TestRenderIndexTemplate_RealFS 验证真实嵌入 FS 的模板预渲染输出完整包含各 partial 内容。
func TestRenderIndexTemplate_RealFS(t *testing.T) {
	out, err := renderIndexTemplate(mustSubStatic(t))
	if err != nil {
		t.Fatalf("renderIndexTemplate failed: %v", err)
	}
	html := string(out)
	if strings.Contains(html, "{{template") {
		t.Fatalf("rendered output still contains raw template directives")
	}
	for _, marker := range []string{
		`id="login-overlay"`,
		`id="image-lightbox"`,
		`id="share-overlay"`,
		`id="shares-manage-overlay"`,
		`id="unlock-overlay"`,
		`id="app-main"`,
		`id="share-landing"`,
		`/js/app.js`,
	} {
		if !strings.Contains(html, marker) {
			t.Errorf("rendered index missing %q", marker)
		}
	}
}

// TestHandleStatic_Routes 校验静态路由：首页、旧平铺路径别名、新模块路径。
func TestHandleStatic_Routes(t *testing.T) {
	cfg := &config.Config{StateBackend: "none"}
	s := NewServer(cfg, web.FS, "test")
	defer s.Close()

	cases := []struct {
		path       string
		wantStatus int
		wantType   string
		wantBody   string
	}{
		{"/", http.StatusOK, "text/html; charset=utf-8", `id="login-overlay"`},
		{"/index.html", http.StatusOK, "text/html; charset=utf-8", `id="app-main"`},
		{"/css/app.css", http.StatusOK, "text/css; charset=utf-8", "base.css"},
		{"/js/core/api.js", http.StatusOK, "application/javascript; charset=utf-8", "apiFetch"},
		{"/app.css", http.StatusOK, "text/css; charset=utf-8", "base.css"},
		{"/app.js", http.StatusOK, "application/javascript; charset=utf-8", "apiFetch"},
		{"/icons.js", http.StatusOK, "application/javascript; charset=utf-8", ""},
		{"/js/modules/preview.js", http.StatusOK, "application/javascript; charset=utf-8", ""},
		{"/nonexistent.js", http.StatusNotFound, "", ""},
	}

	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rec := httptest.NewRecorder()
		s.handleStatic(rec, req)
		if rec.Code != tc.wantStatus {
			t.Errorf("%s: expected status %d, got %d", tc.path, tc.wantStatus, rec.Code)
			continue
		}
		if tc.wantType != "" && rec.Header().Get("Content-Type") != tc.wantType {
			t.Errorf("%s: expected content-type %q, got %q", tc.path, tc.wantType, rec.Header().Get("Content-Type"))
		}
		if tc.wantBody != "" && !strings.Contains(rec.Body.String(), tc.wantBody) {
			t.Errorf("%s: body missing %q", tc.path, tc.wantBody)
		}
	}
}

func mustSubStatic(t *testing.T) fs.FS {
	t.Helper()
	sub, err := fs.Sub(web.FS, "static")
	if err != nil {
		t.Fatalf("fs.Sub failed: %v", err)
	}
	return sub
}
