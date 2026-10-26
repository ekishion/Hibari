package httpapi

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/ekishion/hibari/internal/sharing"
)

type shareCreateRequest struct {
	Path       string `json:"path"`
	Kind       string `json:"kind"`
	Password   string `json:"password"`
	TTLSeconds int    `json:"ttl_seconds"`
	MaxUses    int    `json:"max_uses"`
}

func (s *Server) handleShareCreate(w http.ResponseWriter, r *http.Request) {
	var req shareCreateRequest
	if !decodeJSON(w, r, 8<<10, &req) {
		return
	}
	p, ok := s.resolvePath(w, req.Path)
	if !ok {
		return
	}

	// 校验目标存在并推断类型，避免创建指向空路径的短链。
	entry, err := s.backend.Stat(r.Context(), p)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	kind := sharing.KindFile
	if entry.IsDir {
		kind = sharing.KindDir
	}
	if req.Kind != "" {
		if req.Kind != string(sharing.KindFile) && req.Kind != string(sharing.KindDir) {
			writeError(w, http.StatusBadRequest, codeBadRequest, "分享类型必须是 file 或 dir")
			return
		}
		// 显式类型必须与目标实际类型一致。
		if req.Kind != string(kind) {
			writeError(w, http.StatusBadRequest, codeBadRequest, "分享类型与目标不匹配")
			return
		}
	}

	password := strings.TrimSpace(req.Password)
	ttl := s.cfg.ShareTTLDuration()
	if req.TTLSeconds > 0 {
		ttl = time.Duration(req.TTLSeconds) * time.Second
	}
	maxUses := s.cfg.ShareMaxUses
	if req.MaxUses > 0 {
		maxUses = req.MaxUses
	}

	sh, err := s.sharing.Create(r.Context(), sharing.CreateOptions{
		Kind:     kind,
		Path:     p,
		Name:     path.Base(p),
		Password: password,
		TTL:      ttl,
		MaxUses:  maxUses,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}

	s.audit(r, "share.create", p)
	writeJSON(w, http.StatusCreated, shareDTO(sh))
}

type shareDTOShape struct {
	Token     string `json:"token"`
	Kind      string `json:"kind"`
	Path      string `json:"path"`
	Name      string `json:"name"`
	HasPwd    bool   `json:"has_password"`
	MaxUses   int    `json:"max_uses"`
	Uses      int    `json:"uses"`
	ExpiresAt int64  `json:"expires_at"`
	CreatedAt int64  `json:"created_at"`
}

func shareDTO(sh sharing.Share) shareDTOShape {
	return shareDTOShape{
		Token:     sh.Token,
		Kind:      string(sh.Kind),
		Path:      sh.Path,
		Name:      sh.Name,
		HasPwd:    sh.HasPassword(),
		MaxUses:   sh.MaxUses,
		Uses:      sh.Uses,
		ExpiresAt: sh.ExpiresAt.Unix(),
		CreatedAt: sh.CreatedAt.Unix(),
	}
}

func (s *Server) handleShareList(w http.ResponseWriter, r *http.Request) {
	shares, err := s.sharing.List(r.Context())
	if err != nil {
		writeDomainError(w, err)
		return
	}
	out := make([]shareDTOShape, 0, len(shares))
	for _, sh := range shares {
		out = append(out, shareDTO(sh))
	}
	writeJSON(w, http.StatusOK, map[string]any{"shares": out})
}

func (s *Server) handleShareRevoke(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if token == "" {
		writeError(w, http.StatusBadRequest, codeBadRequest, "缺少分享令牌")
		return
	}
	if err := s.sharing.Revoke(r.Context(), token); err != nil {
		writeDomainError(w, err)
		return
	}
	s.audit(r, "share.revoke", token)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type shareInfoRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

// shareInfoResponse 的字段在未解锁时全部留空，
// 让前端只能区分「需要提取码」与「已解锁」。
type shareInfoResponse struct {
	Token    string `json:"token"`
	Kind     string `json:"kind,omitempty"`
	Name     string `json:"name,omitempty"`
	HasPwd   bool   `json:"has_password"`
	Unlocked bool   `json:"unlocked"`
	Expires  int64  `json:"expires_at,omitempty"`
}

// handleShareInfo 供分享落地页查询元数据。
// 未通过提取码时不返回文件名等实质信息。
func (s *Server) handleShareInfo(w http.ResponseWriter, r *http.Request) {
	var req shareInfoRequest
	if !decodeJSON(w, r, 4<<10, &req) {
		return
	}

	sh, err := s.sharing.Resolve(r.Context(), req.Token, req.Password)
	if err != nil {
		if errors.Is(err, sharing.ErrPasswordNeeded) || errors.Is(err, sharing.ErrPasswordWrong) {
			// 告知前端需要提取码，但不泄露任何内容信息。
			writeJSON(w, http.StatusOK, shareInfoResponse{
				Token:    req.Token,
				HasPwd:   true,
				Unlocked: false,
			})
			return
		}
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, shareInfoResponse{
		Token:    sh.Token,
		Kind:     string(sh.Kind),
		Name:     sh.Name,
		HasPwd:   sh.HasPassword(),
		Unlocked: true,
		Expires:  sh.ExpiresAt.Unix(),
	})
}

// handleShareDownload 是分享链接的下载入口。
// 浏览器直接访问时跳转到落地页，带 download=1 时才真正下载。
func (s *Server) handleShareDownload(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	password := strings.TrimSpace(r.URL.Query().Get("pwd"))

	wantsFile := r.URL.Query().Get("download") == "1"
	if !wantsFile {
		target := "/s/" + token
		if password != "" {
			target += "?pwd=" + url.QueryEscape(password)
		}
		http.Redirect(w, r, target, http.StatusFound)
		return
	}

	sh, err := s.sharing.Resolve(r.Context(), token, password)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	if sh.Kind != sharing.KindFile {
		writeError(w, http.StatusBadRequest, codeBadRequest, "目录分享请通过浏览页访问")
		return
	}
	// 配置可能在分享创建之后收窄，这里按当前白名单复核一次。
	if !s.cfg.PathAllowed(sh.Path) {
		writeError(w, http.StatusForbidden, codeForbidden, "该分享已不可访问")
		return
	}

	// 计次放在全部校验之后，被拒的请求不烧次数。
	if _, err := s.sharing.Consume(r.Context(), sh.Token); err != nil {
		writeDomainError(w, err)
		return
	}

	s.audit(r, "share.download", sh.Path)

	if s.linker != nil {
		url, err := s.linker.DirectURL(r.Context(), sh.Path, r.UserAgent())
		if err == nil {
			http.Redirect(w, r, url, http.StatusFound)
			return
		}
		s.logf("[WARN] 分享直链获取失败，改用代理: %v", err)
	}

	rc, err := s.backend.Open(r.Context(), sh.Path)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	defer rc.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition",
		mime.FormatMediaType("attachment", map[string]string{"filename": sh.Name}))
	if _, err := io.Copy(w, rc); err != nil {
		s.logf("[WARN] 分享下载中断: %v", err)
	}
}
