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

// TestPickDefaultCategory 验证 default_category 的行为：
// 未配置时返回空字符串（表示"不指定分类"，不向上游发送字面 default）。
func TestPickDefaultCategory(t *testing.T) {
	p := &ExternalPool{}
	if got := p.pickDefaultCategory(&ExternalAPIConfig{}); got != "" {
		t.Errorf("pickDefaultCategory(unconfigured) = %q, want empty", got)
	}
	if got := p.pickDefaultCategory(&ExternalAPIConfig{DefaultCategory: []string{"nature"}}); got != "nature" {
		t.Errorf("pickDefaultCategory(single) = %q, want nature", got)
	}
}

func TestSupportsCategory(t *testing.T) {
	pool := &ExternalPool{apis: []ExternalAPIConfig{
		{Name: "a", Categories: []string{"cat"}},
		{Name: "b", Categories: []string{"all"}},
		{Name: "c"}, // 未声明 categories → 匹配所有
	}}

	cases := []struct {
		category string
		want     bool
	}{
		{"", true},        // 空 → 不筛选
		{"default", true}, // default → 不筛选
		{"cat", true},     // a 精确匹配
		{"dog", true},     // b 的 all 通配 / c 未声明
		{"bird", true},    // b 的 all 通配匹配所有分类
	}

	for _, c := range cases {
		if got := pool.SupportsCategory(c.category); got != c.want {
			t.Errorf("SupportsCategory(%q) = %v, want %v", c.category, got, c.want)
		}
	}

	// 无通配/未声明 categories 的池：白名单外的分类返回 false
	strict := &ExternalPool{apis: []ExternalAPIConfig{
		{Name: "a", Categories: []string{"cat"}},
	}}
	if !strict.SupportsCategory("cat") {
		t.Error("strict pool should support cat")
	}
	if strict.SupportsCategory("bird") {
		t.Error("strict pool should not support bird")
	}
}

func TestAPISupportsCategory(t *testing.T) {
	pool := &ExternalPool{apis: []ExternalAPIConfig{
		{Name: "flickr", Categories: []string{"nature", "cat"}},
		{Name: "picsum"}, // 未声明 categories → 匹配所有
	}}

	cases := []struct {
		name     string
		category string
		want     bool
	}{
		{"flickr", "cat", true},
		{"flickr", "nature", true},
		{"flickr", "dog", false},
		{"flickr", "default", true}, // default → 不筛选
		{"flickr", "", true},
		{"picsum", "anything", true}, // 未声明 → 匹配所有
		{"missing", "cat", false},    // API 不存在
	}

	for _, c := range cases {
		if got := pool.APISupportsCategory(c.name, c.category); got != c.want {
			t.Errorf("APISupportsCategory(%q, %q) = %v, want %v", c.name, c.category, got, c.want)
		}
	}
}
