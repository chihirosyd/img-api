package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// TestImageConfigNormalizesKeysButKeepsHeaderCase 回归 D2 / P4-2：
// 键名归一化为小写，但 headers 子映射的键必须保持用户写法——
// HTTP 头名只在接收方比较时大小写不敏感，部分上游按精确大小写匹配，
// 一律转小写会让 X-Api-Key 变成 x-api-key 而被 401。
func TestImageConfigNormalizesKeysButKeepsHeaderCase(t *testing.T) {
	const doc = `
EXTERNAL_APIS:
  - NAME: unsplash
    URL: https://example.com/{width}
    RESPONSE_TYPE: json
    URL_FIELD: urls.raw
    HEADERS:
      X-Api-Key: secret
      User-Agent: custom-ua
      Authorization: Bearer token
`

	var img ImageConfig
	if err := yaml.Unmarshal([]byte(doc), &img); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(img.ExternalAPIs) != 1 {
		t.Fatalf("external_apis 数量 = %d, want 1（配置项键名应大小写不敏感）", len(img.ExternalAPIs))
	}

	api := img.ExternalAPIs[0]
	if api.Name != "unsplash" || api.URLField != "urls.raw" || api.ResponseType != "json" {
		t.Fatalf("字段解析异常: name=%q url_field=%q response_type=%q",
			api.Name, api.URLField, api.ResponseType)
	}

	// 头名必须原样保留
	for _, key := range []string{"X-Api-Key", "User-Agent", "Authorization"} {
		if _, ok := api.Headers[key]; !ok {
			t.Errorf("headers 缺少原始键名 %q（实际 keys=%v）", key, headerKeys(api.Headers))
		}
	}
	// 不得出现被小写化的重复键
	for _, lower := range []string{"x-api-key", "user-agent", "authorization"} {
		if _, ok := api.Headers[lower]; ok {
			t.Errorf("headers 出现被小写化的键 %q（实际 keys=%v）", lower, headerKeys(api.Headers))
		}
	}
	if got := api.Headers["X-Api-Key"]; got != "secret" {
		t.Errorf("X-Api-Key 值 = %q, want \"secret\"", got)
	}
}

// headerKeys 返回 map 的键列表（仅用于测试失败时的可读输出）。
func headerKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
