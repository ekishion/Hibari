package server

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ekishion/jikeqingpan/internal/baidu"
	"github.com/ekishion/jikeqingpan/internal/config"
)

// handleReadme 读取当前目录中的 README 文件，并返回给前端安全渲染。
func (s *Server) handleReadme(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
	var request struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "请求内容有误，请刷新页面重试")
		return
	}
	readmePath := request.Path
	if !config.IsValidBaiduPath(readmePath) {
		writeJSONError(w, http.StatusBadRequest, "invalid_path", "文件路径无效")
		return
	}
	readmeNameStr := readmeName(readmePath)
	if !isReadmeFileName(readmeNameStr) {
		writeJSONError(w, http.StatusNotFound, "readme_not_found", "README 不存在")
		return
	}
	if !s.pathAllowed(readmePath) {
		writeJSONError(w, http.StatusForbidden, "path_not_allowed", "该目录不在共享范围内")
		return
	}

	if cachedName, cachedContent, ok := s.cache.GetReadme(readmePath); ok {
		w.Header().Set("X-Cache", "HIT")
		writeJSON(w, http.StatusOK, map[string]any{
			"found":   true,
			"name":    cachedName,
			"content": cachedContent,
		})
		return
	}

	dlink, err := s.baiduClient.GetBaiduDLink(readmePath, r.Header.Get("User-Agent"))
	if err != nil || !baidu.IsAllowedBaiduDownloadURL(dlink) {
		writeJSONError(w, http.StatusBadGateway, "readme_link_failed", "无法准备 README")
		return
	}
	upstream, err := s.httpClient.Get(dlink)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "readme_fetch_failed", "无法读取 README")
		return
	}
	defer upstream.Body.Close()
	if upstream.StatusCode < http.StatusOK || upstream.StatusCode >= http.StatusMultipleChoices {
		writeJSONError(w, http.StatusBadGateway, "readme_fetch_failed", "README 暂时不可用")
		return
	}
	maxReadmeBytes := s.cfg.ReadmeMaxBytes()
	body, err := io.ReadAll(io.LimitReader(upstream.Body, int64(maxReadmeBytes)+1))
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "readme_fetch_failed", "无法读取 README")
		return
	}
	if len(body) > maxReadmeBytes {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "readme_too_large", "README 超过大小上限，无法展示")
		return
	}
	if !utf8.Valid(body) {
		writeJSONError(w, http.StatusUnsupportedMediaType, "readme_not_text", "README 不是可展示的文本文件")
		return
	}

	content := string(body)
	s.cache.SetReadme(readmePath, readmeNameStr, content)

	w.Header().Set("X-Cache", "MISS")
	writeJSON(w, http.StatusOK, map[string]any{
		"found":   true,
		"name":    readmeNameStr,
		"content": content,
	})
}

func readmeName(filePath string) string {
	if index := strings.LastIndex(filePath, "/"); index >= 0 {
		return filePath[index+1:]
	}
	return filePath
}

// textPreviewExts 允许文本预览的扩展名白名单（与 static/preview.js 的 TEXT_PREVIEW_EXTS 保持同步）。
var textPreviewExts = map[string]struct{}{
	"txt": {}, "md": {}, "markdown": {}, "json": {}, "xml": {}, "yaml": {}, "yml": {},
	"csv": {}, "log": {}, "ini": {}, "conf": {}, "sh": {}, "bat": {}, "ps1": {},
	"py": {}, "js": {}, "ts": {}, "go": {}, "c": {}, "h": {}, "cpp": {}, "hpp": {},
	"java": {}, "rs": {}, "sql": {}, "toml": {}, "html": {}, "css": {},
}

func isTextPreviewableExt(name string) bool {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	_, ok := textPreviewExts[ext]
	return ok
}

// handleTextPreview 返回文本/代码文件内容，供前端在灯箱中安全渲染。
// 内容以 JSON 字符串回传，前端用 textContent/安全渲染器展示，无注入面。
func (s *Server) handleTextPreview(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
	var request struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "请求内容有误，请刷新页面重试")
		return
	}
	if !config.IsValidBaiduPath(request.Path) {
		writeJSONError(w, http.StatusBadRequest, "invalid_path", "文件路径无效")
		return
	}
	if !s.pathAllowed(request.Path) {
		writeJSONError(w, http.StatusForbidden, "path_not_allowed", "该文件不在共享范围内")
		return
	}
	if !isTextPreviewableExt(request.Path) {
		writeJSONError(w, http.StatusUnsupportedMediaType, "text_not_allowed", "该文件类型不支持文本预览")
		return
	}
	if meta, err := s.baiduClient.EnsureFileMeta(request.Path); err != nil || meta.IsDir == 1 {
		writeJSONError(w, http.StatusNotFound, "file_not_found", "文件不存在或已被移动")
		return
	}
	dlink, err := s.baiduClient.GetBaiduDLink(request.Path, r.Header.Get("User-Agent"))
	if err != nil || !baidu.IsAllowedBaiduDownloadURL(dlink) {
		writeJSONError(w, http.StatusBadGateway, "text_link_failed", "无法准备文本预览")
		return
	}
	upstream, err := s.httpClient.Get(dlink)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "text_fetch_failed", "无法读取文本内容")
		return
	}
	defer upstream.Body.Close()
	if upstream.StatusCode < http.StatusOK || upstream.StatusCode >= http.StatusMultipleChoices {
		writeJSONError(w, http.StatusBadGateway, "text_fetch_failed", "文本内容暂时不可用")
		return
	}
	// 与 README 同一文本内容上限配置，避免两处限制漂移。
	maxTextBytes := s.cfg.ReadmeMaxBytes()
	body, err := io.ReadAll(io.LimitReader(upstream.Body, int64(maxTextBytes)+1))
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "text_fetch_failed", "无法读取文本内容")
		return
	}
	truncated := false
	if len(body) > maxTextBytes {
		body = body[:maxTextBytes]
		truncated = true
	}
	if truncated {
		// 截断点可能落在多字节字符中间，最多回退 UTF-8 单字符的最大字节数
		for i := 0; i < utf8.UTFMax && len(body) > 0 && !utf8.Valid(body); i++ {
			body = body[:len(body)-1]
		}
	}
	if !utf8.Valid(body) {
		writeJSONError(w, http.StatusUnsupportedMediaType, "text_not_text", "该文件不是文本内容")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"found":     true,
		"name":      readmeName(request.Path),
		"content":   string(body),
		"truncated": truncated,
	})
}

func isReadmeFileName(name string) bool {
	switch strings.ToLower(name) {
	case "readme.md", "readme.markdown", "readme.txt", "readme":
		return true
	default:
		return false
	}
}

// handlePreview 提供图片预览流式输出。
func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
	var request struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "请求内容有误，请刷新页面重试")
		return
	}
	if !config.IsValidBaiduPath(request.Path) {
		writeJSONError(w, http.StatusBadRequest, "invalid_path", "文件路径无效")
		return
	}
	if !s.pathAllowed(request.Path) {
		writeJSONError(w, http.StatusForbidden, "path_not_allowed", "该文件不在共享范围内")
		return
	}
	meta, err := s.baiduClient.EnsureFileMeta(request.Path)
	if err != nil || meta.IsDir == 1 {
		writeJSONError(w, http.StatusNotFound, "file_not_found", "图片不存在或已被移动")
		return
	}
	dlink, err := s.baiduClient.GetBaiduDLink(request.Path, r.Header.Get("User-Agent"))
	if err != nil || !baidu.IsAllowedBaiduDownloadURL(dlink) {
		writeJSONError(w, http.StatusBadGateway, "preview_link_failed", "无法准备图片预览")
		return
	}
	upstream, err := s.httpClient.Get(dlink)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "preview_fetch_failed", "无法读取图片预览")
		return
	}
	defer upstream.Body.Close()
	if upstream.StatusCode < http.StatusOK || upstream.StatusCode >= http.StatusMultipleChoices {
		writeJSONError(w, http.StatusBadGateway, "preview_fetch_failed", "图片预览暂时不可用")
		return
	}
	maxPreviewBytes := s.cfg.PreviewMaxBytes()
	// 上游明确声明超限时直接拒绝，避免白拉整份流量。
	if upstream.ContentLength > int64(maxPreviewBytes) {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "preview_too_large", "图片超过可预览的大小上限")
		return
	}
	// 只缓冲嗅探所需的前 512 字节，其余流式转发，避免大图整块驻留内存。
	sniff := make([]byte, 512)
	n, _ := io.ReadFull(upstream.Body, sniff)
	contentType := http.DetectContentType(sniff[:n])
	if !strings.HasPrefix(strings.ToLower(contentType), "image/") {
		contentType = mime.TypeByExtension(strings.ToLower(filepath.Ext(request.Path)))
	}
	if !strings.HasPrefix(strings.ToLower(contentType), "image/") {
		writeJSONError(w, http.StatusUnsupportedMediaType, "preview_not_image", "该文件不是可预览的图片")
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("Cache-Control", "private, no-store")
	if upstream.ContentLength > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(upstream.ContentLength, 10))
	}
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(sniff[:n]); err != nil {
		return
	}
	// 无 Content-Length 的上游靠限读兜底；超限截断的图片无法渲染，属可接受的降级。
	_, _ = io.Copy(w, io.LimitReader(upstream.Body, int64(maxPreviewBytes)-int64(n)))
}

// handleStream 无状态直链中转：校验路径后 302 到百度直链。
// 不创建短链、不写持久化状态、不出现在分享管理列表，
// 因此在线预览与直接下载都不会产生对外可分享的链接。
// 携带 X-Stream-Probe 请求头时仅做校验并返回 JSON，
// 供前端在赋值媒体 src 或触发下载前获得可读错误（与图片/文本预览的错误体验一致）。
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	filePath := r.URL.Query().Get("path")
	if !config.IsValidBaiduPath(filePath) {
		writeJSONError(w, http.StatusBadRequest, "invalid_path", "文件路径无效")
		return
	}
	if !s.pathAllowed(filePath) {
		writeJSONError(w, http.StatusForbidden, "path_not_allowed", "该文件不在共享范围内")
		return
	}
	meta, err := s.baiduClient.EnsureFileMeta(filePath)
	if err != nil || meta.IsDir == 1 {
		writeJSONError(w, http.StatusNotFound, "file_not_found", "文件不存在或已被移动")
		return
	}
	if r.Header.Get("X-Stream-Probe") != "" {
		// 探测请求：只校验，不解析直链。审计也记在这一步——每个用户动作恰好一次，
		// 避免播放中拖动进度条产生的分段 Range 请求刷满审计日志。
		s.audit(r, "stream_accessed", filePath)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	dlink, err := s.baiduClient.GetBaiduDLink(filePath, r.Header.Get("User-Agent"))
	if err != nil || !baidu.IsAllowedBaiduDownloadURL(dlink) {
		writeJSONError(w, http.StatusBadGateway, "stream_link_failed", "无法获取下载直链，请稍后重试")
		return
	}
	http.Redirect(w, r, dlink, http.StatusFound)
}
