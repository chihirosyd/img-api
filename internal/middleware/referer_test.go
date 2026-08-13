package middleware

import "testing"

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
