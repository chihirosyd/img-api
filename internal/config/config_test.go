package config

import (
	"os"
	"path/filepath"
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
	saved := make(map[string]string)
	for _, k := range keys {
		if v, ok := os.LookupEnv(k); ok {
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
