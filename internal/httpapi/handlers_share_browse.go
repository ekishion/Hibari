package httpapi

import (
	"context"
	"io"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/ekishion/hibari/internal/library"
	"github.com/ekishion/hibari/internal/sharing"
)

// 目录分享的沙盒：所有路径都相对分享根目录解析，
// 越界即拒绝。这是分享页能安全浏览子目录的唯一依据。

type shareBrowseRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
	Dir      string `json:"dir"`
}

// shareScope 校验短链与提取码，并把请求路径圈进分享根目录。
// 返回分享记录与解析后的绝对路径。它不改变任何状态：
// 需要计次的调用方在自身全部校验通过后再显式调用 Consume。
func (s *Server) shareScope(ctx context.Context, token, password, raw string) (sharing.Share, string, error) {
	sh, err := s.sharing.Resolve(ctx, token, password)
	if err != nil {
		return sharing.Share{}, "", err
	}

	rel, err := library.CleanPath(raw)
	if err != nil {
		return sharing.Share{}, "", err
	}

	var abs string
	if sh.Kind == sharing.KindDir {
		// CleanPath 已拒绝 . 与 .. 段，path.Join 不会逃逸；
		// 下面仍用 IsUnder 复核一次，纵深防御。
		abs = path.Join(sh.Path, rel)
	} else {
		// 文件分享只有自身可访问。
		abs = sh.Path
	}

	if !library.IsUnder(abs, sh.Path) {
		return sharing.Share{}, "", library.ErrInvalid
	}
	if !s.cfg.PathAllowed(abs) {
		return sharing.Share{}, "", library.ErrInvalid
	}
	return sh, abs, nil
}

// handleShareBrowse 返回分享根目录或子目录的内容。浏览不计次。
func (s *Server) handleShareBrowse(w http.ResponseWriter, r *http.Request) {
	var req shareBrowseRequest
	if !decodeJSON(w, r, 4<<10, &req) {
		return
	}

	sh, abs, err := s.shareScope(r.Context(), req.Token, req.Password, req.Dir)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	if sh.Kind != sharing.KindDir {
		writeError(w, http.StatusBadRequest, codeBadRequest, "文件分享不支持浏览")
		return
	}

	entries, err := s.backend.List(r.Context(), abs)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	// 输出相对分享根的路径，避免泄露服务器真实目录结构。
	out := make([]entryDTO, 0, len(entries))
	for _, e := range entries {
		rel := strings.TrimPrefix(e.Path, sh.Path)
		if rel == "" {
			rel = "/"
		}
		dto := toEntryDTO(e)
		dto.Path = rel
		out = append(out, dto)
	}

	dirRel, _ := library.CleanPath(req.Dir)
	writeJSON(w, http.StatusOK, map[string]any{
		"dir":     dirRel,
		"entries": out,
	})
}

// handleShareRaw 让访客在沙盒内下载或预览单个文件。
// 只有显式下载（download=1）才计次；内联预览会发出多次 Range 请求，
// 计次会让一次播放烧光配额。
func (s *Server) handleShareRaw(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	password := r.URL.Query().Get("pwd")
	rel := r.URL.Query().Get("path")
	download := r.URL.Query().Get("download") == "1"

	sh, abs, err := s.shareScope(r.Context(), token, password, rel)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	entry, err := s.backend.Stat(r.Context(), abs)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	if entry.IsDir {
		writeError(w, http.StatusBadRequest, codeBadRequest, "不能直接下载目录")
		return
	}

	if download {
		// 计次放在全部校验之后，被拒的请求不烧次数。
		if _, err := s.sharing.Consume(r.Context(), sh.Token); err != nil {
			writeDomainError(w, err)
			return
		}
	}

	disposition := "inline"
	if download {
		disposition = "attachment"
	}
	s.audit(r, "share.browse", abs)

	rc, err := s.backend.Open(r.Context(), abs)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	defer rc.Close()

	ctype := mime.TypeByExtension(path.Ext(entry.Name))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Disposition",
		mime.FormatMediaType(disposition, map[string]string{"filename": entry.Name}))
	w.Header().Set("Cache-Control", "private, max-age=60")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	// 与 handleRaw 一致：可 seek 的后端支持 Range，否则整段转发。
	if rs, ok := rc.(io.ReadSeeker); ok {
		http.ServeContent(w, r, entry.Name, time.Time{}, rs)
		return
	}
	if _, err := io.Copy(w, rc); err != nil {
		s.logf("[WARN] 分享浏览传输中断: %v", err)
	}
}
