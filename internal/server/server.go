package server

import (
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ekishion/jikeqingpan/internal/baidu"
	"github.com/ekishion/jikeqingpan/internal/config"
	"github.com/ekishion/jikeqingpan/internal/security"
	"github.com/ekishion/jikeqingpan/internal/storage"
)

func init() {
	_ = mime.AddExtensionType(".js", "application/javascript")
	_ = mime.AddExtensionType(".css", "text/css")
}

// Server 应用服务器
type Server struct {
	cfg               *config.Config
	limiter           *security.RateLimiter
	mux               *http.ServeMux
	baiduClient       *baidu.Client
	httpClient        *http.Client
	shortLinks        *storage.ShortLinkStore
	dirLinks          *storage.DirLinkStore
	cache             *storage.FileListCache
	staticRoot        fs.FS
	renderedIndexHTML []byte
	sessions          *security.SessionManager
	loginGuard        *security.LoginGuard
	auditLogger       *security.AuditLogger
	trustedProxy      []*net.IPNet
	persist           storage.StatePersistence
	persistMu         sync.Mutex
	flushStop         chan struct{}
	stateDirty        atomic.Bool
	version           string
}

// NewServer 创建并初始化应用服务器。
func NewServer(cfg *config.Config, staticContent fs.FS, version string) *Server {
	if version == "" {
		version = "dev"
	}
	sub, err := fs.Sub(staticContent, "static")
	if err != nil {
		// 允许测试传入已是 static 根的 FS
		sub = staticContent
	}

	renderedHTML, err := renderIndexTemplate(sub)
	if err != nil {
		log.Printf("[WARN] HTML 模板预编译失败，将回退至动态文件服务: %v", err)
	}

	cache := storage.NewFileListCacheWithLimits(storage.MaxCachedFiles, cfg.FileCacheTTL(), cfg.DlinkCacheTTL())
	httpClient := &http.Client{Timeout: baidu.BaiduClientTimeout}
	baiduClient := baidu.NewClient(cfg, cache, httpClient, cfg.BaiduBaseURL)

	s := &Server{
		cfg:               cfg,
		version:           version,
		limiter:           security.NewRateLimiter(cfg.RateLimitPerSecond),
		mux:               http.NewServeMux(),
		baiduClient:       baiduClient,
		httpClient:        httpClient,
		shortLinks:        storage.NewShortLinkStore(cfg.ShortLinkTTL(), cfg.ShortLinkMaxUses),
		dirLinks:          storage.NewDirLinkStore(cfg.DirLinkTTL()),
		cache:             cache,
		staticRoot:        sub,
		renderedIndexHTML: renderedHTML,
		sessions:          security.NewSessionManager(cfg.SessionSigningKey(), cfg.AuthSessionTTL()),
		loginGuard:        security.NewLoginGuard(),
		auditLogger:       security.NewAuditLogger(cfg.AuditLogPath, security.DefaultAuditMaxBytes),
	}

	for _, raw := range cfg.TrustedProxyIPs {
		raw = strings.TrimSpace(raw)
		if ip := net.ParseIP(raw); ip != nil {
			bits := 128
			if ip4 := ip.To4(); ip4 != nil {
				ip, bits = ip4, 32
			}
			s.trustedProxy = append(s.trustedProxy, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
		} else if _, network, err := net.ParseCIDR(raw); err == nil {
			s.trustedProxy = append(s.trustedProxy, network)
		}
	}

	s.routes()
	s.restorePersistedState()
	s.startStateFlusher()
	return s
}

// Handler 返回 HTTP 路由处理器。
func (s *Server) Handler() http.Handler {
	return s.mux
}

// Close 优雅关闭服务器的持久化与审计日志资源。
func (s *Server) Close() {
	s.closePersist()
	if s.auditLogger != nil {
		s.auditLogger.Close()
	}
}

// WriteTimeoutFor 按 list_max_pages 推导 WriteTimeout。
func WriteTimeoutFor(cfg *config.Config) time.Duration {
	return config.WriteTimeoutFor(cfg)
}

func (s *Server) routes() {
	s.mux.HandleFunc("/api/auth/status", s.withSecurity(s.handleAuthStatus, securityOpts{
		requireAuth: false,
		csrf:        false,
		rateLimit:   true,
	}))
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
	s.mux.HandleFunc("/api/readme", s.withSecurity(s.handleReadme, securityOpts{
		requireAuth: true,
		csrf:        true,
		rateLimit:   true,
		methods:     []string{http.MethodPost},
	}))
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

// ===== 状态持久化 =====

// restorePersistedState 启动时从持久化后端恢复短链状态。
// 读取失败（文件损坏等）时把原文件改名留存后重建，服务不因此拒绝启动。
func (s *Server) restorePersistedState() {
	persist, err := storage.NewStatePersistence(s.cfg)
	if err != nil {
		log.Printf("[WARN] 状态持久化初始化失败，本次运行仅内存模式: %v", err)
		return
	}
	if persist == nil {
		return
	}
	st, err := persist.Load()
	if err != nil {
		backup := s.cfg.StatePath + ".corrupt-" + strconv.FormatInt(time.Now().Unix(), 10)
		_ = persist.Close()
		if renameErr := os.Rename(s.cfg.StatePath, backup); renameErr == nil {
			log.Printf("[WARN] 状态文件损坏已留存为 %s: %v", backup, err)
		} else {
			log.Printf("[WARN] 状态文件损坏且无法留存: %v", err)
		}
		persist, err = storage.NewStatePersistence(s.cfg)
		if err != nil || persist == nil {
			return
		}
		st = &storage.PersistedState{Version: 1}
	}
	s.dirLinks.Restore(st.DirLinks)
	s.shortLinks.Restore(st.ShortLinks)
	s.cache.RestoreDirCache(st.DirCache)
	s.persist = persist
	log.Printf("[状态] 已恢复目录短链 %d 条、下载短链 %d 条、目录缓存 %d 个", len(st.DirLinks), len(st.ShortLinks), len(st.DirCache))
}

// startStateFlusher 周期把变脏的状态落盘（30s）。
func (s *Server) startStateFlusher() {
	if s.persist == nil {
		return
	}
	s.flushStop = make(chan struct{})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if !s.stateDirty.CompareAndSwap(true, false) {
					continue
				}
				if err := s.flushState(); err != nil {
					log.Printf("[WARN] 状态落盘失败，下个周期重试: %v", err)
					s.stateDirty.Store(true)
				}
			case <-s.flushStop:
				return
			}
		}
	}()
}

// markStateDirty 标记状态有变化，等待周期落盘。
func (s *Server) markStateDirty() {
	if s.persist != nil {
		s.stateDirty.Store(true)
	}
}

// flushState 立即把当前快照写入持久化后端。
func (s *Server) flushState() error {
	if s.persist == nil {
		return nil
	}
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	st := &storage.PersistedState{
		Version:    1,
		DirLinks:   s.dirLinks.Snapshot(),
		ShortLinks: s.shortLinks.Snapshot(),
		DirCache:   s.cache.ExportDirCache(),
	}
	return s.persist.Save(st)
}

// closePersist 优雅关闭时强制落盘并关闭后端；可安全重复调用。
func (s *Server) closePersist() {
	if s.persist == nil {
		return
	}
	if s.flushStop != nil {
		close(s.flushStop)
		s.flushStop = nil
	}
	if err := s.flushState(); err != nil {
		log.Printf("[WARN] 关闭前状态落盘失败: %v", err)
	}
	if err := s.persist.Close(); err != nil {
		log.Printf("[WARN] 关闭持久化后端失败: %v", err)
	}
	s.persist = nil
}

func (s *Server) withSecurity(next http.HandlerFunc, opts securityOpts) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 方法限制
		if len(opts.methods) > 0 {
			ok := false
			for _, m := range opts.methods {
				if r.Method == m {
					ok = true
					break
				}
			}
			if !ok {
				writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "请求方式不受支持")
				return
			}
		} else if r.Method != http.MethodGet && r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "请求方式不受支持")
			return
		}

		// 安全响应头
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; "+
				"media-src 'self' https://baidu.com https://*.baidu.com https://baidupcs.com https://*.baidupcs.com https://bdstatic.com https://*.bdstatic.com; "+
				"frame-ancestors 'none'; object-src 'none'; base-uri 'none'; form-action 'self'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Cache-Control", "no-store")

		// CSRF cookie 下发（GET；健康检查无需 Cookie，避免探活请求反复种 Cookie）
		if r.Method == http.MethodGet && r.URL.Path != "/healthz" {
			if _, err := r.Cookie(csrfTokenCookieName); err != nil {
				token, tokenErr := newCSRFToken()
				if tokenErr != nil {
					writeJSONError(w, http.StatusInternalServerError, "csrf_issue", "服务异常，请刷新页面重试")
					return
				}
				s.setCSRFCookie(w, r, token)
			}
		}

		// CSRF 校验：仅保护 POST 且 opts.csrf
		if opts.csrf && r.Method == http.MethodPost {
			if !hasValidCSRFToken(r) {
				writeJSONError(w, http.StatusForbidden, "csrf_invalid", "页面已过期，请刷新后重试")
				return
			}
			if cookie, err := r.Cookie(csrfTokenCookieName); err == nil && cookie.Value != "" {
				s.setCSRFCookie(w, r, cookie.Value)
			}
		}

		// 访问鉴权
		if opts.requireAuth && s.cfg.AuthEnabled() && !s.hasValidAccess(r) {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "请先验证访问权限")
			return
		}

		// 限流
		if opts.rateLimit {
			ip := s.requestClientIP(r)
			if !s.limiter.Allow(ip) {
				writeJSONError(w, http.StatusTooManyRequests, "rate_limited", "操作太频繁了，请稍后再试")
				return
			}
		}

		next(w, r)
	}
}

// legacyStaticMap 旧单体平铺路径与新模块路径的映射关系，保证向下兼容
var legacyStaticMap = map[string]string{
	"/app.js":          "js/app.js",
	"/icons.js":        "js/core/icons.js",
	"/qrcode.js":       "js/core/qrcode.js",
	"/markdown.js":     "js/core/markdown.js",
	"/api.js":          "js/core/api.js",
	"/components.js":   "js/core/components.js",
	"/preview.js":      "js/modules/preview.js",
	"/share_modal.js":  "js/modules/share_modal.js",
	"/file_browser.js": "js/modules/file_browser.js",
	"/share_page.js":   "js/modules/share_page.js",
	"/app.css":         "css/app.css",
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
		http.ServeFileFS(w, r, s.staticRoot, "index.html")
		return
	}

	// 2. 查找是否命中平铺旧路径映射
	targetPath := ""
	if mapped, ok := legacyStaticMap[r.URL.Path]; ok {
		targetPath = mapped
	} else {
		// 3. 正常分类子目录请求（/css/*, /js/*）
		clean := path.Clean(strings.TrimPrefix(r.URL.Path, "/"))
		if strings.HasPrefix(clean, "css/") || strings.HasPrefix(clean, "js/") {
			targetPath = clean
		}
	}

	if targetPath == "" {
		http.NotFound(w, r)
		return
	}

	// 校验静态文件存在性
	if _, err := fs.Stat(s.staticRoot, targetPath); err != nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	if strings.HasSuffix(targetPath, ".js") {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	} else if strings.HasSuffix(targetPath, ".css") {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	} else if strings.HasSuffix(targetPath, ".html") {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}

	http.ServeFileFS(w, r, s.staticRoot, targetPath)
}

func (s *Server) audit(r *http.Request, event, filePath string) {
	if s.auditLogger == nil {
		return
	}
	clientIP := "unknown"
	userAgent := ""
	if r != nil {
		clientIP = s.requestClientIP(r)
		userAgent = r.UserAgent()
	}
	s.auditLogger.Log(event, filePath, clientIP, userAgent)
}
