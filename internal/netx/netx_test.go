package netx

import (
	"context"
	"net"
	"strings"
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
		{"233.252.0.1", true}, // 组播但非链路本地（224.0.0.0/4）
		{"ff05::2", true},     // IPv6 组播（ff00::/8）
		{"0.0.0.0", true},
		{"::", true},
		// 拦截：0.0.0.0/8 与 CGNAT（100.64.0.0/10）
		{"0.1.2.3", true},
		{"100.64.0.1", true},
		// 拦截：IPv6 ULA（fc00::/7）
		{"fc00::1", true},
		{"fd12:3456::1", true},
		// 拦截：保留/文档段（不可公网路由，防内网劫持利用）
		{"192.0.2.1", true},    // TEST-NET-1
		{"198.51.100.7", true}, // TEST-NET-2
		{"203.0.113.9", true},  // TEST-NET-3
		{"198.18.0.1", true},   // 基准测试段
		{"192.0.0.1", true},    // 协议保留
		{"192.88.99.1", true},  // 废弃 6to4 中继
		{"240.0.0.1", true},    // 240.0.0.0/4 保留段
		{"255.255.255.255", true},
		{"2001:db8::1", true}, // IPv6 文档段
		{"fec0::1", true},     // 废弃站点本地
		// 拦截：IPv6 转换段（回归 R5-10，此前全部漏网）
		{"64:ff9b::a9fe:a9fe", true}, // NAT64 → 169.254.169.254（云元数据）
		{"64:ff9b::a00:1", true},     // NAT64 → 10.0.0.1（内网）
		{"2002:a9fe:a9fe::1", true},  // 6to4 → 169.254.169.254
		{"2002:a00:1::1", true},      // 6to4 → 10.0.0.1
		{"2001::1", true},            // Teredo 整段拦截
		{"::a9fe:a9fe", true},        // v4-compatible → 169.254.169.254
		{"::8.8.8.8", true},          // v4-compatible 已废弃，整段拦截
		// 放行：转换到公网地址
		{"64:ff9b::808:808", false}, // NAT64 → 8.8.8.8
		{"2002:808:808::1", false},  // 6to4 → 8.8.8.8
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

// TestSafeDialContextRejectsLoopback 拨号层集成断言：内网目标在建立连接前被拒绝
// （此前只有 IsBlockedIP 的单元测试，拨号链路本身无覆盖）。
func TestSafeDialContextRejectsLoopback(t *testing.T) {
	if _, err := SafeDialContext(context.Background(), "tcp", "127.0.0.1:80"); err == nil {
		t.Fatal("SafeDialContext to 127.0.0.1 should be blocked")
	} else if !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("err = %v, want blocked private address", err)
	}
}

// TestSafeDialerRejectsLoopback 自定义拨号器（resolver 注入入口）同样拦截内网目标。
func TestSafeDialerRejectsLoopback(t *testing.T) {
	d := NewSafeDialer(nil)
	if _, err := d.DialContext(context.Background(), "tcp", "10.0.0.1:80"); err == nil {
		t.Fatal("DialContext to 10.0.0.1 should be blocked")
	}
}
