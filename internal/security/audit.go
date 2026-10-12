package security

import (
	"encoding/json"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DefaultAuditMaxBytes 审计日志滚动阈值：超过后轮转为 *.old，磁盘占用有界。
const DefaultAuditMaxBytes = 10 * 1024 * 1024

// MaskIP 对客户端 IP 进行脱敏处理，防止明文存储用户真实 IP。
// IPv4 掩码后八位（/24），IPv6 保留前 48 位（/48）。
func MaskIP(rawIP string) string {
	rawIP = strings.TrimSpace(rawIP)
	if rawIP == "" {
		return "unknown"
	}
	if host, _, err := net.SplitHostPort(rawIP); err == nil {
		rawIP = host
	}
	ip := net.ParseIP(rawIP)
	if ip == nil {
		return "unknown"
	}
	if ip4 := ip.To4(); ip4 != nil {
		masked := ip4.Mask(net.CIDRMask(24, 32))
		return masked.String() + "/24"
	}
	masked := ip.Mask(net.CIDRMask(48, 128))
	return masked.String() + "/48"
}

// AuditLogger 负责安全审计日志的格式化、写入与有界滚动。
type AuditLogger struct {
	mu       sync.Mutex
	logPath  string
	maxBytes int64
	file     *os.File
	size     int64
}

func NewAuditLogger(logPath string, maxBytes int64) *AuditLogger {
	if maxBytes <= 0 {
		maxBytes = DefaultAuditMaxBytes
	}
	return &AuditLogger{
		logPath:  logPath,
		maxBytes: maxBytes,
	}
}

// Log 写入一条审计日志记录（自动脱敏并换行）。
func (a *AuditLogger) Log(event, resourcePath, rawClientIP, userAgent string) {
	if a == nil || a.logPath == "" {
		return
	}
	clientIP := MaskIP(rawClientIP)
	record := map[string]any{
		"at":         time.Now().UTC().Format(time.RFC3339),
		"event":      event,
		"path":       resourcePath,
		"ip":         clientIP,
		"user_agent": userAgent,
	}
	data, err := json.Marshal(record)
	if err != nil {
		return
	}
	data = append(data, '\n')

	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.writeLocked(data); err != nil {
		log.Printf("[audit] write failed: %v", err)
	}
}

func (a *AuditLogger) writeLocked(data []byte) error {
	if a.file == nil {
		if err := a.openFileLocked(); err != nil {
			return err
		}
	}
	if a.size+int64(len(data)) > a.maxBytes {
		a.rotateFileLocked()
	}
	n, err := a.file.Write(data)
	a.size += int64(n)
	return err
}

func (a *AuditLogger) openFileLocked() error {
	if dir := filepath.Dir(a.logPath); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(a.logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	a.file = f
	a.size = info.Size()
	return nil
}

func (a *AuditLogger) rotateFileLocked() {
	if a.file != nil {
		_ = a.file.Close()
		a.file = nil
	}
	if err := os.Rename(a.logPath, a.logPath+".old"); err != nil && !os.IsNotExist(err) {
		log.Printf("[audit] rotate failed: %v", err)
	}
	if err := a.openFileLocked(); err != nil {
		log.Printf("[audit] reopen after rotate failed: %v", err)
	}
}

// Close 关闭当前持有的审计文件句柄。
func (a *AuditLogger) Close() error {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.file != nil {
		err := a.file.Close()
		a.file = nil
		return err
	}
	return nil
}
