package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeDefaults(t *testing.T) {
	c := &Config{}
	if err := c.Normalize(); err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if c.Addr != "127.0.0.1:4172" {
		t.Errorf("Addr = %q", c.Addr)
	}
	if c.Backend != BackendLocal {
		t.Errorf("Backend = %q", c.Backend)
	}
	if c.LocalRoot != "data/files" {
		t.Errorf("LocalRoot = %q", c.LocalRoot)
	}
	if c.DBPath != "data/hibari.db" {
		t.Errorf("DBPath = %q", c.DBPath)
	}
	if c.UploadMaxBytes() != 1024<<20 {
		t.Errorf("UploadMaxBytes = %d", c.UploadMaxBytes())
	}
}

func TestNormalizeRejectsUnknownBackend(t *testing.T) {
	c := &Config{Backend: "ftp"}
	if err := c.Normalize(); err == nil {
		t.Fatal("未知后端应报错")
	}
}

func TestNormalizeBaiduRequiresAppIDDefault(t *testing.T) {
	c := &Config{Backend: BackendBaidu}
	if err := c.Normalize(); err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if c.BaiduAppID != "250528" {
		t.Errorf("BaiduAppID = %q", c.BaiduAppID)
	}
}

func TestNormalizeCleansAllowedPaths(t *testing.T) {
	c := &Config{AllowedPaths: []string{" /a ", "", "/a", "/b"}}
	if err := c.Normalize(); err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if len(c.AllowedPaths) != 2 || c.AllowedPaths[0] != "/a" || c.AllowedPaths[1] != "/b" {
		t.Errorf("AllowedPaths = %v", c.AllowedPaths)
	}
}

func TestNormalizeRejectsBadAllowedPath(t *testing.T) {
	// 末尾斜杠也算不规范：要求写 path.Clean 之后的形式，避免启动后才暴露歧义。
	for _, bad := range []string{"a", "/a/../b", "/./a", "/b/"} {
		c := &Config{AllowedPaths: []string{bad}}
		if err := c.Normalize(); err == nil {
			t.Errorf("路径 %q 应被拒绝", bad)
		}
	}
}

func TestPathAllowed(t *testing.T) {
	cases := []struct {
		name     string
		allow    []string
		p        string
		expected bool
	}{
		{"空白名单不限制", nil, "/any/thing", true},
		{"根目录始终放行", []string{"/docs"}, "/", true},
		{"精确匹配", []string{"/docs"}, "/docs", true},
		{"子路径匹配", []string{"/docs"}, "/docs/a/b", true},
		{"前缀相近但不匹配", []string{"/docs"}, "/docs2", false},
		{"白名单外", []string{"/docs"}, "/other", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Config{AllowedPaths: tc.allow}
			if got := c.PathAllowed(tc.p); got != tc.expected {
				t.Errorf("PathAllowed(%q) = %v，期望 %v", tc.p, got, tc.expected)
			}
		})
	}
}

func TestIsValidPath(t *testing.T) {
	valid := []string{"/", "/a", "/a/b", "/a b"}
	for _, p := range valid {
		if !IsValidPath(p) {
			t.Errorf("%q 应合法", p)
		}
	}
	invalid := []string{"", "a", "/a/../b", "/a/./b", "/a/", "/a\x00b"}
	for _, p := range invalid {
		if IsValidPath(p) {
			t.Errorf("%q 应非法", p)
		}
	}
}

func TestLoadMissingFileReturnsEmpty(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Addr != "" {
		t.Errorf("缺失文件应返回空配置，Addr = %q", c.Addr)
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	file := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(file, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(file); err == nil {
		t.Fatal("格式错误的配置应报错")
	}
}

func TestLoadFromFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config.json")
	body := `{"addr":":9000","backend":"local","upload_max_mb":32}`
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(file)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Addr != ":9000" || c.UploadMaxMB != 32 {
		t.Errorf("配置未正确解析: %+v", c)
	}
}
