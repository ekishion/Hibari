package httpapi

import (
	"net/http"
	"path"
	"strings"
)

// handleAssets 提供前端静态资源，并为 SPA 路由回退到 index.html。
func (s *Server) handleAssets(w http.ResponseWriter, r *http.Request) {
	// 未注册的 /api 路径必须返回 JSON 404，不能回退成 index.html，
	// 否则前端会把 HTML 当 JSON 解析。
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeError(w, http.StatusNotFound, codeNotFound, "接口不存在")
		return
	}

	clean := path.Clean(r.URL.Path)
	if clean == "/" {
		clean = "/index.html"
	}

	f, err := s.assets.Open(strings.TrimPrefix(clean, "/"))
	if err != nil {
		// 非静态资源路径交给前端路由处理。
		s.serveIndex(w, r)
		return
	}
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		f.Close()
		s.serveIndex(w, r)
		return
	}
	f.Close()

	// 带哈希的构建产物可长期缓存，其余资源每次校验。
	if strings.HasPrefix(clean, "/assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.FileServer(s.assets).ServeHTTP(w, r)
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	f, err := s.assets.Open("index.html")
	if err != nil {
		http.Error(w, "前端资源未构建，请先执行 pnpm build", http.StatusNotFound)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		http.Error(w, "读取前端资源失败", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, "index.html", info.ModTime(), f)
}

// DirFS 把磁盘目录包成 http.FileSystem，供开发期直接指向构建输出。
func DirFS(dir string) http.FileSystem {
	return http.Dir(dir)
}
