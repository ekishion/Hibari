package security

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMaskIP(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"192.168.1.100", "192.168.1.0/24"},
		{"10.0.8.42:8080", "10.0.8.0/24"},
		{"127.0.0.1", "127.0.0.0/24"},
		{"2001:0db8:85a3:0000:0000:8a2e:0370:7334", "2001:db8:85a3::/48"},
		{"[2001:db8:abcd:0012::1]:443", "2001:db8:abcd::/48"},
		{"", "unknown"},
		{"not-an-ip", "unknown"},
	}

	for _, tt := range tests {
		got := MaskIP(tt.input)
		if got != tt.expected {
			t.Errorf("MaskIP(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestAuditLog_WriteAndRotate(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "audit.log")

	logger := NewAuditLogger(logPath, 200) // 设小以便测试轮转
	defer logger.Close()

	// 写入多条记录触发滚动
	for i := 0; i < 10; i++ {
		logger.Log("test_event", "/path/to/resource", "192.168.1.100", "Mozilla/5.0")
	}

	// 确认主日志文件存在
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("主审计日志文件不存在: %v", err)
	}

	// 确认 .old 轮转文件存在
	oldPath := logPath + ".old"
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("轮转审计日志文件不存在: %v", err)
	}
}
