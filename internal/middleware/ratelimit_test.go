package middleware

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestSlidingWindowAllow(t *testing.T) {
	l := &slidingWindowLimiter{
		window:  time.Minute,
		maxReqs: 3,
		ips:     make(map[string]*timestamps),
	}

	for i := 0; i < 3; i++ {
		if !l.allow("1.2.3.4") {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	if l.allow("1.2.3.4") {
		t.Fatal("4th request within window should be blocked")
	}
}

// 未配置可信代理时，伪造的 X-Forwarded-For 必须被忽略，按对端地址计限流。
func TestRealClientIPIgnoresUntrustedForwardedHeaders(t *testing.T) {
	r := httptest.NewRequest("GET", "/random", nil)
	r.RemoteAddr = "203.0.113.7:54321"
	r.Header.Set("X-Forwarded-For", "6.6.6.6")
	r.Header.Set("X-Real-IP", "6.6.6.6")

	if got := realClientIP(r, nil); got != "203.0.113.7" {
		t.Fatalf("realClientIP = %q, want remote addr (forwarded headers must be ignored)", got)
	}
}

// 来自可信网段的请求才信任 X-Forwarded-For 第一段。
func TestRealClientIPTrustsConfiguredProxy(t *testing.T) {
	r := httptest.NewRequest("GET", "/random", nil)
	r.RemoteAddr = "10.0.0.5:12345" // 来自内网代理
	r.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.5")

	trusted := parseTrustedProxies([]string{"10.0.0.0/8"})
	if got := realClientIP(r, trusted); got != "203.0.113.9" {
		t.Fatalf("realClientIP = %q, want XFF first value", got)
	}
}
