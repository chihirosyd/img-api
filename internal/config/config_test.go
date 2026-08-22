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

// TestLoad 验证 .env 解析 → 默认值回退 → 结构体映射的完整链路。
func TestLoad(t *testing.T) {
	dir := t.TempDir()
	content := "APP_PORT=9090\n" +
		"AUTH_ENABLED=true\nAUTH_TOKEN=secret\n" +
		"RATE_LIMIT_MAX=120\n" +
		"REFERER_WHITELIST=a.com,b.com\n" +
		"TRUSTED_PROXIES=10.0.0.0/8\n" +
		"DEFAULT_SOURCE=local\n" +
		"CIRCUIT_FAILURE_THRESHOLD=10\n" +
		"HEALTH_SECRET=hs\n" +
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
	if !C.AuthEnabled || C.AuthToken != "secret" {
		t.Errorf("Auth = %v/%q", C.AuthEnabled, C.AuthToken)
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
	if C.CircuitFailureThreshold != 10 {
		t.Errorf("CircuitFailureThreshold = %d, want 10", C.CircuitFailureThreshold)
	}
	if C.HealthSecret != "hs" {
		t.Errorf("HealthSecret = %q", C.HealthSecret)
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
	if err := Load(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if C.Port != 8080 || C.Name != "img-api" || C.DefaultSource != "txt" {
		t.Errorf("unexpected defaults: port=%d name=%q source=%q", C.Port, C.Name, C.DefaultSource)
	}
}
