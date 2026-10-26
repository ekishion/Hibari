package httpapi

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mustNets 把测试用的 CIDR 字符串解析成网段。
func mustNets(t *testing.T, cidrs ...string) []*net.IPNet {
	t.Helper()
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			t.Fatalf("解析网段 %q: %v", c, err)
		}
		out = append(out, n)
	}
	return out
}

func TestClientIP(t *testing.T) {
	tests := []struct {
		name    string
		proxies []string
		remote  string
		headers map[string]string
		want    string
	}{
		{
			name:    "直连不可信时忽略转发头",
			remote:  "203.0.113.9:1234",
			headers: map[string]string{"X-Forwarded-For": "1.2.3.4", "X-Real-IP": "5.6.7.8"},
			want:    "203.0.113.9",
		},
		{
			name:    "可信代理取最右一跳",
			proxies: []string{"10.0.0.0/8"},
			remote:  "10.0.0.1:1234",
			headers: map[string]string{"X-Forwarded-For": "1.2.3.4, 6.7.8.9"},
			want:    "6.7.8.9",
		},
		{
			name:    "客户端伪造首元素被忽略",
			proxies: []string{"10.0.0.0/8"},
			remote:  "10.0.0.1:1234",
			headers: map[string]string{"X-Forwarded-For": "fake, 6.7.8.9"},
			want:    "6.7.8.9",
		},
		{
			name:    "链中出现非法值回落到直连地址",
			proxies: []string{"10.0.0.0/8"},
			remote:  "10.0.0.1:1234",
			headers: map[string]string{"X-Forwarded-For": "1.2.3.4, not-an-ip"},
			want:    "10.0.0.1",
		},
		{
			name:    "多层可信代理一路跳过",
			proxies: []string{"10.0.0.0/8", "192.168.0.0/16"},
			remote:  "10.0.0.1:1234",
			headers: map[string]string{"X-Forwarded-For": "1.2.3.4, 192.168.0.7"},
			want:    "1.2.3.4",
		},
		{
			name:    "整条链都是可信代理时取最左",
			proxies: []string{"10.0.0.0/8"},
			remote:  "10.0.0.1:1234",
			headers: map[string]string{"X-Forwarded-For": "10.0.0.5, 10.0.0.6"},
			want:    "10.0.0.5",
		},
		{
			name:    "无转发头时用 X-Real-IP",
			proxies: []string{"10.0.0.0/8"},
			remote:  "10.0.0.1:1234",
			headers: map[string]string{"X-Real-IP": "5.6.7.8"},
			want:    "5.6.7.8",
		},
		{
			name:    "无任何转发头时用直连地址",
			proxies: []string{"10.0.0.0/8"},
			remote:  "10.0.0.1:1234",
			want:    "10.0.0.1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{proxies: mustNets(t, tc.proxies...)}
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tc.remote
			for k, v := range tc.headers {
				r.Header.Set(k, v)
			}
			if got := s.clientIP(r); got != tc.want {
				t.Errorf("clientIP() = %q，期望 %q", got, tc.want)
			}
		})
	}
}
