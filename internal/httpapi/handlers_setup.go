package httpapi

import (
	"errors"
	"net/http"

	"github.com/ekishion/hibari/internal/identity"
)

type setupRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// handleSetup 创建首个管理员。系统已有账号时永久关闭此入口。
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	var req setupRequest
	if !decodeJSON(w, r, 8<<10, &req) {
		return
	}
	// 是否已初始化由 INSERT 语句自身判定，避免 COUNT 与 INSERT 之间的竞态。
	user, err := s.identity.CreateAdmin(r.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, identity.ErrAlreadySetup) {
			writeError(w, http.StatusConflict, codeConflict, err.Error())
			return
		}
		writeDomainError(w, err)
		return
	}

	s.audit(r, "setup", user.Username)
	// 初始化成功后直接登录，省掉一次手动输入。
	token, _, err := s.identity.Login(
		r.Context(), user.Username, req.Password, r.UserAgent(), s.clientIP(r))
	if err == nil {
		s.setSessionCookie(w, r, token, s.cfg.SessionTTLDuration())
	}
	writeJSON(w, http.StatusCreated, map[string]any{"user": toUserDTO(user)})
}

type passwordRequest struct {
	Current string `json:"current_password"`
	New     string `json:"new_password"`
}

// handlePassword 修改当前登录用户的密码，成功后吊销全部会话。
func (s *Server) handlePassword(w http.ResponseWriter, r *http.Request) {
	u, ok := userFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, codeUnauth, identity.ErrSessionExpired.Error())
		return
	}
	var req passwordRequest
	if !decodeJSON(w, r, 8<<10, &req) {
		return
	}

	// 重认证，确认请求者知道当前密码。
	if _, err := s.identity.VerifyCredentials(r.Context(), u.Username, req.Current); err != nil {
		if errors.Is(err, identity.ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, codeUnauth, "当前密码不正确")
			return
		}
		writeDomainError(w, err)
		return
	}

	if err := s.identity.ChangePassword(r.Context(), u.ID, req.New); err != nil {
		writeDomainError(w, err)
		return
	}
	s.audit(r, "password.change", u.Username)
	s.clearSessionCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleAudit 返回最近的审计记录，供后台查看。
func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	if s.auditLog == nil {
		writeError(w, http.StatusNotImplemented, codeUnsupported, "当前未启用审计查询")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"), 100, 500)
	events, err := s.auditLog.Recent(r.Context(), limit)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}
