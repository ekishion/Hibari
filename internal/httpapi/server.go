// Package httpapi 暴露 HTTP 接口。它把领域服务翻译为 REST，
// 不做业务判断，也不直接访问数据库。
package httpapi

import (
	"context"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/ekishion/hibari/internal/config"
	"github.com/ekishion/hibari/internal/identity"
	"github.com/ekishion/hibari/internal/library"
	"github.com/ekishion/hibari/internal/sharing"
)

// Deps 是 Server 的全部依赖，由 main 装配。
type Deps struct {
	Config      *config.Config
	Backend     library.Backend
	Identity    *identity.Service
	Sharing     *sharing.Service
	Auditor     Auditor
	Assets      http.FileSystem
	Version     string
	TrustedNets []*net.IPNet
}

// Auditor 记录审计事件。实现方决定落地方式。
type Auditor interface {
	Record(ctx context.Context, event, path, ip, ua string)
}

// AuditLog 额外提供查询能力。后端可以只实现 Auditor。
type AuditLog interface {
	Auditor
	Recent(ctx context.Context, limit int) ([]AuditEvent, error)
}

// Server 持有 HTTP 层需要的所有依赖。
type Server struct {
	cfg               *config.Config
	backend           library.Backend
	writer            library.Writer
	linker            library.Linker
	identity          *identity.Service
	sharing           *sharing.Service
	auditor           Auditor
	auditLog          AuditLog
	assets            http.FileSystem
	version           string
	proxies           []*net.IPNet
	limiter           *rateLimiter
	forceSecureCookie bool
	startedAt         time.Time
}

// New 构造 Server。可选接口在此处一次性探测，避免每个 handler 重复断言。
func New(d Deps) *Server {
	s := &Server{
		cfg:               d.Config,
		backend:           d.Backend,
		identity:          d.Identity,
		sharing:           d.Sharing,
		auditor:           d.Auditor,
		assets:            d.Assets,
		version:           d.Version,
		proxies:           d.TrustedNets,
		limiter:           newRateLimiter(d.Config.RateLimitPerSecond),
		forceSecureCookie: d.Config.SecureCookie,
		startedAt:         time.Now(),
	}
	// 可选能力探测：一次断言，后续直接判 nil。
	if w, ok := d.Backend.(library.Writer); ok {
		s.writer = w
	}
	if l, ok := d.Backend.(library.Linker); ok {
		s.linker = l
	}
	if al, ok := d.Auditor.(AuditLog); ok {
		s.auditLog = al
	}
	return s
}

// accessLevel 声明路由的访问要求。
type accessLevel int

const (
	accessPublic accessLevel = iota // 无需登录
	accessAdmin                     // 需要管理员会话
)

// route 是一行路由声明。
type route struct {
	method  string
	pattern string
	access  accessLevel
	limited bool
	handle  http.HandlerFunc
}

// routes 是全部路由的唯一清单。新增接口只需在此追加一行。
func (s *Server) routes() []route {
	return []route{
		// 公开：认证与状态
		{http.MethodPost, "/api/setup", accessPublic, true, s.handleSetup},
		{http.MethodPost, "/api/login", accessPublic, true, s.handleLogin},
		{http.MethodPost, "/api/logout", accessPublic, true, s.handleLogout},
		{http.MethodGet, "/api/status", accessPublic, true, s.handleStatus},

		// 公开：分享访问（凭证是短链本身）
		{http.MethodPost, "/api/share/info", accessPublic, true, s.handleShareInfo},
		{http.MethodPost, "/api/share/browse", accessPublic, true, s.handleShareBrowse},
		{http.MethodGet, "/api/share/{token}", accessPublic, true, s.handleShareDownload},
		{http.MethodGet, "/api/share/{token}/raw", accessPublic, true, s.handleShareRaw},

		// 需要管理员
		{http.MethodGet, "/api/files", accessAdmin, true, s.handleList},
		{http.MethodGet, "/api/files/raw", accessAdmin, true, s.handleRaw},
		{http.MethodGet, "/api/quota", accessAdmin, true, s.handleQuota},
		{http.MethodPost, "/api/shares", accessAdmin, true, s.handleShareCreate},
		{http.MethodGet, "/api/shares", accessAdmin, true, s.handleShareList},
		{http.MethodDelete, "/api/shares/{token}", accessAdmin, true, s.handleShareRevoke},
		{http.MethodPost, "/api/files/mkdir", accessAdmin, true, s.handleMkdir},
		{http.MethodPost, "/api/files/rename", accessAdmin, true, s.handleRename},
		{http.MethodPost, "/api/files/remove", accessAdmin, true, s.handleRemove},
		{http.MethodPost, "/api/upload", accessAdmin, true, s.handleUpload},
		{http.MethodPost, "/api/password", accessAdmin, true, s.handlePassword},
		{http.MethodGet, "/api/audit", accessAdmin, true, s.handleAudit},

		// 运维
		{http.MethodGet, "/healthz", accessPublic, false, s.handleHealth},

		// 静态资源兜底
		{http.MethodGet, "/", accessPublic, false, s.handleAssets},
	}
}

// Handler 构建最终的多路复用器。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	for _, rt := range s.routes() {
		mux.HandleFunc(rt.method+" "+rt.pattern, s.middleware(rt.access, rt.limited, rt.handle))
	}
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": s.version,
		"uptime":  int64(time.Since(s.startedAt).Seconds()),
	})
}

// audit 记录事件，失败不影响主流程。
func (s *Server) audit(r *http.Request, event, path string) {
	if s.auditor == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	s.auditor.Record(ctx, event, path, s.clientIP(r), r.UserAgent())
}

func (s *Server) logf(format string, args ...any) {
	log.Printf(format, args...)
}
