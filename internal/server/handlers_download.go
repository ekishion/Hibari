package server

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ekishion/jikeqingpan/internal/baidu"
	"github.com/ekishion/jikeqingpan/internal/config"
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
