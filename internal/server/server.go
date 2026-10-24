package server

import (
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
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
	cspScriptNonce    string
	// gzipCache 静态文本资源的惰性 gzip 缓存（embed 内容不可变，键为目标路径）
	gzipCache    sync.Map
	sessions     *security.SessionManager
	loginGuard   *security.LoginGuard
	auditLogger  *security.AuditLogger
	trustedProxy []*net.IPNet
	persist      storage.StatePersistence
	persistMu    sync.Mutex
	flushStop    chan struct{}
	stateDirty   atomic.Bool
	version      string
}

// NewServer 创建并初始化应用服务器。
func NewServer(cfg *config.Config, staticContent fs.FS, version string) *Server {
	if version == "" {
		version = "dev"
	}
	staticRoot := resolveStaticRoot(staticContent)

	renderedHTML, err := renderIndexTemplate(staticRoot)
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
		staticRoot:        staticRoot,
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

	// Nuxt SPA 依赖内联 bootstrap script，CSP 需以 nonce 放行（保持 script-src 不退化到 unsafe-inline）
	s.cspScriptNonce = newCSPNonce()
	s.renderedIndexHTML = injectScriptNonce(s.renderedIndexHTML, s.cspScriptNonce)

	s.routes()
	s.restorePersistedState()
	s.startStateFlusher()
	return s
}

// resolveStaticRoot 自适应解析静态资源根：
// 优先 frontend 构建产物目录（web/dist 嵌入为 dist/），其次旧 web/static（static/），
// 都不满足时视为调用方传入的已是根 FS（测试场景）。
func resolveStaticRoot(staticContent fs.FS) fs.FS {
	if _, err := fs.Stat(staticContent, "index.html"); err == nil {
		return staticContent
	}
	for _, prefix := range []string{"dist", "static"} {
		sub, err := fs.Sub(staticContent, prefix)
		if err != nil {
			continue
		}
		if _, err := fs.Stat(sub, "index.html"); err == nil {
			return sub
		}
	}
	return staticContent
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
