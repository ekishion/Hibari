package baidu

import (
	"strings"
	"testing"
)

func TestIsAllowedBaiduDownloadURL(t *testing.T) {
	tests := []struct {
		url     string
		allowed bool
	}{
		{"https://d.pcs.baidu.com/file/xyz", true},
		{"https://pan.baidu.com/share/download", true},
		{"https://dl.baidupcs.com/file/123", true},
		{"https://g.bdstatic.com/static/123", true},
		{"http://d.pcs.baidu.com/file/xyz", false}, // http not allowed
		{"https://evil.com/fake.baidu.com", false},
		{"https://notbaidu.com", false},
		{"invalid-url", false},
	}

	for _, tt := range tests {
		got := IsAllowedBaiduDownloadURL(tt.url)
		if got != tt.allowed {
			t.Errorf("IsAllowedBaiduDownloadURL(%q) = %v; want %v", tt.url, got, tt.allowed)
		}
	}
}

func TestNormalizeDownloadUA(t *testing.T) {
	if got := NormalizeDownloadUA(""); got != DefaultBaiduUA {
		t.Errorf("NormalizeDownloadUA(\"\") = %q; want %q", got, DefaultBaiduUA)
	}
	custom := "Mozilla/5.0 CustomAgent"
	if got := NormalizeDownloadUA("  " + custom + "  "); got != custom {
		t.Errorf("NormalizeDownloadUA with padding = %q; want %q", got, custom)
	}
}

func TestWithPrivateCacheControl(t *testing.T) {
	u1 := "https://example.com/file?foo=bar"
	got1 := WithPrivateCacheControl(u1)
	if !strings.Contains(got1, "&response-cache-control=private") {
		t.Errorf("expected &response-cache-control=private, got %s", got1)
	}

	u2 := "https://example.com/file"
	got2 := WithPrivateCacheControl(u2)
	if !strings.Contains(got2, "?response-cache-control=private") {
		t.Errorf("expected ?response-cache-control=private, got %s", got2)
	}

	// idempotent
	got3 := WithPrivateCacheControl(got2)
	if got3 != got2 {
		t.Errorf("expected idempotent result, got %s", got3)
	}
}

func TestStripDownloadLinks(t *testing.T) {
	raw := []byte(`{"errno":0,"list":[{"fs_id":1,"dlink":"https://secret.url","server_filename":"a.txt"}]}`)
	stripped, err := StripDownloadLinks(raw)
	if err != nil {
		t.Fatalf("StripDownloadLinks failed: %v", err)
	}
	if strings.Contains(string(stripped), "secret.url") {
		t.Errorf("expected dlink to be removed from output: %s", string(stripped))
	}
	if !strings.Contains(string(stripped), "a.txt") {
		t.Errorf("expected server_filename to be preserved: %s", string(stripped))
	}
}

func TestLocatedownloadRandAndSign(t *testing.T) {
	randVal := LocatedownloadRand(123456, "secretkey", 1700000000000)
	if len(randVal) != 40 { // sha1 hex is 40 chars
		t.Errorf("expected 40 chars hex for sha1, got %d (%s)", len(randVal), randVal)
	}

	signVal := LocatedownloadSign("d41d8cd98f00b204e9800998ecf8427e", "987654321", 123456, 1700000000000)
	if len(signVal) != 32 { // md5 hex is 32 chars
		t.Errorf("expected 32 chars hex for md5, got %d (%s)", len(signVal), signVal)
	}
}
