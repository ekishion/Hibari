package library

import (
	"errors"
	"testing"
)

func TestCleanPath(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", "/", false},
		{"   ", "/", false},
		{"/", "/", false},
		{"/a/b", "/a/b", false},
		{"a/b", "/a/b", false},
		{"/a//b", "/a/b", false},
		{"/a/b/", "/a/b", false},
		{`\a\b`, "/a/b", false},
		{"/../etc/passwd", "", true},
		{"/a/../../b", "", true},
		{"..", "", true},
		{".", "", true},
		{"/a/./b", "", true},
		{"/a\x00b", "", true},
		{"/a\nb", "", true},
	}
	for _, tc := range cases {
		got, err := CleanPath(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("CleanPath(%q) 应报错，得到 %q", tc.in, got)
			} else if !errors.Is(err, ErrInvalid) {
				t.Errorf("CleanPath(%q) 错误类型 = %v", tc.in, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("CleanPath(%q) 意外报错: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("CleanPath(%q) = %q，期望 %q", tc.in, got, tc.want)
		}
	}
}

func TestJoin(t *testing.T) {
	got, err := Join("/dir", "name.txt")
	if err != nil || got != "/dir/name.txt" {
		t.Fatalf("Join = %q, %v", got, err)
	}
	if got, err := Join("/", "a"); err != nil || got != "/a" {
		t.Fatalf("根目录下 Join = %q, %v", got, err)
	}

	// 文件名不允许任何分隔符或控制字符，否则可以逃逸出目标目录。
	for _, bad := range []string{"", "  ", ".", "..", "a/b", `a\b`, "a\x00b", "a\nb"} {
		if _, err := Join("/dir", bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("Join(%q) 应拒绝，得到 %v", bad, err)
		}
	}
}

func TestIsUnder(t *testing.T) {
	cases := []struct {
		p, root string
		want    bool
	}{
		{"/a/b", "/a", true},
		{"/a", "/a", true},
		{"/a/b/c", "/a", true},
		{"/ab", "/a", false},
		{"/b", "/a", false},
		{"/anything", "/", true},
		{"/", "/", true},
	}
	for _, tc := range cases {
		if got := IsUnder(tc.p, tc.root); got != tc.want {
			t.Errorf("IsUnder(%q, %q) = %v，期望 %v", tc.p, tc.root, got, tc.want)
		}
	}
}

func TestQuotaFree(t *testing.T) {
	cases := []struct {
		q    Quota
		want int64
	}{
		{Quota{Total: 0, Used: 100}, 0},   // 总量未知
		{Quota{Total: 100, Used: 40}, 60}, // 正常
		{Quota{Total: 100, Used: 100}, 0}, // 用满
		{Quota{Total: 100, Used: 200}, 0}, // 超额不返回负数
	}
	for _, tc := range cases {
		if got := tc.q.Free(); got != tc.want {
			t.Errorf("Quota%+v.Free() = %d，期望 %d", tc.q, got, tc.want)
		}
	}
}
