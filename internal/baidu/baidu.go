package baidu

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
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

// ProbeBaiduSession 探测 Cookie/Session 是否可用（用于 readiness）。
func (c *Client) ProbeBaiduSession() error {
	_, _, err := c.GetSession()
	return err
}

// NormalizeDownloadUA 规范化下载 UA
func NormalizeDownloadUA(ua string) string {
	ua = strings.TrimSpace(ua)
	if ua == "" {
		return DefaultBaiduUA
	}
	return ua
}
