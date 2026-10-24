package config

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"
	"time"
)

// validPathRe 只允许以 / 开头的合法路径，防止路径穿越
var validPathRe = regexp.MustCompile(`^/[^\x00-\x1f]*$`)

// IsValidBaiduPath 检查是否为合法规范的百度网盘路径
func IsValidBaiduPath(p string) bool {
	if !validPathRe.MatchString(p) {
		return false
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	cleaned := path.Clean(p)
	return cleaned == p && strings.HasPrefix(cleaned, "/")
}

// Config 服务配置
type Config struct {
	Port               int    `json:"port"`
	BindAddress        string `json:"bind_address"`
	BaiduCookie        string `json:"baidu_cookie"`
	RateLimitPerSecond int    `json:"rate_limit_per_second"`
	BaiduAppID         string `json:"baidu_app_id"`

	// AccessToken 非空时启用全站访问控制（API 与下载短链均需鉴权）。
	// 适合 VPS 给少数用户使用：共享一个长随机令牌即可。
	AccessToken string `json:"access_token"`

	// SessionSecret 用于给登录会话签名的密钥。为空时由 AccessToken 派生，
	// 因此零配置即可用；显式修改该值可一键吊销所有已登录会话。
	SessionSecret string `json:"session_secret"`

	// AuthSessionTTLSeconds 登录会话有效期，默认 7 天。会话令牌内嵌过期时间，
	// 客户端无法延长；配合 SessionSecret 轮换实现服务端强制下线。
	AuthSessionTTLSeconds int `json:"auth_session_ttl_seconds"`

	// ForceSecureCookie 在 TLS 终结于反向代理时强制 Secure Cookie。
	ForceSecureCookie bool `json:"force_secure_cookie"`

	// ShortLinkTTLSeconds 短链有效期，默认 3600。
	ShortLinkTTLSeconds int `json:"short_link_ttl_seconds"`
	// ShortLinkMaxUses 短链最大使用次数；0 表示不限制。
	ShortLinkMaxUses int `json:"short_link_max_uses"`

	// DirLinkTTLSeconds 目录短链有效期，默认 7 天。目录短链用于地址栏
	// 分享/书签（?d=令牌）。
	DirLinkTTLSeconds int `json:"dir_link_ttl_seconds"`

	// SharePasswordLength 提取码/访问码默认生成长度（位），默认 6 位。
	SharePasswordLength int `json:"share_password_length"`

	// StateBackend 状态持久化后端：none（默认，重启后短链全部失效）|
	// sqlite（state_path 为 .db 文件，纯 Go 驱动无需 CGO）。
	// 持久化范围：目录短链、下载短链与目录列表本地缓存（降低百度请求频率）。
	StateBackend string `json:"state_backend"`
	// StatePath 持久化文件路径；state_backend 非 none 时必填。
	StatePath string `json:"state_path"`

	// SessionTTLSeconds 百度 uk/sk 会话缓存有效期，默认 3600。
	SessionTTLSeconds  int      `json:"session_ttl_seconds"`
	TrustedProxyIPs    []string `json:"trusted_proxy_ips"`
	AuditLogPath       string   `json:"audit_log_path"`
	AllowedPaths       []string `json:"allowed_paths"`
	ShowReadme         *bool    `json:"show_readme"`
	ShowReadmeOverview *bool    `json:"show_readme_overview"`

	// ListMaxPages 目录列表自动翻页上限；0 表示默认 15 页（每页 100 项）。
	ListMaxPages int `json:"list_max_pages"`
	// PreviewMaxMB 图片预览大小上限（MB，整数）；0 表示默认 16 MB。
	PreviewMaxMB int `json:"preview_max_mb"`
	// ReadmeMaxMB README/文本预览大小上限（MB，整数）；0 表示默认 512 KB。
	ReadmeMaxMB int `json:"readme_max_mb"`
	// FileCacheTTLSeconds 文件元数据缓存有效期；0 表示默认 15 分钟。
	FileCacheTTLSeconds int `json:"file_cache_ttl_seconds"`
	// DlinkCacheTTLSeconds 下载直链缓存有效期；0 表示默认 5 分钟。
	DlinkCacheTTLSeconds int `json:"dlink_cache_ttl_seconds"`

	// UploadEnabled 是否允许通过本服务向百度网盘上传文件。默认关闭：
	// 上传消耗的是站长 cookie 账号的容量与流量，需显式开启；
	// 可配合 allowed_paths 限定可上传目录。
	UploadEnabled bool `json:"upload_enabled"`
	// UploadMaxMB 单文件上传上限（MB，整数）；0 表示不限。
	UploadMaxMB int `json:"upload_max_mb"`
	// UploadChunkMB 上传分片大小（MB，整数）；0 表示默认 4 MB，最大 16。
	UploadChunkMB int `json:"upload_chunk_mb"`

	// BaiduBaseURL / BaiduPCSBaseURL 仅测试用：替换 pan.baidu.com 与
	// d.pcs.baidu.com 的请求目标，正常部署留空。
	BaiduBaseURL    string `json:"baidu_base_url"`
	BaiduPCSBaseURL string `json:"baidu_pcs_base_url"`
}

func (c *Config) ShowReadmeEnabled() bool {
	return c.ShowReadme == nil || *c.ShowReadme
}

func (c *Config) ShowReadmeOverviewEnabled() bool {
	return c.ShowReadmeOverview == nil || *c.ShowReadmeOverview
}

func (c *Config) ShortLinkTTL() time.Duration {
	if c.ShortLinkTTLSeconds <= 0 {
		return time.Hour
	}
	return time.Duration(c.ShortLinkTTLSeconds) * time.Second
}

// DirLinkTTL 目录短链有效期，默认 7 天。
func (c *Config) DirLinkTTL() time.Duration {
	if c.DirLinkTTLSeconds <= 0 {
		return 7 * 24 * time.Hour
	}
	return time.Duration(c.DirLinkTTLSeconds) * time.Second
}

func (c *Config) GetSharePasswordLength() int {
	if c.SharePasswordLength < 2 || c.SharePasswordLength > 16 {
		return 6
	}
	return c.SharePasswordLength
}

func (c *Config) SessionTTL() time.Duration {
	if c.SessionTTLSeconds <= 0 {
		return time.Hour
	}
	return time.Duration(c.SessionTTLSeconds) * time.Second
}

// AuthSessionTTL 登录会话有效期，默认 7 天。
func (c *Config) AuthSessionTTL() time.Duration {
	if c.AuthSessionTTLSeconds <= 0 {
		return 7 * 24 * time.Hour
	}
	return time.Duration(c.AuthSessionTTLSeconds) * time.Second
}

// ListMaxPagesCount 目录列表自动翻页上限，默认 15 页（每页 100，即 1500 项）。
func (c *Config) ListMaxPagesCount() int {
	if c.ListMaxPages > 0 {
		return c.ListMaxPages
	}
	return 15
}

// PreviewMaxBytes 图片预览大小上限（由 MB 换算），默认 16 MB。
func (c *Config) PreviewMaxBytes() int {
	if c.PreviewMaxMB > 0 {
		return c.PreviewMaxMB * 1024 * 1024
	}
	return 16 * 1024 * 1024
}

// ReadmeMaxBytes README/文本预览大小上限（由 MB 换算），默认 512 KB。
func (c *Config) ReadmeMaxBytes() int {
	if c.ReadmeMaxMB > 0 {
		return c.ReadmeMaxMB * 1024 * 1024
	}
	return 512 * 1024
}

// FileCacheTTL 文件元数据缓存有效期，默认 15 分钟。
func (c *Config) FileCacheTTL() time.Duration {
	if c.FileCacheTTLSeconds > 0 {
		return time.Duration(c.FileCacheTTLSeconds) * time.Second
	}
	return 15 * time.Minute
}

// DlinkCacheTTL 下载直链缓存有效期，默认 5 分钟。
func (c *Config) DlinkCacheTTL() time.Duration {
	if c.DlinkCacheTTLSeconds > 0 {
		return time.Duration(c.DlinkCacheTTLSeconds) * time.Second
	}
	return 5 * time.Minute
}

// UploadMaxBytes 单文件上传上限（由 MB 换算）；0 表示不限。
func (c *Config) UploadMaxBytes() int64 {
	if c.UploadMaxMB > 0 {
		return int64(c.UploadMaxMB) * 1024 * 1024
	}
	return 0
}

// UploadChunkBytes 上传分片大小（由 MB 换算），默认 4 MB（百度网页流标准分片）。
func (c *Config) UploadChunkBytes() int {
	if c.UploadChunkMB > 0 {
		return c.UploadChunkMB * 1024 * 1024
	}
	return 4 * 1024 * 1024
}

// SessionSigningKey 派生会话签名密钥：优先用显式 SessionSecret，
// 否则由 AccessToken 派生（轮换 AccessToken 会自动失效旧会话）。
func (c *Config) SessionSigningKey() []byte {
	base := strings.TrimSpace(c.SessionSecret)
	if base == "" {
		base = c.AccessToken
	}
	mac := hmac.New(sha256.New, []byte(base))
	mac.Write([]byte("jikeqingpan/session/v1"))
	return mac.Sum(nil)
}

// AuthEnabled reports whether access token authentication is enabled.
func (c *Config) AuthEnabled() bool {
	return strings.TrimSpace(c.AccessToken) != ""
}

// LoadConfig 从指定路径加载并解析配置文件，并自动应用环境变量覆盖与规范化校验。
func LoadConfig(path string) (*Config, error) {
	var cfg Config
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("读取配置文件失败: %w", err)
		}
		// 文件不存在时允许纯环境变量启动（Docker/编排友好）。
	} else {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("解析配置文件失败: %w", err)
		}
	}
	applyEnvOverrides(&cfg)
	if err := cfg.normalizeAndValidate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) normalizeAndValidate() error {
	if c.Port == 0 {
		c.Port = 4172
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("port 必须在 1 到 65535 之间")
	}
	if strings.TrimSpace(c.BindAddress) == "" {
		c.BindAddress = "127.0.0.1"
	}
	if c.RateLimitPerSecond == 0 {
		c.RateLimitPerSecond = 10
	}
	if c.RateLimitPerSecond < 1 {
		return fmt.Errorf("rate_limit_per_second 必须大于 0")
	}
	if c.BaiduAppID == "" {
		c.BaiduAppID = "250528"
	}
	if strings.TrimSpace(c.BaiduCookie) == "" {
		return fmt.Errorf("baidu_cookie 不能为空（可写在配置文件，或通过环境变量 BAIDU_COOKIE 注入）")
	}
	if c.ShortLinkTTLSeconds < 0 {
		return fmt.Errorf("short_link_ttl_seconds 不能为负数")
	}
	if c.DirLinkTTLSeconds < 0 {
		return fmt.Errorf("dir_link_ttl_seconds 不能为负数")
	}
	if c.SharePasswordLength == 0 {
		c.SharePasswordLength = 6
	} else if c.SharePasswordLength < 2 || c.SharePasswordLength > 16 {
		return fmt.Errorf("share_password_length 必须在 2 到 16 之间")
	}
	c.StateBackend = strings.ToLower(strings.TrimSpace(c.StateBackend))
	if c.StateBackend == "" {
		c.StateBackend = "none"
	}
	switch c.StateBackend {
	case "none", "sqlite":
	default:
		return fmt.Errorf("state_backend 必须是 none 或 sqlite")
	}
	c.StatePath = strings.TrimSpace(c.StatePath)
	if c.StateBackend != "none" && c.StatePath == "" {
		return fmt.Errorf("state_backend=%s 时必须配置 state_path", c.StateBackend)
	}
	if c.ShortLinkMaxUses < 0 {
		return fmt.Errorf("short_link_max_uses 不能为负数")
	}
	if c.SessionTTLSeconds < 0 {
		return fmt.Errorf("session_ttl_seconds 不能为负数")
	}
	if c.AuthSessionTTLSeconds < 0 {
		return fmt.Errorf("auth_session_ttl_seconds 不能为负数")
	}
	if c.ListMaxPages != 0 && (c.ListMaxPages < 1 || c.ListMaxPages > 100) {
		return fmt.Errorf("list_max_pages 必须在 1 到 100 之间")
	}
	if c.PreviewMaxMB < 0 {
		return fmt.Errorf("preview_max_mb 不能为负数")
	}
	if c.ReadmeMaxMB < 0 {
		return fmt.Errorf("readme_max_mb 不能为负数")
	}
	if c.FileCacheTTLSeconds < 0 {
		return fmt.Errorf("file_cache_ttl_seconds 不能为负数")
	}
	if c.DlinkCacheTTLSeconds < 0 {
		return fmt.Errorf("dlink_cache_ttl_seconds 不能为负数")
	}
	if c.UploadMaxMB < 0 {
		return fmt.Errorf("upload_max_mb 不能为负数（0 表示不限）")
	}
	if c.UploadChunkMB < 0 || c.UploadChunkMB > 16 {
		return fmt.Errorf("upload_chunk_mb 必须在 0 到 16 之间（0 表示默认 4 MB）")
	}
	c.AccessToken = strings.TrimSpace(c.AccessToken)
	c.SessionSecret = strings.TrimSpace(c.SessionSecret)
	for i := range c.TrustedProxyIPs {
		c.TrustedProxyIPs[i] = strings.TrimSpace(c.TrustedProxyIPs[i])
	}
	seenAllowed := make(map[string]struct{}, len(c.AllowedPaths))
	normalizedAllowed := make([]string, 0, len(c.AllowedPaths))
	for _, raw := range c.AllowedPaths {
		allowed := strings.TrimSpace(raw)
		if allowed == "" || !IsValidBaiduPath(allowed) {
			return fmt.Errorf("allowed_paths contains an invalid path: %q", raw)
		}
		if _, exists := seenAllowed[allowed]; exists {
			continue
		}
		seenAllowed[allowed] = struct{}{}
		normalizedAllowed = append(normalizedAllowed, allowed)
	}
	c.AllowedPaths = normalizedAllowed
	return nil
}
