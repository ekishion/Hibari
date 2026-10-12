package server

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ekishion/jikeqingpan/internal/baidu"
	"github.com/ekishion/jikeqingpan/internal/config"
	"github.com/ekishion/jikeqingpan/internal/storage"
)

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": s.version})
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if err := s.baiduClient.ProbeBaiduSession(); err != nil {
		log.Printf("[readyz] baidu session probe failed: %v", err)
		writeJSONError(w, http.StatusServiceUnavailable, "not_ready", "百度会话不可用，请检查 baidu_cookie")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// handleFiles 获取文件列表，代理百度网盘 list API。
// 支持两种定位方式：{dir} 明文路径，或 {token} 目录短链（服务端解析）。
func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	var dir, token, password string
	var refresh bool
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
		var request struct {
			Dir      string `json:"dir"`
			Token    string `json:"token"`
			Password string `json:"password"`
			Refresh  bool   `json:"refresh"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeJSONError(w, http.StatusBadRequest, "bad_request", "请求内容有误，请刷新页面重试")
			return
		}
		dir = request.Dir
		token = request.Token
		password = strings.TrimSpace(request.Password)
		refresh = request.Refresh
	} else {
		dir = r.URL.Query().Get("dir")
		token = r.URL.Query().Get("token")
		password = strings.TrimSpace(r.URL.Query().Get("pwd"))
		if password == "" {
			password = strings.TrimSpace(r.URL.Query().Get("password"))
		}
		qRefresh := r.URL.Query().Get("refresh")
		refresh = qRefresh == "1" || strings.EqualFold(qRefresh, "true")
	}
	var sandboxRoot string
	if token != "" {
		resolved, ok, passwordRequired := s.dirLinks.ResolveWithPassword(token, password)
		if passwordRequired {
			writeJSONError(w, http.StatusUnauthorized, "share_password_required", "该分享目录受访问码保护，请输入提取码")
			return
		}
		if !ok {
			writeJSONError(w, http.StatusNotFound, "dirlink_not_found", "目录链接已失效，请从首页重新进入")
			return
		}
		sandboxRoot = path.Clean(resolved)
		cleanDir := path.Clean(dir)
		// 如果未指定子目录或请求的是根目录/自身，则定位到沙盒根目录
		if dir == "" || cleanDir == "/" || cleanDir == "." || cleanDir == sandboxRoot {
			dir = sandboxRoot
		} else {
			// 强制沙盒圈定：客户端请求的 dir 必须严格在 sandboxRoot 子树之下
			if !strings.HasPrefix(cleanDir, sandboxRoot+"/") {
				log.Printf("[SECURITY] 拦截目录短链越权逃逸: 授权根目录 %q, 试图访问 %q", sandboxRoot, cleanDir)
				writeJSONError(w, http.StatusForbidden, "path_not_allowed", "越界访问被拒绝：超出共享目录范围")
				return
			}
			dir = cleanDir
		}
	} else {
		// 非短链请求：必须校验全站鉴权（若开启了访问权限保护）
		if s.cfg.AuthEnabled() && !s.hasValidAccess(r) {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "请先验证访问权限")
			return
		}
	}
	if dir == "" {
		dir = "/"
	}
	if !config.IsValidBaiduPath(dir) {
		log.Printf("[WARN] 获取文件列表的非法路径被拒绝: %q", dir)
		writeJSONError(w, http.StatusBadRequest, "invalid_path", "文件路径无效")
		return
	}
	if !s.pathNavigable(dir) {
		writeJSONError(w, http.StatusForbidden, "path_not_allowed", "该目录不在共享范围内")
		return
	}

	var listBody []byte
	fromCache := false
	if !refresh {
		if cached, ok := s.cache.GetDirList(dir); ok {
			listBody = cached
			fromCache = true
		}
	}

	if listBody == nil {
		if refresh {
			s.cache.InvalidateDir(dir)
		}
		result, err := s.baiduClient.FetchFileList(dir)
		if err != nil {
			log.Printf("[ERROR] 获取文件列表失败: %v", err)
			writeJSONError(w, http.StatusBadGateway, "baidu_list_failed", "无法获取文件列表，请稍后重试")
			return
		}

		filtered, err := s.filterFileList(result.Body)
		if err != nil {
			log.Printf("[ERROR] 过滤共享范围失败: %v", err)
			writeJSONError(w, http.StatusBadGateway, "list_process_failed", "文件列表处理失败")
			return
		}
		s.cache.Update(filtered)
		s.cache.SetDirList(dir, filtered)
		s.markStateDirty()
		listBody = filtered
	}

	// 无论是缓存还是实时获取，都统一过一次范围过滤（防御 AllowedPaths 动态调整）
	filteredBody, err := s.filterFileList(listBody)
	if err != nil {
		log.Printf("[ERROR] 过滤共享范围失败: %v", err)
		writeJSONError(w, http.StatusBadGateway, "list_process_failed", "文件列表处理失败")
		return
	}

	publicBody, err := baidu.StripDownloadLinks(filteredBody)
	if err != nil {
		log.Printf("[ERROR] 清理文件列表直链失败: %v", err)
		writeJSONError(w, http.StatusBadGateway, "list_process_failed", "文件列表处理失败，请稍后重试")
		return
	}

	publicBody, err = injectResolvedDir(publicBody, dir, sandboxRoot)
	if err != nil {
		log.Printf("[ERROR] 注入目录信息失败: %v", err)
		writeJSONError(w, http.StatusBadGateway, "list_process_failed", "文件列表处理失败")
		return
	}

	if fromCache {
		w.Header().Set("X-Cache", "HIT")
	} else {
		w.Header().Set("X-Cache", "MISS")
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(publicBody)
}

// injectResolvedDir 在列表响应中带上当前目录与沙盒虚拟根：用 token 请求时前端据此还原路径并锁定沙盒边界。
func injectResolvedDir(body []byte, dir, sandboxRoot string) ([]byte, error) {
	var response map[string]json.RawMessage
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(dir)
	if err != nil {
		return nil, err
	}
	response["resolved_dir"] = raw
	if sandboxRoot != "" {
		sbRaw, err := json.Marshal(sandboxRoot)
		if err == nil {
			response["sandbox_root"] = sbRaw
		}
	}
	return json.Marshal(response)
}

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

func (s *Server) filterFileList(body []byte) ([]byte, error) {
	if s == nil || s.cfg == nil || len(s.cfg.AllowedPaths) == 0 {
		return body, nil
	}
	var response map[string]json.RawMessage
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	listRaw, ok := response["list"]
	if !ok {
		return body, nil
	}
	var list []map[string]json.RawMessage
	if err := json.Unmarshal(listRaw, &list); err != nil {
		return nil, err
	}
	filtered := make([]map[string]json.RawMessage, 0, len(list))
	for _, item := range list {
		var itemPath string
		if err := json.Unmarshal(item["path"], &itemPath); err != nil || !config.IsValidBaiduPath(itemPath) {
			continue
		}
		if s.pathVisible(itemPath) {
			filtered = append(filtered, item)
		}
	}
	cleanList, err := json.Marshal(filtered)
	if err != nil {
		return nil, err
	}
	response["list"] = cleanList
	response["list_items"] = json.RawMessage(strconv.Itoa(len(filtered)))
	return json.Marshal(response)
}

// handleDownload 仅创建不透明短链接；真实百度直链在 /d/{token} 时再解析。
func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
	var request struct {
		Path          string `json:"path"`
		Password      string `json:"password"`
		ExpireSeconds int    `json:"expire_seconds"`
		MaxUses       *int   `json:"max_uses"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "请求内容有误，请刷新页面重试")
		return
	}
	filePath := request.Path
	if filePath == "" {
		writeJSONError(w, http.StatusBadRequest, "missing_path", "请选择要下载的文件")
		return
	}
	if !config.IsValidBaiduPath(filePath) {
		log.Printf("[WARN] 非法路径请求被拒绝: %q", filePath)
		writeJSONError(w, http.StatusBadRequest, "invalid_path", "文件路径无效")
		return
	}
	if !s.pathAllowed(filePath) {
		writeJSONError(w, http.StatusForbidden, "path_not_allowed", "该文件不在共享范围内")
		return
	}

	// 创建短链前确认文件存在且非目录
	if _, err := s.baiduClient.EnsureFileMeta(filePath); err != nil {
		log.Printf("[WARN] 创建短链前校验失败: %v", err)
		writeJSONError(w, http.StatusNotFound, "file_not_found", "文件不存在或已被移动")
		return
	}

	password := strings.TrimSpace(request.Password)
	var customTTL time.Duration
	if request.ExpireSeconds > 0 {
		customTTL = time.Duration(request.ExpireSeconds) * time.Second
	}
	customMaxUses := -1
	if request.MaxUses != nil {
		customMaxUses = *request.MaxUses
	}

	token, err := s.shortLinks.CreateWithOptions(filePath, password, customTTL, customMaxUses)
	if err != nil {
		log.Printf("[ERROR] 创建短链接失败: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "shortlink_failed", "生成下载链接失败，请重试")
		return
	}
	s.markStateDirty()
	s.audit(r, "shortlink_issued", filePath)

	writeJSON(w, http.StatusOK, map[string]any{
		"urls": []map[string]string{
			{"url": "/d/" + token},
		},
		"token":        token,
		"url":          "/d/" + token,
		"has_password": password != "",
	})
}

// handleShareInfo 获取单文件短链公开元数据或校验提取码。
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
			"file_path":      meta.FilePath,
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
		"file_path":      meta.FilePath,
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

func (s *Server) handleShortDownload(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.URL.Path, "/d/")
	token = strings.Trim(token, "/")
	password := strings.TrimSpace(r.URL.Query().Get("pwd"))
	if password == "" {
		password = strings.TrimSpace(r.URL.Query().Get("password"))
	}
	isBrowserHTML := strings.Contains(r.Header.Get("Accept"), "text/html")
	isDownloadDirect := r.URL.Query().Get("download") == "1"

	// 浏览器直接访问 /d/{token} 且未显式指定 ?download=1 时，重定向至单文件独立落地页
	if isBrowserHTML && !isDownloadDirect {
		targetURL := "/?s=" + url.QueryEscape(token)
		if password != "" {
			targetURL += "&pwd=" + url.QueryEscape(password)
		}
		http.Redirect(w, r, targetURL, http.StatusFound)
		return
	}

	// 先探查不计入使用次数，直链解析成功后才消耗，避免解析失败白耗短链。
	filePath, ok, passwordRequired := s.shortLinks.ResolveWithPassword(token, password, false)
	if passwordRequired {
		if isBrowserHTML {
			http.Redirect(w, r, "/?s="+url.QueryEscape(token)+"&err=pwd", http.StatusFound)
			return
		}
		writeJSONError(w, http.StatusUnauthorized, "share_password_required", "该下载链接受提取码保护，请输入提取码")
		return
	}
	if !ok {
		if isBrowserHTML {
			http.Redirect(w, r, "/?s="+url.QueryEscape(token)+"&err=expired", http.StatusFound)
			return
		}
		writeJSONError(w, http.StatusNotFound, "shortlink_not_found", "下载链接已失效，请重新获取")
		return
	}
	if !s.pathAllowed(filePath) {
		if isBrowserHTML {
			http.Redirect(w, r, "/?s="+url.QueryEscape(token)+"&err=forbidden", http.StatusFound)
			return
		}
		writeJSONError(w, http.StatusForbidden, "path_not_allowed", "该文件不在共享范围内")
		return
	}
	dlink, err := s.baiduClient.GetBaiduDLink(filePath, r.Header.Get("User-Agent"))
	if err != nil {
		log.Printf("[ERROR] 获取百度直链失败: %v", err)
		if isBrowserHTML {
			http.Redirect(w, r, "/?s="+url.QueryEscape(token)+"&err=dlink", http.StatusFound)
			return
		}
		writeJSONError(w, http.StatusBadGateway, "dlink_failed", "下载失败，请稍后重试")
		return
	}
	if !baidu.IsAllowedBaiduDownloadURL(dlink) {
		log.Printf("[ERROR] 拒绝非白名单下载地址: %q", dlink)
		if isBrowserHTML {
			http.Redirect(w, r, "/?s="+url.QueryEscape(token)+"&err=dlink", http.StatusFound)
			return
		}
		writeJSONError(w, http.StatusBadGateway, "dlink_rejected", "下载失败，请稍后重试")
		return
	}
	if _, consumed, _ := s.shortLinks.ResolveWithPassword(token, password, true); !consumed {
		// 探查与消耗之间短链恰好过期或耗尽，按失效处理。
		if isBrowserHTML {
			http.Redirect(w, r, "/?s="+url.QueryEscape(token)+"&err=expired", http.StatusFound)
			return
		}
		writeJSONError(w, http.StatusNotFound, "shortlink_not_found", "下载链接已失效，请重新获取")
		return
	}
	s.markStateDirty()
	s.audit(r, "shortlink_accessed", filePath)
	http.Redirect(w, r, dlink, http.StatusFound)
}
