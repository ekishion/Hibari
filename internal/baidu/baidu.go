package baidu

import (
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ekishion/jikeqingpan/internal/config"
	"github.com/ekishion/jikeqingpan/internal/storage"
)

const (
	FileListPageSize        = 100
	DefaultFileListMaxPages = 15
	BaiduClientTimeout      = 15 * time.Second
	DefaultBaiduUA          = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"
)

// ListFetchResult 目录列表拉取结果（含截断信息）
type ListFetchResult struct {
	Body      []byte
	Truncated bool
	Pages     int
	Items     int
}

// Client 百度网盘青春版官方接口客户端
type Client struct {
	cfg          *config.Config
	cache        *storage.FileListCache
	httpClient   *http.Client
	uploadClient *http.Client
	baseURL      string

	sessionMu sync.Mutex
	uk        int64
	sk        string
	sessionAt time.Time
}

// NewClient 创建百度网盘客户端
func NewClient(cfg *config.Config, cache *storage.FileListCache, httpClient *http.Client, baseURL string) *Client {
	if baseURL == "" {
		baseURL = "https://pan.baidu.com"
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: BaiduClientTimeout}
	}
	// 分片上传体量大、耗时远超普通 API，用独立的长超时客户端；
	// Transport 沿用注入实例，保证测试可整体替换。
	uploadClient := &http.Client{Timeout: UploadChunkTimeout, Transport: httpClient.Transport}
	return &Client{
		cfg:          cfg,
		cache:        cache,
		httpClient:   httpClient,
		uploadClient: uploadClient,
		baseURL:      baseURL,
	}
}

// FetchFileList 拉取百度网盘文件列表（自动翻页）。下载直链只在服务端按需生成。
func (c *Client) FetchFileList(dir string) (*ListFetchResult, error) {
	if dir == "" {
		dir = "/"
	}
	maxPages := DefaultFileListMaxPages
	if c.cfg != nil && c.cfg.ListMaxPagesCount() > 0 {
		maxPages = c.cfg.ListMaxPagesCount()
	}

	var (
		mergedList []json.RawMessage
		baseResp   map[string]json.RawMessage
		truncated  bool
		pages      int
	)

	for page := 1; page <= maxPages; page++ {
		apiURL := fmt.Sprintf(
			c.baseURL+"/youth/api/list?clienttype=0&app_id=%s&web=1&order=time&desc=1&num=%d&page=%d&dlink=1&dir=%s",
			url.QueryEscape(c.cfg.BaiduAppID),
			FileListPageSize,
			page,
			url.QueryEscape(dir),
		)
		body, err := c.BaiduGet(apiURL, "")
		if err != nil {
			return nil, err
		}

		var resp map[string]json.RawMessage
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("解析文件列表失败: %w", err)
		}
		if page == 1 {
			baseResp = resp
		}
		pages = page

		// 百度业务错误（errno!=0）直接返回原始响应，由上层/缓存逻辑处理。
		if errnoRaw, ok := resp["errno"]; ok {
			var errno int
			if json.Unmarshal(errnoRaw, &errno) == nil && errno != 0 {
				if page == 1 {
					return &ListFetchResult{Body: body, Pages: 1}, nil
				}
				break
			}
		}

		listRaw, ok := resp["list"]
		if !ok {
			if page == 1 {
				return &ListFetchResult{Body: body, Pages: 1}, nil
			}
			break
		}
		var pageList []json.RawMessage
		if err := json.Unmarshal(listRaw, &pageList); err != nil {
			return nil, fmt.Errorf("解析文件列表 list 失败: %w", err)
		}
		mergedList = append(mergedList, pageList...)
		if len(pageList) < FileListPageSize {
			break
		}
		if page == maxPages {
			truncated = true
			log.Printf("[WARN] 目录 %q 达到翻页上限 %d 页（每页 %d），可能仍有未加载文件", dir, maxPages, FileListPageSize)
		}
	}

	if baseResp == nil {
		return nil, fmt.Errorf("文件列表响应为空")
	}
	cleanList, err := json.Marshal(mergedList)
	if err != nil {
		return nil, err
	}
	baseResp["list"] = cleanList
	// 附加服务端元信息，前端可用于提示截断。
	baseResp["truncated"] = json.RawMessage(strconv.FormatBool(truncated))
	baseResp["list_pages"] = json.RawMessage(strconv.Itoa(pages))
	baseResp["list_items"] = json.RawMessage(strconv.Itoa(len(mergedList)))
	baseResp["list_page_limit"] = json.RawMessage(strconv.Itoa(maxPages * FileListPageSize))

	out, err := json.Marshal(baseResp)
	if err != nil {
		return nil, err
	}
	return &ListFetchResult{
		Body:      out,
		Truncated: truncated,
		Pages:     pages,
		Items:     len(mergedList),
	}, nil
}

// StripDownloadLinks 从响应中剔除 dlink 字段，保证直链不暴露给普通列表接口。
func StripDownloadLinks(body []byte) ([]byte, error) {
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
	for _, item := range list {
		delete(item, "dlink")
	}
	cleanList, err := json.Marshal(list)
	if err != nil {
		return nil, err
	}
	response["list"] = cleanList
	return json.Marshal(response)
}

// fetchUserSession 从百度接口获取 uk 与 sk
func (c *Client) fetchUserSession() (int64, string, error) {
	// 1. 尝试从 /youth/api/user/getinfo 获取 uk/sk
	apiURL := fmt.Sprintf(
		c.baseURL+"/youth/api/user/getinfo?app_id=%s&clienttype=0&web=1&need_selfinfo=1",
		url.QueryEscape(c.cfg.BaiduAppID),
	)
	body, err := c.BaiduGet(apiURL, "")
	var uk int64
	var sk string
	if err == nil {
		var resp struct {
			Errno   int `json:"errno"`
			Records []struct {
				Uk int64  `json:"uk"`
				Sk string `json:"sk"`
			} `json:"records"`
		}
		if json.Unmarshal(body, &resp) == nil && len(resp.Records) > 0 {
			uk = resp.Records[0].Uk
			sk = resp.Records[0].Sk
		}
	}

	// 2. 如果不完整，从 /api/gettemplatevariable 获取
	if uk == 0 || sk == "" {
		fallbackURL := c.baseURL + `/api/gettemplatevariable?fields=["bdstoken","uk","sk"]`
		body2, err2 := c.BaiduGet(fallbackURL, "")
		if err2 == nil {
			var resp2 struct {
				Result struct {
					Uk int64  `json:"uk"`
					Sk string `json:"sk"`
				} `json:"result"`
			}
			if json.Unmarshal(body2, &resp2) == nil {
				if uk == 0 {
					uk = resp2.Result.Uk
				}
				if sk == "" {
					sk = resp2.Result.Sk
				}
			}
		}
	}

	// 3. 如果还是没有 sk，从 /youth/api/report/user 获取
	if sk == "" && uk != 0 {
		skURL := fmt.Sprintf(
			c.baseURL+"/youth/api/report/user?app_id=%s&clienttype=0&web=1&action=sapi_auth&timestamp=%d",
			url.QueryEscape(c.cfg.BaiduAppID),
			time.Now().UnixMilli(),
		)
		bodySK, errSK := c.BaiduGet(skURL, "")
		if errSK == nil {
			var respSK struct {
				Uinfo string `json:"uinfo"`
			}
			if json.Unmarshal(bodySK, &respSK) == nil && respSK.Uinfo != "" {
				sk = respSK.Uinfo
			}
		}
	}

	if uk == 0 || sk == "" {
		return 0, "", fmt.Errorf("无法从百度网盘获取完整的 Session uk=%d，sk 缺失或无效", uk)
	}

	return uk, sk, nil
}

// GetSession 线程安全地获取或刷新 uk/sk（含 TTL）
func (c *Client) GetSession() (int64, string, error) {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	ttl := c.cfg.SessionTTL()
	if c.uk != 0 && c.sk != "" && !c.sessionAt.IsZero() && time.Since(c.sessionAt) < ttl {
		return c.uk, c.sk, nil
	}
	uk, sk, err := c.fetchUserSession()
	if err != nil {
		return 0, "", err
	}
	c.uk = uk
	c.sk = sk
	c.sessionAt = time.Now()
	log.Printf("[Session] 获取成功, uk: %d", uk)
	return uk, sk, nil
}

// ClearSession 清除会话缓存
func (c *Client) ClearSession() {
	c.sessionMu.Lock()
	c.sk = ""
	c.uk = 0
	c.sessionAt = time.Time{}
	c.sessionMu.Unlock()
}

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

// EnsureFileMeta 确保路径对应的文件元数据在缓存中（非目录）。
func (c *Client) EnsureFileMeta(filePath string) (storage.FileMeta, error) {
	meta, ok := c.cache.GetFileMeta(filePath)
	if !ok {
		parentDir := path.Dir(filePath)
		log.Printf("[缓存] 未命中 %q，重新拉取父目录 %q 的文件列表", filePath, parentDir)
		result, err := c.FetchFileList(parentDir)
		if err != nil {
			return storage.FileMeta{}, fmt.Errorf("重新拉取文件列表失败: %w", err)
		}
		c.cache.Update(result.Body)
		meta, ok = c.cache.GetFileMeta(filePath)
	}
	if !ok {
		return storage.FileMeta{}, fmt.Errorf("找不到文件: %s", filePath)
	}
	if meta.IsDir == 1 {
		return storage.FileMeta{}, fmt.Errorf("路径是目录，不能下载: %s", filePath)
	}
	return meta, nil
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

// BaiduGet 向百度网盘API发起GET请求，Cookie由服务端注入
func (c *Client) BaiduGet(apiURL string, ua string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	c.setBaiduHeaders(req, ua)
	return c.readAPIResponse(req, c.httpClient)
}

// BaiduPostForm 向百度网盘API发起 form 编码 POST 请求（上传链路用），Cookie由服务端注入
func (c *Client) BaiduPostForm(apiURL string, form url.Values, ua string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodPost, apiURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c.setBaiduHeaders(req, ua)
	return c.readAPIResponse(req, c.httpClient)
}

// BaiduPostQuery 发起参数全部位于 query string、body 为空的 POST 请求
// （青春版上传接口的约定形态），Cookie 由服务端注入。
func (c *Client) BaiduPostQuery(apiURL string, ua string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodPost, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	c.setBaiduHeaders(req, ua)
	return c.readAPIResponse(req, c.httpClient)
}

func (c *Client) setBaiduHeaders(req *http.Request, ua string) {
	// Cookie只在服务端注入，绝不返回给客户端
	req.Header.Set("Cookie", c.cfg.BaiduCookie)
	req.Header.Set("User-Agent", NormalizeDownloadUA(ua))
	req.Header.Set("Referer", "https://pan.baidu.com/")
}

func (c *Client) readAPIResponse(req *http.Request, client *http.Client) ([]byte, error) {
	if client == nil {
		client = &http.Client{Timeout: BaiduClientTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求百度网盘API失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("百度网盘API返回非200状态: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	return body, nil
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

// ProbeBaiduSession 探测 Cookie/Session 是否可用（用于 readiness）。
func (c *Client) ProbeBaiduSession() error {
	_, _, err := c.GetSession()
	return err
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

// NormalizeDownloadUA 规范化下载 UA
func NormalizeDownloadUA(ua string) string {
	ua = strings.TrimSpace(ua)
	if ua == "" {
		return DefaultBaiduUA
	}
	return ua
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
