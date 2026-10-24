package baidu

import (
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ekishion/jikeqingpan/internal/storage"
)

// LocatedownloadRand 使用 SHA-1 计算位于下载的 rand 参数。
// 哈希算法由百度 locatedownload API 契约固定（服务端按同样方式校验），
// 非本项目的加密选型，不存在可替换的更强算法。
func LocatedownloadRand(uk int64, sk string, nowMilli int64) string {
	data := fmt.Sprintf("%d%s%d0", uk, sk, nowMilli)
	h := sha1.New() // #nosec G401 -- 百度 API 契约要求的签名算法
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

// LocatedownloadSign 使用 MD5 计算位于下载的 sign 参数。
// 同上：MD5 为百度 API 契约固定要求，非安全敏感用途。
func LocatedownloadSign(fileMD5 string, fileID string, uk int64, nowMilli int64) string {
	data := fmt.Sprintf("%s_%d_%s_%d", fileMD5, uk, fileID, nowMilli)
	h := md5.New() // #nosec G501 -- 百度 API 契约要求的签名算法
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

// GetBaiduDLink 计算签名并向百度 locatedownload 接口获取直链（包含 sk 过期自动重试）
func (c *Client) GetBaiduDLink(filePath string, ua string) (string, error) {
	meta, err := c.EnsureFileMeta(filePath)
	if err != nil {
		return "", err
	}
	ua = NormalizeDownloadUA(ua)
	if cachedDLink, ok := c.cache.GetCachedDLink(filePath, ua); ok {
		return WithPrivateCacheControl(cachedDLink), nil
	}

	uk, sk, err := c.GetSession()
	if err != nil {
		return "", fmt.Errorf("获取百度Session失败: %w", err)
	}

	// 百度有时会用 HTTP 200 返回 errno 错误，因此响应解析失败也必须进入一次 Session 刷新重试。
	body, err := c.RequestDLink(filePath, meta, uk, sk, ua)
	dlink, parseErr := parseDLinkResponse(body)
	if err != nil || parseErr != nil {
		if err != nil {
			log.Printf("[WARN] 首次 locatedownload 失败: %v，尝试清除 sk 并重试", err)
		} else {
			log.Printf("[WARN] 首次 locatedownload 返回错误: %v，尝试清除 sk 并重试", parseErr)
		}
		c.ClearSession()

		uk, sk, err = c.GetSession()
		if err != nil {
			return "", fmt.Errorf("重新获取Session失败: %w", err)
		}
		body, err = c.RequestDLink(filePath, meta, uk, sk, ua)
		if err != nil {
			return "", fmt.Errorf("重试 locatedownload 失败: %w", err)
		}
		dlink, err = parseDLinkResponse(body)
		if err != nil {
			return "", fmt.Errorf("重试 locatedownload 失败: %w", err)
		}
	}

	dlink = WithPrivateCacheControl(dlink)
	c.cache.SetDLink(filePath, dlink, ua)
	return dlink, nil
}

// RequestDLink 发起 locatedownload 请求
func (c *Client) RequestDLink(filePath string, meta storage.FileMeta, uk int64, sk, ua string) ([]byte, error) {
	nowMilli := time.Now().UnixMilli()
	randVal := LocatedownloadRand(uk, sk, nowMilli)
	signVal := LocatedownloadSign(meta.MD5, strconv.FormatInt(meta.FsID, 10), uk, nowMilli)
	locateURL := fmt.Sprintf(
		c.baseURL+"/youth/api/locatedownload?app_id=%s&clienttype=0&web=1&devuid=0&dp-logid=%d&path=%s&rand=%s&sign=%s&time=%d",
		url.QueryEscape(c.cfg.BaiduAppID),
		time.Now().UnixNano(),
		url.QueryEscape(filePath),
		randVal,
		signVal,
		nowMilli,
	)
	return c.BaiduGet(locateURL, ua)
}

func parseDLinkResponse(body []byte) (string, error) {
	if len(body) == 0 {
		return "", fmt.Errorf("locatedownload 响应为空")
	}
	var resp struct {
		Errno   int    `json:"errno"`
		ShowMsg string `json:"show_msg"`
		URL     string `json:"url"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("解析 locatedownload 响应失败: %w", err)
	}
	if resp.Errno != 0 || resp.URL == "" {
		return "", fmt.Errorf("百度 locatedownload 返回错误 errno=%d, msg=%q", resp.Errno, resp.ShowMsg)
	}
	if !IsAllowedBaiduDownloadURL(resp.URL) {
		return "", fmt.Errorf("百度 locatedownload 返回了非白名单下载地址")
	}
	return resp.URL, nil
}

// DownloadJSONResponse 构造下载返回的 JSON 结构体
func DownloadJSONResponse(dlink string) ([]byte, error) {
	return json.Marshal(struct {
		URLs []struct {
			URL string `json:"url"`
		} `json:"urls"`
	}{URLs: []struct {
		URL string `json:"url"`
	}{{URL: dlink}}})
}

// IsAllowedBaiduDownloadURL 校验直链是否指向百度下载相关域名，避免开放重定向。
func IsAllowedBaiduDownloadURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "baidu.com" || host == "baidupcs.com" || host == "bdstatic.com" {
		return true
	}
	for _, suffix := range []string{".baidu.com", ".baidupcs.com", ".bdstatic.com"} {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

// WithPrivateCacheControl 附加 response-cache-control=private
func WithPrivateCacheControl(dlink string) string {
	if strings.Contains(dlink, "response-cache-control=") {
		return dlink
	}
	sep := "&"
	if !strings.Contains(dlink, "?") {
		sep = "?"
	}
	return dlink + sep + "response-cache-control=private"
}
