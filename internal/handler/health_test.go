package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"img-api/internal/cache"
	"img-api/internal/model"
	"img-api/internal/service"
)

// TestHealthOKWhenNoSourceConfigured 回归 R5-01：一个图源都没配置的实例（全新安装）
// 返回 ok —— 判定只覆盖"已配置内容"的图源，没有可判定的对象时不应虚报 degraded。
func TestHealthOKWhenNoSourceConfigured(t *testing.T) {
	root := t.TempDir()
	stats := service.NewStats()
	svc := service.NewRandomService(root, cache.NewMemoryCache(), nil, stats)
	h := NewHealthHandler(svc, stats)

	w := httptest.NewRecorder()
	h.Health(w, httptest.NewRequest(http.MethodGet, "/health", nil))

	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("响应不是合法 JSON: %v (body=%q)", err, w.Body.String())
	}
	if w.Code != http.StatusOK {
		t.Errorf("状态码 = %d, want 200（/health 恒 200，状态在 body）", w.Code)
	}
	if got["status"] != "ok" {
		t.Errorf("status = %v, want ok（没有已配置图源时无可判定对象）", got["status"])
	}
}

// TestHealthDegradedWhenDeviceDirMissing 回归 D6 / R5-01：txt 有内容但 pe 子目录缺失
// → 仓库不健康 → /health 报 degraded（"已配置但坏了"必须能被检出）。
func TestHealthDegradedWhenDeviceDirMissing(t *testing.T) {
	root := t.TempDir()
	// 只建 pc，缺 pe；txt 内有内容，因此 txt 属于"已配置"、参与判定
	if err := os.MkdirAll(filepath.Join(root, "resources", "txt", "pc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "resources", "txt", "pc", "default.txt"),
		[]byte("https://example.com/a.jpg\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stats := service.NewStats()
	svc := service.NewRandomService(root, cache.NewMemoryCache(), nil, stats)
	_ = svc.GetRepo(model.SourceTxt) // Health 只遍历已初始化的仓库
	h := NewHealthHandler(svc, stats)

	w := httptest.NewRecorder()
	h.Health(w, httptest.NewRequest(http.MethodGet, "/health", nil))

	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if got["status"] != "degraded" {
		t.Errorf("status = %v, want degraded（pc/pe 子目录缺失应被检出）", got["status"])
	}
	checks, _ := got["checks"].(map[string]any)
	if checks == nil {
		t.Fatal("checks 字段缺失")
	}
	if s, _ := checks["txt"].(string); s == "healthy" {
		t.Errorf("checks.txt = %q, want 含设备目录缺失原因", s)
	}
}

// TestHealthOKWithoutExternalRepo 回归 R5-01（修正 P4-6 的过度收紧）：
// txt 有内容且健康、local 无内容（未配置）、external 未配置 →
// checks 里没有 "external" 键，但不应因此判为 degraded。
func TestHealthOKWithoutExternalRepo(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{
		filepath.Join("resources", "txt", "pc"),
		filepath.Join("resources", "txt", "pe"),
		filepath.Join("resources", "local", "pc"),
		filepath.Join("resources", "local", "pe"),
	} {
		if err := os.MkdirAll(filepath.Join(root, rel), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// txt 图库有内容，避免落入"图源未配置"分支（该分支不属于 health 判定）
	if err := os.WriteFile(filepath.Join(root, "resources", "txt", "pc", "default.txt"),
		[]byte("https://example.com/a.jpg\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stats := service.NewStats()
	svc := service.NewRandomService(root, cache.NewMemoryCache(), nil, stats)
	// 预热 txt/local：两者健康；external 未配置（pool 为 nil）→ 未初始化
	_ = svc.GetRepo(model.SourceTxt)
	_ = svc.GetRepo(model.SourceLocal)

	h := NewHealthHandler(svc, stats)
	w := httptest.NewRecorder()
	h.Health(w, httptest.NewRequest(http.MethodGet, "/health", nil))

	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	checks, _ := got["checks"].(map[string]any)
	if checks == nil {
		t.Fatal("checks 字段缺失")
	}
	for _, src := range []string{"txt", "local"} {
		if s, _ := checks[src].(string); s != "healthy" {
			t.Errorf("checks.%s = %q, want healthy", src, s)
		}
	}
	if _, ok := checks["external"]; ok {
		t.Errorf("checks.external 不该存在（external 走 ExternalPool，不经 repos）")
	}
	if got["status"] != "ok" {
		t.Errorf("status = %v, want ok（external 未配置不参与判定）", got["status"])
	}
}

// TestHealthUptimeMatchesStats 回归 N-02：同一响应里的 uptime 与 stats.uptime_seconds
// 必须同源——两者各自取时刻时会夹进启动期的预热扫描，一个响应报出两个运行时长。
func TestHealthUptimeMatchesStats(t *testing.T) {
	stats := service.NewStats()
	svc := service.NewRandomService(t.TempDir(), cache.NewMemoryCache(), nil, stats)
	h := NewHealthHandler(svc, stats)

	w := httptest.NewRecorder()
	h.Health(w, httptest.NewRequest(http.MethodGet, "/health", nil))

	var got struct {
		Uptime string `json:"uptime"`
		Stats  struct {
			UptimeSeconds int64 `json:"uptime_seconds"`
		} `json:"stats"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if want := formatDuration(time.Duration(got.Stats.UptimeSeconds) * time.Second); got.Uptime != want {
		t.Errorf("uptime = %q, stats.uptime_seconds = %d, 两者不同源（应为 %q）",
			got.Uptime, got.Stats.UptimeSeconds, want)
	}
	// 独立时间点已移除，防止再引入第二个时间源
	if _, ok := reflect.TypeOf(HealthHandler{}).FieldByName("startAt"); ok {
		t.Error("HealthHandler 不该再持有独立的 startAt")
	}
}

// TestHealthCircuitTripFieldNames 回归 C-02：checks.circuit_trips（熔断器进入 OPEN 的次数，
// 字符串）与 stats.circuit_rejected（被熔断器拒绝的请求数，整数）必须不同名。
func TestHealthCircuitTripFieldNames(t *testing.T) {
	stats := service.NewStats()
	stats.RecordCircuitTrip()
	svc := service.NewRandomService(t.TempDir(), cache.NewMemoryCache(), nil, stats)
	h := NewHealthHandler(svc, stats)

	w := httptest.NewRecorder()
	h.Health(w, httptest.NewRequest(http.MethodGet, "/health", nil))

	var got struct {
		Checks map[string]string `json:"checks"`
		Stats  map[string]any    `json:"stats"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if _, ok := got.Checks["circuit_trips"]; !ok {
		t.Error("checks.circuit_trips 应保留（熔断器进入 OPEN 的次数）")
	}
	if _, ok := got.Stats["circuit_trips"]; ok {
		t.Error("stats.circuit_trips 应改名为 stats.circuit_rejected")
	}
	if v, ok := got.Stats["circuit_rejected"]; !ok || v != float64(1) {
		t.Errorf("stats.circuit_rejected = %v (存在=%v), want 1", v, ok)
	}
}

// TestHealthChecksCached 回归 I-03：checks 在 healthCacheTTL 内复用，避免 /health 被轮询时
// 反复扫盘；缓存只包 checks，status 仍反映缓存时刻的真实状态。
func TestHealthChecksCached(t *testing.T) {
	root := t.TempDir()
	pc := filepath.Join(root, "resources", "txt", "pc")
	if err := os.MkdirAll(pc, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pc, "default.txt"), []byte("https://example.com/a.jpg\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stats := service.NewStats()
	svc := service.NewRandomService(root, cache.NewMemoryCache(), nil, stats)
	_ = svc.GetRepo(model.SourceTxt)
	h := NewHealthHandler(svc, stats)

	statusOf := func() string {
		w := httptest.NewRecorder()
		h.Health(w, httptest.NewRequest(http.MethodGet, "/health", nil))
		var got map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("响应不是合法 JSON: %v", err)
		}
		s, _ := got["status"].(string)
		return s
	}

	// pe 子目录缺失 → 首次请求应检出 degraded
	if got := statusOf(); got != "degraded" {
		t.Fatalf("首次 status = %q, want degraded（pe 子目录缺失）", got)
	}

	// 补齐目录后立即再请求：仍命中缓存，不应立刻翻牌
	if err := os.MkdirAll(filepath.Join(root, "resources", "txt", "pe"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := statusOf(); got != "degraded" {
		t.Errorf("缓存期内 status = %q, want degraded（%v 内复用结果）", got, healthCacheTTL)
	}

	// 缓存过期后应反映真实状态
	h.checksMu.Lock()
	h.checksAt = time.Time{}
	h.checksMu.Unlock()
	if got := statusOf(); got != "ok" {
		t.Errorf("缓存过期后 status = %q, want ok", got)
	}
}
