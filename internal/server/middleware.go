package server

import (
	"net/http"
)

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
		scriptSrc := "script-src 'self'"
		if s.cspScriptNonce != "" {
			scriptSrc += " 'nonce-" + s.cspScriptNonce + "'"
		}
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; "+scriptSrc+"; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; "+
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
