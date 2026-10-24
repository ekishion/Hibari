package server

import (
	"net/http"
)

func (s *Server) routes() {
	s.mux.HandleFunc("/api/auth/status", s.withSecurity(s.handleAuthStatus, securityOpts{
		requireAuth: false,
		csrf:        false,
		rateLimit:   true,
	}))
	// 鉴权路由只在启用 access_token 时注册；auth/status 始终保留（前端引导需要）
	if s.cfg.AuthEnabled() {
		s.mux.HandleFunc("/api/login", s.withSecurity(s.handleLogin, securityOpts{
			requireAuth: false,
			csrf:        true,
			rateLimit:   true,
			methods:     []string{http.MethodPost},
		}))
		s.mux.HandleFunc("/api/logout", s.withSecurity(s.handleLogout, securityOpts{
			requireAuth: true,
			csrf:        true,
			rateLimit:   true,
			methods:     []string{http.MethodPost},
		}))
	}
	s.mux.HandleFunc("/api/files", s.withSecurity(s.handleFiles, securityOpts{
		requireAuth: false, // 目录短链允许外部访客在 token 沙盒内受限浏览；无 token 时在 handleFiles 内部校验后台 access_token
		csrf:        true,  // POST 校验；GET 跳过
		rateLimit:   true,
		methods:     []string{http.MethodGet, http.MethodPost},
	}))
	s.mux.HandleFunc("/api/share/info", s.withSecurity(s.handleShareInfo, securityOpts{
		requireAuth: false, // 允许访客探查公开单文件短链元数据或校验提取码
		csrf:        true,
		rateLimit:   true,
		methods:     []string{http.MethodPost},
	}))
	s.mux.HandleFunc("/api/dir-link", s.withSecurity(s.handleDirLink, securityOpts{
		requireAuth: true,
		csrf:        true,
		rateLimit:   true,
		methods:     []string{http.MethodPost},
	}))
	s.mux.HandleFunc("/api/shares", s.withSecurity(s.handleSharesList, securityOpts{
		requireAuth: true,
		csrf:        false,
		rateLimit:   true,
		methods:     []string{http.MethodGet, http.MethodPost},
	}))
	s.mux.HandleFunc("/api/shares/revoke", s.withSecurity(s.handleShareRevoke, securityOpts{
		requireAuth: true,
		csrf:        true,
		rateLimit:   true,
		methods:     []string{http.MethodPost},
	}))
	// README 功能关闭时整个路由不注册，代码路径不可达
	if s.cfg.ShowReadmeEnabled() {
		s.mux.HandleFunc("/api/readme", s.withSecurity(s.handleReadme, securityOpts{
			requireAuth: true,
			csrf:        true,
			rateLimit:   true,
			methods:     []string{http.MethodPost},
		}))
	}
	s.mux.HandleFunc("/api/download", s.withSecurity(s.handleDownload, securityOpts{
		requireAuth: true,
		csrf:        true,
		rateLimit:   true,
		methods:     []string{http.MethodPost},
	}))
	s.mux.HandleFunc("/api/preview", s.withSecurity(s.handlePreview, securityOpts{
		requireAuth: true,
		csrf:        true,
		rateLimit:   true,
		methods:     []string{http.MethodPost},
	}))
	s.mux.HandleFunc("/api/text", s.withSecurity(s.handleTextPreview, securityOpts{
		requireAuth: true,
		csrf:        true,
		rateLimit:   true,
		methods:     []string{http.MethodPost},
	}))
	s.mux.HandleFunc("/api/stream", s.withSecurity(s.handleStream, securityOpts{
		requireAuth: true,
		csrf:        false, // 只读中转：GET 无状态变更，无需 CSRF
		rateLimit:   true,
		methods:     []string{http.MethodGet, http.MethodHead},
	}))
	// 上传功能关闭时不注册 /api/upload 与 /api/quota（容量展示仅服务于上传弹窗）
	if s.cfg.UploadEnabled {
		s.mux.HandleFunc("/api/quota", s.withSecurity(s.handleQuota, securityOpts{
			requireAuth: true,
			csrf:        false, // GET 只读：网盘容量展示
			rateLimit:   true,
			methods:     []string{http.MethodGet},
		}))
		s.mux.HandleFunc("/api/upload", s.withSecurity(s.handleUpload, securityOpts{
			requireAuth: true,
			csrf:        true,
			rateLimit:   true,
			methods:     []string{http.MethodPost},
		}))
	}
	s.mux.HandleFunc("/healthz", s.withSecurity(s.handleHealthz, securityOpts{
		requireAuth: false,
		csrf:        false,
		rateLimit:   false,
		methods:     []string{http.MethodGet, http.MethodHead},
	}))
	s.mux.HandleFunc("/readyz", s.withSecurity(s.handleReadyz, securityOpts{
		requireAuth: true, // 避免未鉴权刷百度探测
		csrf:        false,
		rateLimit:   true,
	}))
	s.mux.HandleFunc("/d/", s.withSecurity(s.handleShortDownload, securityOpts{
		requireAuth: false, // 短链本身通过 token 及可选提取码鉴权，不要求全站后台 access_token
		csrf:        false,
		rateLimit:   true,
		methods:     []string{http.MethodGet, http.MethodHead},
	}))
	s.mux.HandleFunc("/", s.withSecurity(s.handleStatic, securityOpts{
		requireAuth: false, // 登录页需要能加载静态资源
		csrf:        false,
		rateLimit:   false,
		methods:     []string{http.MethodGet, http.MethodHead},
	}))
}

type securityOpts struct {
	requireAuth bool
	csrf        bool
	rateLimit   bool
	methods     []string
}
