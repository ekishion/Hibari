package server

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/ekishion/jikeqingpan/internal/config"
	"github.com/ekishion/jikeqingpan/internal/storage"
)

// handleDirLink 为目录生成地址栏短链（/?d=令牌），隐藏真实路径。
func (s *Server) handleDirLink(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
	var request struct {
		Dir           string `json:"dir"`
		Password      string `json:"password"`
		ExpireSeconds int    `json:"expire_seconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "请求内容有误，请刷新页面重试")
		return
	}
	dir := request.Dir
	if dir == "" {
		dir = "/"
	}
	if !config.IsValidBaiduPath(dir) {
		writeJSONError(w, http.StatusBadRequest, "invalid_path", "目录路径无效")
		return
	}
	if !s.pathNavigable(dir) {
		writeJSONError(w, http.StatusForbidden, "path_not_allowed", "该目录不在共享范围内")
		return
	}
	password := strings.TrimSpace(request.Password)
	var customTTL time.Duration
	if request.ExpireSeconds > 0 {
		customTTL = time.Duration(request.ExpireSeconds) * time.Second
	}
	token, err := s.dirLinks.CreateWithOptions(dir, password, customTTL)
	if err != nil {
		log.Printf("[ERROR] 创建目录短链失败: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "dirlink_failed", "生成目录链接失败，请重试")
		return
	}
	s.markStateDirty()
	s.audit(r, "dirlink_issued", dir)
	writeJSON(w, http.StatusOK, map[string]any{
		"token":        token,
		"url":          "/?d=" + token,
		"has_password": password != "",
	})
}

// handleShareInfo 获取单文件短链公开元数据或校验提取码。
// 响应刻意不含 file_path：那是内部网盘绝对路径，访客凭 token + download_url 即可完成下载。
func (s *Server) handleShareInfo(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
	var request struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "请求内容有误")
		return
	}
	token := strings.TrimSpace(request.Token)
	if !storage.ShortLinkTokenRe.MatchString(token) {
		writeJSONError(w, http.StatusBadRequest, "invalid_token", "分享链接格式无效")
		return
	}

	meta, ok := s.shortLinks.GetMeta(token)
	if !ok {
		writeJSONError(w, http.StatusNotFound, "shortlink_not_found", "分享链接已失效或已被取消")
		return
	}

	password := strings.TrimSpace(request.Password)
	passwordRequired := meta.HasPassword

	// 如果需要提取码，但未输入或密码错误
	if passwordRequired {
		if password == "" {
			writeJSON(w, http.StatusOK, map[string]any{
				"token":          token,
				"file_name":      meta.FileName,
				"has_password":   true,
				"password_valid": false,
				"expires_at":     meta.ExpiresAt.Unix(),
				"max_uses":       meta.MaxUses,
				"uses":           meta.Uses,
			})
			return
		}
		// 校验密码（不消耗使用次数）
		filePath, verified, pwdReq := s.shortLinks.ResolveWithPassword(token, password, false)
		if pwdReq || !verified {
			writeJSONError(w, http.StatusUnauthorized, "share_password_incorrect", "提取码错误，请重新输入")
			return
		}
		// 密码正确，获取文件大小等信息
		fileSize := int64(0)
		if fMeta, err := s.baiduClient.EnsureFileMeta(filePath); err == nil {
			fileSize = fMeta.Size
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"token":          token,
			"file_name":      meta.FileName,
			"has_password":   true,
			"password_valid": true,
			"size":           fileSize,
			"expires_at":     meta.ExpiresAt.Unix(),
			"max_uses":       meta.MaxUses,
			"uses":           meta.Uses,
			"download_url":   "/d/" + token + "?download=1&pwd=" + url.QueryEscape(password),
		})
		return
	}

	// 无需密码
	filePath, resolved := s.shortLinks.Resolve(token, false)
	if !resolved {
		writeJSONError(w, http.StatusNotFound, "shortlink_not_found", "分享链接已失效或已被取消")
		return
	}
	fileSize := int64(0)
	if fMeta, err := s.baiduClient.EnsureFileMeta(filePath); err == nil {
		fileSize = fMeta.Size
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token":          token,
		"file_name":      meta.FileName,
		"has_password":   false,
		"password_valid": true,
		"size":           fileSize,
		"expires_at":     meta.ExpiresAt.Unix(),
		"max_uses":       meta.MaxUses,
		"uses":           meta.Uses,
		"download_url":   "/d/" + token + "?download=1",
	})
}

// handleSharesList 列出当前系统中所有活跃的文件分享和目录分享。
func (s *Server) handleSharesList(w http.ResponseWriter, r *http.Request) {
	fileShares := s.shortLinks.List()
	dirShares := s.dirLinks.List()

	sort.Slice(fileShares, func(i, j int) bool {
		return fileShares[i].CreatedAt.After(fileShares[j].CreatedAt)
	})
	sort.Slice(dirShares, func(i, j int) bool {
		return dirShares[i].CreatedAt.After(dirShares[j].CreatedAt)
	})

	type fileItem struct {
		Token       string `json:"token"`
		FilePath    string `json:"file_path"`
		FileName    string `json:"file_name"`
		HasPassword bool   `json:"has_password"`
		ExpiresAt   int64  `json:"expires_at"` // unix 秒级时间戳
		CreatedAt   int64  `json:"created_at"` // unix 秒级时间戳
		MaxUses     int    `json:"max_uses"`
		Uses        int    `json:"uses"`
		URL         string `json:"url"`
	}

	type dirItem struct {
		Token       string `json:"token"`
		Dir         string `json:"dir"`
		DirName     string `json:"dir_name"`
		HasPassword bool   `json:"has_password"`
		ExpiresAt   int64  `json:"expires_at"` // unix 秒级时间戳
		CreatedAt   int64  `json:"created_at"` // unix 秒级时间戳
		URL         string `json:"url"`
	}

	files := make([]fileItem, 0, len(fileShares))
	for _, f := range fileShares {
		files = append(files, fileItem{
			Token:       f.Token,
			FilePath:    f.FilePath,
			FileName:    f.FileName,
			HasPassword: f.HasPassword,
			ExpiresAt:   f.ExpiresAt.Unix(),
			CreatedAt:   f.CreatedAt.Unix(),
			MaxUses:     f.MaxUses,
			Uses:        f.Uses,
			URL:         "/d/" + f.Token,
		})
	}

	dirs := make([]dirItem, 0, len(dirShares))
	for _, d := range dirShares {
		dirs = append(dirs, dirItem{
			Token:       d.Token,
			Dir:         d.Dir,
			DirName:     d.DirName,
			HasPassword: d.HasPassword,
			ExpiresAt:   d.ExpiresAt.Unix(),
			CreatedAt:   d.CreatedAt.Unix(),
			URL:         "/?d=" + d.Token,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"files": files,
		"dirs":  dirs,
		"total": len(files) + len(dirs),
	})
}

// handleShareRevoke 手动撤销指定的分享短链接（支持文件短链与目录短链）。
func (s *Server) handleShareRevoke(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
	var request struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "请求格式错误")
		return
	}
	token := strings.TrimSpace(request.Token)
	if !storage.ShortLinkTokenRe.MatchString(token) {
		writeJSONError(w, http.StatusBadRequest, "invalid_token", "分享令牌格式无效")
		return
	}

	revoked := s.shortLinks.Revoke(token)
	shareType := "file"
	if !revoked {
		revoked = s.dirLinks.Revoke(token)
		shareType = "dir"
	}

	if !revoked {
		writeJSONError(w, http.StatusNotFound, "share_not_found", "分享链接不存在或已过期失效")
		return
	}

	s.markStateDirty()
	s.audit(r, "share_revoked", token)

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"token": token,
		"type":  shareType,
	})
}
