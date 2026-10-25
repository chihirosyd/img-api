package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseWhitelist(t *testing.T) {
	got := parseWhitelist(" a.com, blog.b.com ,,c.com ")
	want := []string{"a.com", "blog.b.com", "c.com"}

	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	if parseWhitelist("") != nil {
		t.Error("empty string should return nil")
	}
	if parseWhitelist(" , , ") != nil {
		t.Error("whitespace-only string should return nil")
	}
}

// clearEnv 清空会影响 Load 的环境变量并自动恢复。
// 环境变量的优先级高于 .env 文件与默认值：开发机终端里若残留
// APP_PORT 等导出变量（如冒烟测试时设置过），TestLoad 会在污染环境下失败。
// 清空后测试结果与执行环境无关。
// 匹配大小写不敏感（lookupEnvFold 会同时命中小写键，Linux 上 os.LookupEnv
// 大小写敏感，因此必须遍历 os.Environ 清理所有大小写变体）。
func clearEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"APP_DEBUG", "APP_NAME", "APP_HOST", "APP_PORT", "APP_VERSION",
		"CORS_ENABLED", "RATE_LIMIT_ENABLED", "RATE_LIMIT_MAX",
		"REFERER_WHITELIST", "TRUSTED_PROXIES", "DEFAULT_SOURCE", "LOCAL_INDEX_REFRESH",
		"TXT_DEFAULT_CATEGORY", "LOCAL_DEFAULT_CATEGORY",
		"REDIS_ADDR", "REDIS_PASSWORD", "REDIS_DB",
		"CIRCUIT_FAILURE_THRESHOLD", "CIRCUIT_TIMEOUT_SECONDS", "CIRCUIT_HALF_OPEN_MAX",
		"LOG_LEVEL", "LOG_DIR", "LOG_MAX_AGE", "LOG_MAX_SIZE",
	}
	target := make(map[string]bool, len(keys))
	for _, k := range keys {
		target[strings.ToLower(k)] = true
	}

	saved := make(map[string]string)
	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if target[strings.ToLower(k)] {
			saved[k] = v
			os.Unsetenv(k)
		}
	}
	t.Cleanup(func() {
		for k, v := range saved {
			os.Setenv(k, v)
		}
	})
}

// TestLoad 验证 .env 解析 → 默认值回退 → 结构体映射的完整链路。
func TestLoad(t *testing.T) {
	clearEnv(t)

	dir := t.TempDir()
	content := "APP_PORT=9090\n" +
		"RATE_LIMIT_MAX=120\n" +
		"REFERER_WHITELIST=a.com,b.com\n" +
		"TRUSTED_PROXIES=10.0.0.0/8\n" +
		"DEFAULT_SOURCE=local\n" +
		"LOCAL_INDEX_REFRESH=@daily\n" +
		"TXT_DEFAULT_CATEGORY=wallpaper\n" +
		"LOCAL_DEFAULT_CATEGORY=photos\n" +
		"CIRCUIT_FAILURE_THRESHOLD=10\n" +
		"LOG_LEVEL=debug\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	if err := Load(dir); err != nil {
		t.Fatal(err)
	}

	if C.Port != 9090 {
		t.Errorf("Port = %d, want 9090", C.Port)
	}
	if C.RateLimitMax != 120 {
		t.Errorf("RateLimitMax = %d, want 120", C.RateLimitMax)
	}
	if len(C.RefererWhitelist) != 2 || C.RefererWhitelist[0] != "a.com" {
		t.Errorf("RefererWhitelist = %v", C.RefererWhitelist)
	}
	if C.DefaultSource != "local" {
		t.Errorf("DefaultSource = %q, want local", C.DefaultSource)
	}
	if C.LocalIndexRefresh != "@daily" {
		t.Errorf("LocalIndexRefresh = %q, want @daily", C.LocalIndexRefresh)
	}
	if C.TxtDefaultCategory != "wallpaper" || C.LocalDefaultCategory != "photos" {
		t.Errorf("default categories = %q / %q, want wallpaper / photos",
			C.TxtDefaultCategory, C.LocalDefaultCategory)
	}
	if C.CircuitFailureThreshold != 10 {
		t.Errorf("CircuitFailureThreshold = %d, want 10", C.CircuitFailureThreshold)
	}
	if C.LogLevel != "debug" {
		t.Errorf("LogLevel = %q", C.LogLevel)
	}
	if C.ListenAddr() != "0.0.0.0:9090" {
		t.Errorf("ListenAddr = %q", C.ListenAddr())
	}
	if C.IsRedisEnabled() {
		t.Error("IsRedisEnabled should be false without REDIS_ADDR")
	}
	// 未配置项回退默认值
	if C.RateLimitEnabled != true || C.CircuitTimeoutSeconds != 30 || C.CircuitHalfOpenMax != 3 {
		t.Error("defaults not applied for unset items")
	}
}

// TestLoadMissingEnv 验证无 .env 时使用默认值启动（最小可用）。
func TestLoadMissingEnv(t *testing.T) {
	clearEnv(t)

	if err := Load(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if C.Port != 8080 || C.Name != "img-api" || C.DefaultSource != "txt" {
		t.Errorf("unexpected defaults: port=%d name=%q source=%q", C.Port, C.Name, C.DefaultSource)
	}
	if C.LocalIndexRefresh != "" {
		t.Errorf("LocalIndexRefresh default = %q, want empty (disabled)", C.LocalIndexRefresh)
	}
}

// TestLoadCircuitHalfOpenMaxFallback 验证"非法值回退到默认值"对两类非法输入都成立：
// CIRCUIT_HALF_OPEN_MAX=0（<=0 由边界校验兜底）与 =abc（Atoi 失败由 getInt 兜底）
// 的最终值必须一致且等于默认值 3——此前 =0 会回退成 1，同一种"非法"出现两种结果。
func TestLoadCircuitHalfOpenMaxFallback(t *testing.T) {
	values := make(map[string]int, 2)
	for _, raw := range []string{"0", "abc"} {
		clearEnv(t)
		t.Setenv("CIRCUIT_HALF_OPEN_MAX", raw)
		if err := Load(t.TempDir()); err != nil {
			t.Fatalf("CIRCUIT_HALF_OPEN_MAX=%q: %v", raw, err)
		}
		values[raw] = C.CircuitHalfOpenMax
	}

	if values["0"] != values["abc"] {
		t.Fatalf("非法值回退不一致：=0 → %d，=abc → %d", values["0"], values["abc"])
	}
	if values["0"] != 3 {
		t.Fatalf("非法值回退 = %d，want 3（默认值）", values["0"])
	}
}

// TestLoadBoolFallback 验证非法布尔值回退到默认值（与 getInt 的整数回退一致）：
// 此前 ParseBool 失败恒返回 false，RATE_LIMIT_ENABLED=yes 会静默把限流关掉。
// CORS_ENABLED / RATE_LIMIT_ENABLED 的默认值是 true，回退逻辑没生效就必然变 false；
// 最后一例是合法值 false，必须原样生效，不能被"回退默认"覆盖成 true。
func TestLoadBoolFallback(t *testing.T) {
	hasWarn := func(substr string) bool {
		for _, w := range Warnings {
			if strings.Contains(w, substr) {
				return true
			}
		}
		return false
	}

	cases := []struct {
		key   string
		raw   string
		want  bool
		warns bool
		read  func() bool
	}{
		{"CORS_ENABLED", "yes", true, true, func() bool { return C.CorsEnabled }},
		{"RATE_LIMIT_ENABLED", "on", true, true, func() bool { return C.RateLimitEnabled }},
		{"APP_DEBUG", "maybe", false, true, func() bool { return C.Debug }},
		{"RATE_LIMIT_ENABLED", "false", false, false, func() bool { return C.RateLimitEnabled }},
	}

	for _, tc := range cases {
		clearEnv(t)
		t.Setenv(tc.key, tc.raw)
		if err := Load(t.TempDir()); err != nil {
			t.Fatalf("%s=%q: %v", tc.key, tc.raw, err)
		}
		if got := tc.read(); got != tc.want {
			t.Errorf("%s=%q → %v, want %v", tc.key, tc.raw, got, tc.want)
		}
		if tc.warns && !hasWarn(tc.key+"=\""+tc.raw+"\"") {
			t.Errorf("%s=%q 回退默认值却没有告警（Warnings 中找不到该条）", tc.key, tc.raw)
		}
	}
}
