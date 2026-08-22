package netx

import (
	"net"
	"testing"
)

// TestIsBlockedIP 覆盖 SSRF 防护的拦截与放行范围（安全关键函数）。
func TestIsBlockedIP(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		// 拦截：回环
		{"127.0.0.1", true},
		{"127.1.2.3", true},
		{"::1", true},
		// 拦截：内网 RFC1918
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"192.168.1.1", true},
		// 拦截：链路本地 / 组播 / 未指定
		{"169.254.169.254", true},
		{"224.0.0.1", true},
		{"0.0.0.0", true},
		{"::", true},
		// 拦截：0.0.0.0/8 与 CGNAT（100.64.0.0/10）
		{"0.1.2.3", true},
		{"100.64.0.1", true},
		// 拦截：IPv6 ULA（fc00::/7）
		{"fc00::1", true},
		{"fd12:3456::1", true},
		// 放行：公网地址
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"114.114.114.114", false},
		{"2606:4700:4700::1111", false}, // Cloudflare
	}

	for _, c := range cases {
		ip := net.ParseIP(c.ip)
		if ip == nil {
			t.Fatalf("invalid test IP %q", c.ip)
		}
		if got := IsBlockedIP(ip); got != c.want {
			t.Errorf("IsBlockedIP(%s) = %v, want %v", c.ip, got, c.want)
		}
	}
}
