package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"img-api/internal/config"
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

// 来自可信网段的请求才信任 X-Forwarded-For：取最右侧（最接近真实客户端）
// 的不可信地址。
func TestRealClientIPTrustsConfiguredProxy(t *testing.T) {
	r := httptest.NewRequest("GET", "/random", nil)
	r.RemoteAddr = "10.0.0.5:12345" // 来自内网代理
	r.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.5")

	trusted := parseTrustedProxies([]string{"10.0.0.0/8"})
	if got := realClientIP(r, trusted); got != "203.0.113.9" {
		t.Fatalf("realClientIP = %q, want XFF first value", got)
	}
}

// 回归 Z7：XFF 里没有可用地址（垃圾/伪造值）时回退对端 IP，
// 防止攻击者轮换任意字符串制造无限限流桶。
func TestRealClientIPRejectsGarbageForwardedValue(t *testing.T) {
	r := httptest.NewRequest("GET", "/random", nil)
	r.RemoteAddr = "10.0.0.5:12345"
	r.Header.Set("X-Forwarded-For", "not-an-ip, 10.0.0.5")

	trusted := parseTrustedProxies([]string{"10.0.0.0/8"})
	if got := realClientIP(r, trusted); got != "10.0.0.5" {
		t.Fatalf("realClientIP = %q, want remote addr fallback", got)
	}
}

// 回归 R5-09：追加型反代（$proxy_add_x_forwarded_for）会把客户端自带的
// XFF 放在最左段。必须取"从右往左第一个不可信地址"，而不是最左段，
// 否则攻击者每次换一个伪造 IP 就能换一个限流桶。
func TestRealClientIPSkipsForgedLeftmostSegment(t *testing.T) {
	r := httptest.NewRequest("GET", "/random", nil)
	r.RemoteAddr = "10.0.0.5:12345" // 对端是可信代理
	// 攻击者伪造 6.6.6.6，反代把真实 IP 追加在后
	r.Header.Set("X-Forwarded-For", "6.6.6.6, 203.0.113.9, 10.0.0.5")

	trusted := parseTrustedProxies([]string{"10.0.0.0/8"})
	if got := realClientIP(r, trusted); got != "203.0.113.9" {
		t.Fatalf("realClientIP = %q, want 203.0.113.9（跳过伪造的最左段）", got)
	}
}

// 全部段都落在可信网段内 → 没有可用的客户端地址，回退对端 IP。
func TestRealClientIPAllSegmentsTrustedFallsBack(t *testing.T) {
	r := httptest.NewRequest("GET", "/random", nil)
	r.RemoteAddr = "10.0.0.5:12345"
	r.Header.Set("X-Forwarded-For", "10.0.0.7, 10.0.0.5")

	trusted := parseTrustedProxies([]string{"10.0.0.0/8"})
	if got := realClientIP(r, trusted); got != "10.0.0.5" {
		t.Fatalf("realClientIP = %q, want remote addr 10.0.0.5", got)
	}
}

// X-Real-IP 同样必须是合法 IP，否则回退对端地址。
func TestRealClientIPRejectsGarbageRealIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/random", nil)
	r.RemoteAddr = "10.0.0.5:12345"
	r.Header.Set("X-Real-IP", "garbage")

	trusted := parseTrustedProxies([]string{"10.0.0.0/8"})
	if got := realClientIP(r, trusted); got != "10.0.0.5" {
		t.Fatalf("realClientIP = %q, want remote addr fallback", got)
	}
}

// TestRateLimiterRetryAfterAndHealthExempt 回归 P3A-2：
// 429 响应带 Retry-After 头；/health 探针不占限流配额。
func TestRateLimiterRetryAfterAndHealthExempt(t *testing.T) {
	old := config.C
	config.C = &config.AppConfig{RateLimitEnabled: true, RateLimitMax: 2}
	defer func() { config.C = old }()

	mw, stop := RateLimiter()
	defer stop()
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	send := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		r.RemoteAddr = "198.51.100.7:12345"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	// /health 豁免：多次请求不占配额、不被 429
	for i := 0; i < 5; i++ {
		if w := send("/health"); w.Code != http.StatusNoContent {
			t.Fatalf("health request %d: status = %d, want exempt", i+1, w.Code)
		}
	}

	// 前 2 次放行，第 3 次 429 + Retry-After: 60
	if w := send("/random"); w.Code != http.StatusNoContent {
		t.Fatalf("req1 status = %d, want 204", w.Code)
	}
	if w := send("/random"); w.Code != http.StatusNoContent {
		t.Fatalf("req2 status = %d, want 204", w.Code)
	}
	w := send("/random")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("req3 status = %d, want 429", w.Code)
	}
	if ra := w.Header().Get("Retry-After"); ra != "60" {
		t.Fatalf("Retry-After = %q, want 60", ra)
	}
}

// 回归 R5-11：限流器在 CORS 外层，429 响应必须自带跨域头与安全头。
// 否则浏览器 fetch 只会报"CORS 错误"，读不到 429 与 Retry-After，前端无法
// 识别是限流并优雅退避。
func TestRateLimiter429CarriesCORSHeaders(t *testing.T) {
	old := config.C
	config.C = &config.AppConfig{RateLimitEnabled: true, RateLimitMax: 1, CorsEnabled: true}
	defer func() { config.C = old }()

	mw, stop := RateLimiter()
	defer stop()
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	send := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/random", nil)
		r.RemoteAddr = "198.51.100.8:12345"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	if w := send(); w.Code != http.StatusNoContent {
		t.Fatalf("req1 status = %d, want 204", w.Code)
	}
	w := send()
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("req2 status = %d, want 429", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want *", got)
	}
	if got := w.Header().Get("Retry-After"); got != "60" {
		t.Errorf("Retry-After = %q, want 60", got)
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
}
