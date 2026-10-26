// Package config 负责服务配置的装载与校验。它不 import 任何其它 internal 包。
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

// Backend 存储后端类型。
const (
	BackendLocal = "local"
	BackendBaidu = "baidu"
)

// Config 是服务的完整配置。所有字段都有显式默认值，由 Normalize 补齐。
type Config struct {
	// 监听
	Addr string `json:"addr"`

	// 存储后端
	Backend   string `json:"backend"`
	LocalRoot string `json:"local_root"`

	// 百度后端专用
	BaiduCookie  string `json:"baidu_cookie"`
	BaiduAppID   string `json:"baidu_app_id"`
	BaiduBaseURL string `json:"baidu_base_url"`

	// 数据
	DBPath string `json:"db_path"`

	// 行为
	AllowedPaths       []string `json:"allowed_paths"`
	RateLimitPerSecond int      `json:"rate_limit_per_second"`
	TrustedProxies     []string `json:"trusted_proxies"`
	SecureCookie       bool     `json:"secure_cookie"`

	// 时长与上限
	SessionTTL   int `json:"session_ttl_seconds"`
	ShareTTL     int `json:"share_ttl_seconds"`
	UploadMaxMB  int `json:"upload_max_mb"`
	ShareMaxUses int `json:"share_max_uses"`
}

// Load 从文件读取配置。文件不存在时返回空配置（走安装向导），
// 存在但格式错误时报错。
func Load(file string) (*Config, error) {
	var c Config
	data, err := os.ReadFile(file)
	if err != nil {
		if os.IsNotExist(err) {
			return &c, nil
		}
		return nil, fmt.Errorf("读取配置文件: %w", err)
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("解析配置文件: %w", err)
	}
	return &c, nil
}

// ApplyEnv 用环境变量覆盖配置。环境变量优先级高于文件。
// 数值解析失败必须报错：静默退回默认值会让运维以为配置生效了。
func (c *Config) ApplyEnv() error {
	set := func(key string, dst *string) {
		if v, ok := os.LookupEnv(key); ok {
			*dst = strings.TrimSpace(v)
		}
	}
	setInt := func(key string, dst *int) error {
		v, ok := os.LookupEnv(key)
		if !ok {
			return nil
		}
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return fmt.Errorf("环境变量 %s 不是整数: %q", key, v)
		}
		*dst = n
		return nil
	}
	setBool := func(key string, dst *bool) error {
		v, ok := os.LookupEnv(key)
		if !ok {
			return nil
		}
		b, err := strconv.ParseBool(strings.TrimSpace(v))
		if err != nil {
			return fmt.Errorf("环境变量 %s 不是布尔值: %q", key, v)
		}
		*dst = b
		return nil
	}

	set("ADDR", &c.Addr)
	set("BACKEND", &c.Backend)
	set("LOCAL_ROOT", &c.LocalRoot)
	set("BAIDU_COOKIE", &c.BaiduCookie)
	set("BAIDU_APP_ID", &c.BaiduAppID)
	set("BAIDU_BASE_URL", &c.BaiduBaseURL)
	set("DB_PATH", &c.DBPath)

	for _, err := range []error{
		setInt("RATE_LIMIT_PER_SECOND", &c.RateLimitPerSecond),
		setInt("SESSION_TTL_SECONDS", &c.SessionTTL),
		setInt("SHARE_TTL_SECONDS", &c.ShareTTL),
		setInt("UPLOAD_MAX_MB", &c.UploadMaxMB),
		setInt("SHARE_MAX_USES", &c.ShareMaxUses),
		setBool("SECURE_COOKIE", &c.SecureCookie),
	} {
		if err != nil {
			return err
		}
	}
	return nil
}

// Normalize 补齐默认值并校验。返回的错误都是启动期的配置错误，应当直接终止进程。
func (c *Config) Normalize() error {
	if c.Addr == "" {
		c.Addr = "127.0.0.1:4172"
	}
	if c.Backend == "" {
		c.Backend = BackendLocal
	}
	switch c.Backend {
	case BackendLocal:
		if c.LocalRoot == "" {
			c.LocalRoot = "data/files"
		}
		c.LocalRoot = path.Clean(strings.ReplaceAll(c.LocalRoot, "\\", "/"))
	case BackendBaidu:
		if c.BaiduAppID == "" {
			c.BaiduAppID = "250528"
		}
	default:
		return fmt.Errorf("backend 必须是 %q 或 %q，当前为 %q", BackendLocal, BackendBaidu, c.Backend)
	}
	if c.DBPath == "" {
		c.DBPath = "data/hibari.db"
	}

	c.RateLimitPerSecond = orDefault(c.RateLimitPerSecond, 10)
	c.SessionTTL = orDefault(c.SessionTTL, 7*24*3600)
	c.ShareTTL = orDefault(c.ShareTTL, 24*3600)
	c.UploadMaxMB = orDefault(c.UploadMaxMB, 1024)
	c.ShareMaxUses = orDefault(c.ShareMaxUses, 0)

	if c.RateLimitPerSecond < 1 {
		return fmt.Errorf("rate_limit_per_second 必须为正数")
	}

	cleaned := make([]string, 0, len(c.AllowedPaths))
	seen := make(map[string]bool, len(c.AllowedPaths))
	for _, raw := range c.AllowedPaths {
		p := strings.TrimSpace(raw)
		if p == "" {
			continue
		}
		if !IsValidPath(p) {
			return fmt.Errorf("allowed_paths 含非法路径: %q", raw)
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		cleaned = append(cleaned, p)
	}
	c.AllowedPaths = cleaned

	return nil
}

// IsValidPath 判断是否是以 / 开头的规范化绝对路径，且不含 . 与 .. 段。
func IsValidPath(p string) bool {
	if !strings.HasPrefix(p, "/") {
		return false
	}
	if strings.ContainsAny(p, "\x00\n\r\t") {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "." || seg == ".." {
			return false
		}
	}
	return path.Clean(p) == p
}

func (c *Config) SessionTTLDuration() time.Duration {
	return time.Duration(c.SessionTTL) * time.Second
}

func (c *Config) ShareTTLDuration() time.Duration {
	return time.Duration(c.ShareTTL) * time.Second
}

func (c *Config) UploadMaxBytes() int64 {
	return int64(c.UploadMaxMB) << 20
}

// PathAllowed 判断路径是否在白名单内。空白名单表示不限制。
// 根目录始终放行：它是挂载容器，本身不含内容，列出时由调用方逐项过滤。
func (c *Config) PathAllowed(p string) bool {
	if len(c.AllowedPaths) == 0 || p == "/" {
		return true
	}
	for _, root := range c.AllowedPaths {
		if p == root || strings.HasPrefix(p, strings.TrimSuffix(root, "/")+"/") {
			return true
		}
	}
	return false
}

func orDefault(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}
