// Command hibari 启动云雀服务。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ekishion/hibari/internal/backend/baidu"
	"github.com/ekishion/hibari/internal/backend/local"
	"github.com/ekishion/hibari/internal/config"
	"github.com/ekishion/hibari/internal/httpapi"
	"github.com/ekishion/hibari/internal/identity"
	"github.com/ekishion/hibari/internal/library"
	"github.com/ekishion/hibari/internal/sharing"
	"github.com/ekishion/hibari/internal/sqlite"
)

var version = "dev"

func main() {
	configPath := flag.String("config", "config.json", "配置文件路径")
	assetsDir := flag.String("assets", "", "前端构建产物目录（留空则使用内嵌资源）")
	flag.Parse()

	if err := run(*configPath, *assetsDir); err != nil {
		log.Fatalf("[FATAL] %v", err)
	}
}

func run(configPath, assetsDir string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	if err := cfg.ApplyEnv(); err != nil {
		return err
	}
	if err := cfg.Normalize(); err != nil {
		return fmt.Errorf("配置无效: %w", err)
	}

	db, err := sqlite.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("初始化数据库: %w", err)
	}
	defer db.Close()

	backend, closeBackend, err := newBackend(cfg)
	if err != nil {
		return err
	}
	defer closeBackend()

	identitySvc := identity.New(identity.NewSQLStore(db), cfg.SessionTTLDuration())
	sharingSvc := sharing.New(sharing.NewSQLStore(db))

	assets, err := resolveAssets(assetsDir)
	if err != nil {
		return err
	}

	srv := httpapi.New(httpapi.Deps{
		Config:      cfg,
		Backend:     backend,
		Identity:    identitySvc,
		Sharing:     sharingSvc,
		Auditor:     httpapi.NewSQLAuditor(db),
		Assets:      assets,
		Version:     version,
		TrustedNets: parseNets(cfg.TrustedProxies),
	})

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// ReadTimeout 覆盖整个请求体。上传走单独的读截止时间，
		// 所以这里只留够读普通 JSON 的余量。
		ReadTimeout: 60 * time.Second,
		// WriteTimeout 是「整个响应」的截止时间而非空闲超时，
		// 设了会把大文件下载拦腰切断，因此交给各 handler 自己控制。
		// shortcut: 只发请求不收响应的客户端不会被服务端断开，
		// 依赖反向代理的 proxy_read_timeout 兜底。
		WriteTimeout: 0,
		IdleTimeout:  90 * time.Second,
	}

	// 后台清理过期会话与分享。
	stopCleanup := startCleanup(db)

	errc := make(chan error, 1)
	go func() {
		log.Printf("云雀已启动: http://%s  后端: %s  版本: %s", cfg.Addr, cfg.Backend, version)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-errc:
		stopCleanup()
		return err
	case <-ctx.Done():
	}

	log.Printf("正在关闭…")
	stopCleanup()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return httpSrv.Shutdown(shutdownCtx)
}

// newBackend 是后端选择的唯一位置。新增适配器只需在此加一个分支。
// 返回的 close 在进程退出时释放后端持有的资源。
func newBackend(cfg *config.Config) (library.Backend, func(), error) {
	switch cfg.Backend {
	case config.BackendLocal:
		b, err := local.New(cfg.LocalRoot)
		if err != nil {
			return nil, nil, err
		}
		return b, func() { b.Close() }, nil
	case config.BackendBaidu:
		if cfg.BaiduCookie == "" {
			return nil, nil, fmt.Errorf("backend=baidu 需要配置 baidu_cookie")
		}
		b, err := baidu.New(baidu.Config{
			Cookie:  cfg.BaiduCookie,
			AppID:   cfg.BaiduAppID,
			BaseURL: cfg.BaiduBaseURL,
		})
		if err != nil {
			return nil, nil, err
		}
		return b, func() {}, nil
	default:
		return nil, nil, fmt.Errorf("未知后端: %s", cfg.Backend)
	}
}

// resolveAssets 返回前端资源。指定目录时读磁盘（开发期热更新），
// 否则使用编译期嵌入的产物。
func resolveAssets(dir string) (http.FileSystem, error) {
	if dir != "" {
		if _, err := os.Stat(filepath.Join(dir, "index.html")); err != nil {
			return nil, fmt.Errorf("资源目录 %s 缺少 index.html: %w", dir, err)
		}
		return httpapi.DirFS(dir), nil
	}
	return httpapi.EmbeddedAssets()
}

// parseNets 把 CIDR 或单个 IP 字符串解析为网段列表。
func parseNets(raw []string) []*net.IPNet {
	var out []*net.IPNet
	for _, item := range raw {
		item = strings.TrimSpace(item)
		if _, n, err := net.ParseCIDR(item); err == nil {
			out = append(out, n)
			continue
		}
		if ip := net.ParseIP(item); ip != nil {
			bits := 128
			if v4 := ip.To4(); v4 != nil {
				ip, bits = v4, 32
			}
			out = append(out, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
		}
	}
	return out
}

func startCleanup(db *sqlite.DB) func() {
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				if err := db.Cleanup(ctx, 30*24*time.Hour); err != nil {
					log.Printf("[WARN] 清理过期数据失败: %v", err)
				}
				cancel()
			}
		}
	}()
	return func() { close(stop) }
}
