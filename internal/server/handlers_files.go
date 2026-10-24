package server

import (
	"encoding/json"
	"log"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/ekishion/jikeqingpan/internal/baidu"
	"github.com/ekishion/jikeqingpan/internal/config"
)

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
			// 带了密码仍要求提取码 ⇒ 密码不对，前端弹窗要显示可读错误
			if password != "" {
				writeJSONError(w, http.StatusUnauthorized, "share_password_incorrect", "提取码错误，请重新输入")
				return
			}
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
			// 强制沙盒圈定：客户端请求的 dir 必须严格在 sandboxRoot 子树之下。
			// 根目录短链的 sandboxRoot 为 "/"，前缀须归一为 "/" 而非 "//"
			prefix := strings.TrimSuffix(sandboxRoot, "/") + "/"
			if !strings.HasPrefix(cleanDir, prefix) {
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
