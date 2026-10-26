package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ekishion/hibari/internal/identity"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type userDTO struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

func toUserDTO(u identity.User) userDTO {
	return userDTO{ID: u.ID, Username: u.Username, Role: u.Role}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !decodeJSON(w, r, 8<<10, &req) {
		return
	}
	if strings.TrimSpace(req.Username) == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, codeBadRequest, "请输入用户名与密码")
		return
	}

	token, user, err := s.identity.Login(
		r.Context(), req.Username, req.Password, r.UserAgent(), s.clientIP(r))
	if err != nil {
		if errors.Is(err, identity.ErrInvalidCredentials) {
			// 延迟响应，抬高暴力破解成本。
			time.Sleep(300 * time.Millisecond)
		}
		writeDomainError(w, err)
		return
	}

	s.setSessionCookie(w, r, token, s.cfg.SessionTTLDuration())
	s.audit(r, "login", user.Username)
	writeJSON(w, http.StatusOK, map[string]any{"user": toUserDTO(user)})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		_ = s.identity.Logout(r.Context(), c.Value)
	}
	s.clearSessionCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleStatus 返回前端启动所需的最小状态。
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	needsSetup, err := s.identity.NeedsSetup(r.Context())
	if err != nil {
		writeDomainError(w, err)
		return
	}

	resp := map[string]any{
		"needs_setup": needsSetup,
		"version":     s.version,
		"backend":     s.cfg.Backend,
		"caps": map[string]bool{
			"write":  s.writer != nil,
			"upload": s.writer != nil,
			"quota":  s.backend.Caps().Quota,
		},
		"authenticated": false,
	}
	if !needsSetup {
		if u, err := s.currentUser(r); err == nil {
			resp["authenticated"] = true
			resp["user"] = toUserDTO(u)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}
