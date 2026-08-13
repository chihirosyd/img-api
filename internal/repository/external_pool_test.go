package repository

import "testing"

func TestExtractNested(t *testing.T) {
	data := map[string]any{
		"urls": map[string]any{
			"raw": "https://example.com/a.jpg",
		},
	}

	if got := extractNested(data, "urls.raw"); got != "https://example.com/a.jpg" {
		t.Errorf("extractNested(urls.raw) = %q", got)
	}
	if got := extractNested(data, "urls.missing"); got != "" {
		t.Errorf("extractNested(urls.missing) = %q, want empty", got)
	}
	if got := extractNested(data, "url"); got != "" {
		t.Errorf("extractNested(url) = %q, want empty (non-string)", got)
	}
	// 自动探测常见字段名
	if got := extractNested(data, ""); got != "https://example.com/a.jpg" {
		t.Errorf("extractNested(auto) = %q", got)
	}
}

func TestAPIMatchesCategory(t *testing.T) {
	p := &ExternalPool{}

	cases := []struct {
		categories []string
		category   string
		want       bool
	}{
		{nil, "anime", true},                         // 未声明 → 匹配所有
		{[]string{"all"}, "anime", true},             // all 通配
		{[]string{"nature", "anime"}, "anime", true}, // 精确匹配
		{[]string{"nature", "anime"}, "cat", false},  // 不在列表
		{[]string{"ANIME"}, "anime", true},           // 大小写不敏感
	}

	for _, c := range cases {
		got := p.apiMatchesCategory(ExternalAPIConfig{Categories: c.categories}, c.category)
		if got != c.want {
			t.Errorf("apiMatchesCategory(%v, %q) = %v, want %v", c.categories, c.category, got, c.want)
		}
	}
}
