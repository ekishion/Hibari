package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	enabled := s.cfg.AuthEnabled()
	authenticated := !enabled || s.hasValidAccess(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"auth_required":         enabled,
		"authenticated":         authenticated,
		"show_readme":           s.cfg.ShowReadmeEnabled(),
		"show_readme_overview":  s.cfg.ShowReadmeOverviewEnabled(),
		"share_password_length": s.cfg.GetSharePasswordLength(),
		"upload_enabled":        s.cfg.UploadEnabled,
		"upload_max_mb":         s.cfg.UploadMaxMB,
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.AuthEnabled() {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "auth_required": false})
		return
	}
	// 登录失败指数退避：锁定期内直接拒绝，不比对令牌。
	ip := s.requestClientIP(r)
	now := time.Now()
	if ok, retryAfter := s.loginGuard.Allow(ip, now); !ok {
		secs := int(retryAfter.Seconds())
		if secs < 1 {
			secs = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		writeJSONError(w, http.StatusTooManyRequests, "login_locked", "验证过于频繁，请稍后再试")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
	var request struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "请求内容有误，请刷新页面重试")
		return
	}
	token := strings.TrimSpace(request.Token)
	if subtleCompareToken(token, s.cfg.AccessToken) {
		s.loginGuard.RecordSuccess(ip)
		if err := s.issueSession(w, r); err != nil {
			log.Printf("[ERROR] 签发会话令牌失败: %v", err)
			writeJSONError(w, http.StatusInternalServerError, "session_issue_failed", "验证失败，请稍后重试")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	s.loginGuard.RecordFailure(ip, now)
	writeJSONError(w, http.StatusUnauthorized, "invalid_token", "访问令牌不正确，请检查后重试")
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.clearAccessCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
