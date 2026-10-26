package httpapi

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/ekishion/hibari/internal/library"
)

type entryDTO struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	IsDir   bool   `json:"is_dir"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"mod_time"`
}

func toEntryDTO(e library.Entry) entryDTO {
	return entryDTO{
		Name:    e.Name,
		Path:    e.Path,
		IsDir:   e.IsDir,
		Size:    e.Size,
		ModTime: e.ModTime.Unix(),
	}
}

// resolvePath 解析并校验请求中的路径参数。
// 返回 false 表示已写入错误响应。
func (s *Server) resolvePath(w http.ResponseWriter, raw string) (string, bool) {
	p, err := library.CleanPath(raw)
	if err != nil {
		writeDomainError(w, err)
		return "", false
	}
	if !s.cfg.PathAllowed(p) {
		writeError(w, http.StatusForbidden, codeForbidden, "该路径不在允许访问的范围内")
		return "", false
	}
	return p, true
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.resolvePath(w, r.URL.Query().Get("dir"))
	if !ok {
		return
	}

	entries, err := s.backend.List(r.Context(), dir)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	// 按允许范围过滤，避免白名单外的兄弟目录出现在列表中。
	out := make([]entryDTO, 0, len(entries))
	for _, e := range entries {
		if !s.cfg.PathAllowed(e.Path) {
			continue
		}
		out = append(out, toEntryDTO(e))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"dir":     dir,
		"entries": out,
	})
}

// handleRaw 提供文件内容。后端支持直链时 302 到上游，
// 否则由本服务代理流式转发。
func (s *Server) handleRaw(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolvePath(w, r.URL.Query().Get("path"))
	if !ok {
		return
	}

	// download=1 触发附件下载，否则内联展示。
	disposition := "inline"
	if r.URL.Query().Get("download") == "1" {
		disposition = "attachment"
	}

	if s.linker != nil {
		url, err := s.linker.DirectURL(r.Context(), p, r.UserAgent())
		if err != nil {
			// 不回落 backend.Open：它内部调的就是同一个 DirectURL，
			// 必然同样失败，只会把一次失败放大成多次上游请求。
			writeDomainError(w, err)
			return
		}
		w.Header().Set("Content-Disposition",
			mime.FormatMediaType(disposition, map[string]string{"filename": path.Base(p)}))
		http.Redirect(w, r, url, http.StatusFound)
		return
	}

	rc, err := s.backend.Open(r.Context(), p)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	defer rc.Close()

	name := path.Base(p)
	ctype := mime.TypeByExtension(path.Ext(name))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Disposition",
		mime.FormatMediaType(disposition, map[string]string{"filename": name}))
	w.Header().Set("Cache-Control", "private, max-age=60")

	// 可 seek 的后端交给 ServeContent，它负责 Range、206 与 If-Range，
	// 浏览器才能拖动视频进度。不可 seek 的后端回落到整段转发。
	if rs, ok := rc.(io.ReadSeeker); ok {
		http.ServeContent(w, r, name, time.Time{}, rs)
		return
	}
	if _, err := io.Copy(w, rc); err != nil {
		// 响应已开始，无法再写错误体，只能记录。
		s.logf("[WARN] 传输文件 %q 中断: %v", p, err)
	}
}

func (s *Server) handleQuota(w http.ResponseWriter, r *http.Request) {
	if !s.backend.Caps().Quota {
		writeError(w, http.StatusNotImplemented, codeUnsupported, "当前后端不提供容量信息")
		return
	}
	q, err := s.backend.Quota(r.Context())
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total": q.Total,
		"used":  q.Used,
		"free":  q.Free(),
	})
}

type mkdirRequest struct {
	Dir  string `json:"dir"`
	Name string `json:"name"`
}

func (s *Server) handleMkdir(w http.ResponseWriter, r *http.Request) {
	if s.writer == nil {
		writeError(w, http.StatusNotImplemented, codeUnsupported, "当前后端不支持写操作")
		return
	}
	var req mkdirRequest
	if !decodeJSON(w, r, 4<<10, &req) {
		return
	}
	dir, ok := s.resolvePath(w, req.Dir)
	if !ok {
		return
	}
	created, err := s.writer.Mkdir(r.Context(), dir, req.Name)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	s.audit(r, "mkdir", created)
	writeJSON(w, http.StatusCreated, map[string]any{"path": created})
}

type renameRequest struct {
	Path    string `json:"path"`
	NewName string `json:"new_name"`
}

func (s *Server) handleRename(w http.ResponseWriter, r *http.Request) {
	if s.writer == nil {
		writeError(w, http.StatusNotImplemented, codeUnsupported, "当前后端不支持写操作")
		return
	}
	var req renameRequest
	if !decodeJSON(w, r, 4<<10, &req) {
		return
	}
	p, ok := s.resolvePath(w, req.Path)
	if !ok {
		return
	}
	renamed, err := s.writer.Rename(r.Context(), p, req.NewName)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	s.audit(r, "rename", p+" -> "+renamed)
	writeJSON(w, http.StatusOK, map[string]any{"path": renamed})
}

type pathsRequest struct {
	Paths []string `json:"paths"`
	Dest  string   `json:"dest"`
}

// resolvePaths 校验批量操作的路径列表。
func (s *Server) resolvePaths(w http.ResponseWriter, raw []string) ([]string, bool) {
	if len(raw) == 0 {
		writeError(w, http.StatusBadRequest, codeBadRequest, "未指定任何路径")
		return nil, false
	}
	if len(raw) > 100 {
		writeError(w, http.StatusBadRequest, codeBadRequest, "单次操作最多 100 项")
		return nil, false
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		p, ok := s.resolvePath(w, item)
		if !ok {
			return nil, false
		}
		out = append(out, p)
	}
	return out, true
}

func (s *Server) handleRemove(w http.ResponseWriter, r *http.Request) {
	if s.writer == nil {
		writeError(w, http.StatusNotImplemented, codeUnsupported, "当前后端不支持写操作")
		return
	}
	var req pathsRequest
	if !decodeJSON(w, r, 16<<10, &req) {
		return
	}
	paths, ok := s.resolvePaths(w, req.Paths)
	if !ok {
		return
	}
	if err := s.writer.Remove(r.Context(), paths); err != nil {
		writeDomainError(w, err)
		return
	}
	s.audit(r, "remove", strings.Join(paths, ", "))
	writeJSON(w, http.StatusOK, map[string]any{"removed": len(paths)})
}

// handleUpload 接收 multipart 上传。表单字段顺序无关，
// 但必须包含 dir、name 与 file。
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if s.writer == nil {
		writeError(w, http.StatusNotImplemented, codeUnsupported, "当前后端不支持上传")
		return
	}

	maxBytes := s.cfg.UploadMaxBytes()
	// 上传可能需要很久，放宽本次请求的读截止时间。
	// 服务器全局 ReadTimeout 只有 60 秒，不够传大文件。
	if err := http.NewResponseController(w).SetReadDeadline(time.Now().Add(30 * time.Minute)); err != nil {
		s.logf("[WARN] 无法调整上传读截止时间: %v", err)
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+(4<<20))
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, codeTooLarge,
				"文件超过大小上限")
			return
		}
		writeError(w, http.StatusBadRequest, codeBadRequest, "上传表单解析失败")
		return
	}
	defer r.MultipartForm.RemoveAll()

	dir, ok := s.resolvePath(w, r.FormValue("dir"))
	if !ok {
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		writeError(w, http.StatusBadRequest, codeBadRequest, "缺少文件名")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, "缺少文件内容")
		return
	}
	defer file.Close()

	if header.Size > maxBytes {
		writeError(w, http.StatusRequestEntityTooLarge, codeTooLarge, "文件超过大小上限")
		return
	}

	saved, err := s.writer.Put(r.Context(), dir, name, file, header.Size)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	s.audit(r, "upload", saved)
	writeJSON(w, http.StatusCreated, map[string]any{
		"path": saved,
		"size": header.Size,
	})
}
