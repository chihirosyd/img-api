package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"img-api/internal/config"
)

func TestIsAllowed(t *testing.T) {
	whitelist := []string{"mysite.com", "blog.mysite.com"}

	cases := []struct {
		referer string
		want    bool
	}{
		{"https://mysite.com/page", true},
		{"https://www.mysite.com/", true},          // 子域名匹配
		{"https://evilmysite.com/", false},          // 前缀拼接不能绕过
		{"https://mysite.com.evil.com/", false},     // 后缀拼接不能绕过
		{"https://blog.mysite.com/", true},          // 精确白名单项
		{"https://other.com/", false},
		{"http://mysite.com:8080/x", true},          // 端口不影响
		{"not-a-valid-url", false},
		{"MYSITE.COM", true},                        // 大小写不敏感
	}

	for _, c := range cases {
		if got := isAllowed(c.referer, whitelist); got != c.want {
			t.Errorf("isAllowed(%q) = %v, want %v", c.referer, got, c.want)
		}
	}
}

// TestRefererExemptsHomeAndHealth 回归 Z8：
// 防盗链只保护图片接口，/ 首页与 /health 应豁免（外链访问首页不应 403）。
func TestRefererExemptsHomeAndHealth(t *testing.T) {
	old := config.C
	config.C = &config.AppConfig{RefererWhitelist: []string{"mysite.com"}}
	defer func() { config.C = old }()

	mw := Referer()
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	cases := []struct {
		path string
		want int
	}{
		{"/", http.StatusNoContent},        // 首页豁免
		{"/health", http.StatusNoContent},  // 健康检查豁免
		{"/random", http.StatusForbidden},  // 图片接口仍拦截
		{"/other", http.StatusForbidden},   // 其它路径仍拦截
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", c.path, nil)
		r.Header.Set("Referer", "https://evil.com/")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != c.want {
			t.Errorf("%s with foreign referer: status = %d, want %d", c.path, w.Code, c.want)
		}
	}
}

// TestRefererProtectsImageBranchOfHome 回归 R5-08："/" 在浏览器 <img> 嵌入
// （Accept 以 image/ 开头）或带 mode 参数时会直接出图，这类请求不能因为
// 路径是 "/" 就豁免防盗链——否则盗链方把 /random 换成 / 即可绕过白名单。
func TestRefererProtectsImageBranchOfHome(t *testing.T) {
	old := config.C
	config.C = &config.AppConfig{RefererWhitelist: []string{"mysite.com"}}
	defer func() { config.C = old }()

	mw := Referer()
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	cases := []struct {
		name    string
		path    string
		accept  string
		referer string
		want    int
	}{
		// 首页（浏览器导航）：Accept 以 text/html 开头 → 仍豁免
		{"homepage with foreign referer", "/", "text/html,application/xhtml+xml", "https://evil.com/", http.StatusNoContent},
		// <img> 嵌入根路径：Accept 以 image/ 开头 → 必须走白名单
		{"img embed of root", "/", "image/avif,image/webp,*/*", "https://evil.com/", http.StatusForbidden},
		// 带 mode 参数的根路径 → 出图，必须走白名单
		{"root with mode=image", "/?mode=image", "", "https://evil.com/", http.StatusForbidden},
		// 白名单内的站点嵌入根路径 → 放行
		{"img embed from whitelisted site", "/", "image/png", "https://mysite.com/page", http.StatusNoContent},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", c.path, nil)
			if c.accept != "" {
				r.Header.Set("Accept", c.accept)
			}
			r.Header.Set("Referer", c.referer)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != c.want {
				t.Errorf("status = %d, want %d", w.Code, c.want)
			}
		})
	}
}
