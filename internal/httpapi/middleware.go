package httpapi

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ekishion/hibari/internal/identity"
)

// ctxKey 是本包私有的上下文键类型，避免与其它包冲突。
type ctxKey int

const ctxUser ctxKey = iota

// userFrom 取出中间件注入的用户。
func userFrom(ctx context.Context) (identity.User, bool) {
	u, ok := ctx.Value(ctxUser).(identity.User)
	return u, ok
}

// sessionCookie 是存放会话令牌的 Cookie 名。
const sessionCookie = "hibari_session"

// middleware 把一组横切关注点按固定顺序串起来：
// 安全头 → Origin 校验 → 认证 → 限流 → handler。
func (s *Server) middleware(access accessLevel, limited bool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.setSecurityHeaders(w)

		if !s.checkOrigin(r) {
			writeError(w, http.StatusForbidden, codeForbidden, "跨站请求被拒绝")
			return
		}

		ctx := r.Context()
		if access != accessPublic {
			u, err := s.currentUser(r)
			if err != nil {
				writeDomainError(w, err)
				return
			}
			ctx = context.WithValue(ctx, ctxUser, u)
			r = r.WithContext(ctx)
		}

		if limited && !s.limiter.Allow(s.clientIP(r)) {
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusTooManyRequests, codeRateLimit, "请求过于频繁，请稍后再试")
			return
		}

		next(w, r)
	}
}

func (s *Server) setSecurityHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	h.Set("Content-Security-Policy",
		"default-src 'self'; "+
			"script-src 'self'; "+
			"connect-src 'self'; "+
			"img-src 'self' data: blob:; "+
			"media-src 'self' blob: https://*.baidu.com https://*.baidupcs.com https://*.bdstatic.com; "+
			"style-src 'self' 'unsafe-inline'; "+
			"frame-ancestors 'none'; object-src 'none'; base-uri 'none'; form-action 'self'")
}

// checkOrigin 实现 CSRF 防护：非安全方法要求 Origin 与本站一致。
// 浏览器对所有跨站 POST 都会带 Origin；不带 Origin 的是非浏览器客户端
// （curl、服务端调用），不受 CSRF 影响，放行。
func (s *Server) checkOrigin(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	// 同源请求的 Origin 形如 scheme://host[:port]，与 Host 头对齐即可。
	host := origin
	if i := strings.Index(origin, "://"); i >= 0 {
		host = origin[i+3:]
	}
	return strings.EqualFold(host, r.Host)
}

// currentUser 从 Cookie 解析会话。
func (s *Server) currentUser(r *http.Request) (identity.User, error) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return identity.User{}, identity.ErrSessionExpired
	}
	return s.identity.Authenticate(r.Context(), c.Value)
}

// clientIP 返回调用方 IP，仅在直连来源属于可信代理时才信任转发头。
// X-Forwarded-For 是「客户端, 代理1, 代理2」的追加式列表，左侧全部由客户端
// 可伪造。必须从右往左跳过可信代理，取第一个不可信地址，否则伪造一个
// 首元素就能绕过限流。
func (s *Server) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = strings.TrimSpace(r.RemoteAddr)
	}
	if !s.trustedProxy(host) {
		return host
	}

	fwd := r.Header.Get("X-Forwarded-For")
	if fwd == "" {
		if real := strings.TrimSpace(r.Header.Get("X-Real-IP")); net.ParseIP(real) != nil {
			return real
		}
		return host
	}

	parts := strings.Split(fwd, ",")
	leftmost := ""
	for i := len(parts) - 1; i >= 0; i-- {
		hop := strings.TrimSpace(parts[i])
		if net.ParseIP(hop) == nil {
			// 链上出现非法值，后面的都不可信，回落到直连地址。
			return host
		}
		leftmost = hop
		if !s.trustedProxy(hop) {
			return hop
		}
	}
	// 整条链都是可信代理，最左一跳才是真实客户端。
	return leftmost
}

func (s *Server) trustedProxy(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, n := range s.proxies {
		if n.Contains(parsed) {
			return true
		}
	}
	return false
}

// setSessionCookie 写入 HttpOnly 会话 Cookie。
func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, token string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.secureCookie(r),
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.secureCookie(r),
	})
}

func (s *Server) secureCookie(r *http.Request) bool {
	return s.forceSecureCookie || r.TLS != nil
}

// rateLimiter 是按 IP 的令牌桶。每个 IP 一个桶，定期回收空闲桶。
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64
	burst   float64
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newRateLimiter(perSecond int) *rateLimiter {
	if perSecond < 1 {
		perSecond = 10
	}
	rl := &rateLimiter{
		buckets: make(map[string]*bucket),
		rate:    float64(perSecond),
		burst:   float64(perSecond) * 2,
	}
	go rl.reap()
	return rl
}

func (rl *rateLimiter) Allow(key string) bool {
	now := time.Now()
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.buckets[key]
	if !ok {
		b = &bucket{tokens: rl.burst, last: now}
		rl.buckets[key] = b
	}
	// 按经过时间补充令牌。
	b.tokens += now.Sub(b.last).Seconds() * rl.rate
	if b.tokens > rl.burst {
		b.tokens = rl.burst
	}
	b.last = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// reap 定期删除长时间未使用的桶，防止内存无限增长。
func (rl *rateLimiter) reap() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := time.Now().Add(-10 * time.Minute)
		rl.mu.Lock()
		for k, b := range rl.buckets {
			if b.last.Before(cutoff) {
				delete(rl.buckets, k)
			}
		}
		rl.mu.Unlock()
	}
}

// parseLimit 解析分页或数量参数。
func parseLimit(raw string, def, max int) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}
