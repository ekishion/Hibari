package config

import (
	"log"
	"os"
	"strconv"
	"strings"
)

// applyEnvOverrides 允许 Docker/编排用环境变量覆盖配置中的敏感项与监听参数。
// 环境变量优先于 config.json 中的同名字段。
func applyEnvOverrides(c *Config) {
	if n, ok := envInt("PORT"); ok {
		c.Port = n
	}
	if v := strings.TrimSpace(os.Getenv("BIND_ADDRESS")); v != "" {
		c.BindAddress = v
	}
	if v := strings.TrimSpace(os.Getenv("BAIDU_COOKIE")); v != "" {
		c.BaiduCookie = v
	}
	if v := strings.TrimSpace(os.Getenv("ACCESS_TOKEN")); v != "" {
		c.AccessToken = v
	}
	if v := strings.TrimSpace(os.Getenv("SESSION_SECRET")); v != "" {
		c.SessionSecret = v
	}
	if n, ok := envInt("AUTH_SESSION_TTL_SECONDS"); ok {
		c.AuthSessionTTLSeconds = n
	}
	if v := strings.TrimSpace(os.Getenv("BAIDU_APP_ID")); v != "" {
		c.BaiduAppID = v
	}
	if v := strings.TrimSpace(os.Getenv("FORCE_SECURE_COOKIE")); v != "" {
		c.ForceSecureCookie = parseBoolEnv(v)
	}
	if n, ok := envInt("RATE_LIMIT_PER_SECOND"); ok {
		c.RateLimitPerSecond = n
	}
	if n, ok := envInt("SHORT_LINK_TTL_SECONDS"); ok {
		c.ShortLinkTTLSeconds = n
	}
	if n, ok := envInt("SHORT_LINK_MAX_USES"); ok {
		c.ShortLinkMaxUses = n
	}
	if n, ok := envInt("DIR_LINK_TTL_SECONDS"); ok {
		c.DirLinkTTLSeconds = n
	}
	if n, ok := envInt("SHARE_PASSWORD_LENGTH"); ok {
		c.SharePasswordLength = n
	}
	if v := strings.TrimSpace(os.Getenv("STATE_BACKEND")); v != "" {
		c.StateBackend = v
	}
	if v := strings.TrimSpace(os.Getenv("STATE_PATH")); v != "" {
		c.StatePath = v
	}
	if n, ok := envInt("SESSION_TTL_SECONDS"); ok {
		c.SessionTTLSeconds = n
	}
	if v := strings.TrimSpace(os.Getenv("TRUSTED_PROXY_IPS")); v != "" {
		c.TrustedProxyIPs = strings.Split(v, ",")
	}
	if v := strings.TrimSpace(os.Getenv("AUDIT_LOG_PATH")); v != "" {
		c.AuditLogPath = v
	}
	if v := strings.TrimSpace(os.Getenv("ALLOWED_PATHS")); v != "" {
		c.AllowedPaths = strings.Split(v, ",")
	}
	if v := strings.TrimSpace(os.Getenv("SHOW_README")); v != "" {
		value := parseBoolEnv(v)
		c.ShowReadme = &value
	}
	if v := strings.TrimSpace(os.Getenv("SHOW_README_OVERVIEW")); v != "" {
		value := parseBoolEnv(v)
		c.ShowReadmeOverview = &value
	}
	if n, ok := envInt("LIST_MAX_PAGES"); ok {
		c.ListMaxPages = n
	}
	if n, ok := envInt("PREVIEW_MAX_MB"); ok {
		c.PreviewMaxMB = n
	}
	if n, ok := envInt("README_MAX_MB"); ok {
		c.ReadmeMaxMB = n
	}
	if n, ok := envInt("FILE_CACHE_TTL_SECONDS"); ok {
		c.FileCacheTTLSeconds = n
	}
	if n, ok := envInt("DLINK_CACHE_TTL_SECONDS"); ok {
		c.DlinkCacheTTLSeconds = n
	}
	if v := strings.TrimSpace(os.Getenv("UPLOAD_ENABLED")); v != "" {
		c.UploadEnabled = parseBoolEnv(v)
	}
	if n, ok := envInt("UPLOAD_MAX_MB"); ok {
		c.UploadMaxMB = n
	}
	if n, ok := envInt("UPLOAD_CHUNK_MB"); ok {
		c.UploadChunkMB = n
	}
	if v := strings.TrimSpace(os.Getenv("BAIDU_BASE_URL")); v != "" {
		c.BaiduBaseURL = v
	}
	if v := strings.TrimSpace(os.Getenv("BAIDU_PCS_BASE_URL")); v != "" {
		c.BaiduPCSBaseURL = v
	}
}

func envInt(key string) (int, bool) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		log.Printf("[WARN] 环境变量 %s 不是有效整数: %q，已忽略", key, v)
		return 0, false
	}
	return n, true
}

func parseBoolEnv(v string) bool {
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}
