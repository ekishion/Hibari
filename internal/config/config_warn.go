package config

import (
	"log"
	"net"
	"strings"
	"time"
)

const baiduClientTimeout = 15 * time.Second

// WriteTimeoutFor 按 list_max_pages 推导 WriteTimeout：最坏路径是
// 逐页翻拉满全部 pages，每页单次百度超时 baiduClientTimeout。
// 预留 5s 余量（处理+本地写出），下限 30s。
func WriteTimeoutFor(cfg *Config) time.Duration {
	pages := cfg.ListMaxPagesCount()
	worstCase := time.Duration(pages)*baiduClientTimeout + 5*time.Second
	if worstCase < 30*time.Second {
		return 30 * time.Second
	}
	return worstCase
}

// WarnRiskyConfig 启动时提示有风险但不阻断的配置组合。
func WarnRiskyConfig(cfg *Config) {
	if cfg.AuthEnabled() && len(cfg.AccessToken) < 16 {
		log.Printf("[WARN] access_token 长度不足 16 位，容易被穷举；建议用 openssl rand -hex 24 生成高熵令牌")
	}
	bindIP := net.ParseIP(cfg.BindAddress)
	bindLoopback := cfg.BindAddress == "localhost" || (bindIP != nil && bindIP.IsLoopback())
	if bindLoopback {
		return
	}
	for _, raw := range cfg.TrustedProxyIPs {
		raw = strings.TrimSpace(raw)
		loopback := false
		if ip := net.ParseIP(raw); ip != nil {
			loopback = ip.IsLoopback()
		} else if _, network, err := net.ParseCIDR(raw); err == nil {
			loopback = network.Contains(net.ParseIP("127.0.0.1")) || network.Contains(net.ParseIP("::1"))
		}
		if loopback {
			log.Printf("[WARN] trusted_proxy_ips 含环回地址 %q 且服务绑定在 %q：本机任意进程可伪造 X-Forwarded-For "+
				"获得新的限流桶并重置登录锁定。仅当反代确实经环回直连（非容器网络）时才安全；"+
				"容器部署请填写 docker 网关 CIDR 而非 127.0.0.1", raw, cfg.BindAddress)
			return
		}
	}
}
