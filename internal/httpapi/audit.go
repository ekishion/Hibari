package httpapi

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/ekishion/hibari/internal/sqlite"
)

// SQLAuditor 把审计事件写入数据库，IP 做掩码后存储。
type SQLAuditor struct {
	db *sqlite.DB
}

func NewSQLAuditor(db *sqlite.DB) *SQLAuditor {
	return &SQLAuditor{db: db}
}

func (a *SQLAuditor) Record(ctx context.Context, event, path, ip, ua string) {
	if a == nil || a.db == nil {
		return
	}
	_, err := a.db.ExecContext(ctx,
		`INSERT INTO audit (event, path, client_ip, user_agent, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		event, path, MaskIP(ip), truncate(ua, 256),
		time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		log.Printf("[WARN] 写审计日志失败: %v", err)
	}
}

// AuditEvent 是审计记录的可读形式，IP 已掩码。
type AuditEvent struct {
	Event     string `json:"event"`
	Path      string `json:"path"`
	ClientIP  string `json:"client_ip"`
	UserAgent string `json:"user_agent"`
	CreatedAt string `json:"created_at"`
}

// Recent 返回最近 limit 条审计记录，新的在前。
func (a *SQLAuditor) Recent(ctx context.Context, limit int) ([]AuditEvent, error) {
	if a == nil || a.db == nil {
		return nil, nil
	}
	rows, err := a.db.QueryContext(ctx,
		`SELECT event, path, client_ip, user_agent, created_at
		 FROM audit ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]AuditEvent, 0, limit)
	for rows.Next() {
		var e AuditEvent
		if err := rows.Scan(&e.Event, &e.Path, &e.ClientIP, &e.UserAgent, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// MaskIP 把 IPv4 的末段、IPv6 的后 80 位替换为 x，保留可追溯性同时降低隐私风险。
func MaskIP(raw string) string {
	ip := net.ParseIP(strings.TrimSpace(raw))
	if ip == nil {
		return "unknown"
	}
	if v4 := ip.To4(); v4 != nil {
		return fmt.Sprintf("%d.%d.%d.x", v4[0], v4[1], v4[2])
	}
	return ip.Mask(net.CIDRMask(48, 128)).String()
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

var _ AuditLog = (*SQLAuditor)(nil)
