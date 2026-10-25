package handler

import (
	"crypto/tls"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"img-api/internal/cache"
	"img-api/internal/config"
	"img-api/internal/logger"
	"img-api/internal/model"
	"img-api/internal/service"
)

func TestMain(m *testing.M) {
	// config.C / logger.L 是包级全局，测试中先初始化（.env 缺失时使用默认值）
	if err := config.Load(filepath.Join(os.TempDir(), "img-api-handler-test")); err != nil {
		panic(err)
	}
	logger.L = slog.New(slog.NewTextHandler(io.Discard, nil))
	os.Exit(m.Run())
}

func newTestContext(query string) (*httptest.ResponseRecorder, *http.Request) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/random"+query, nil)
	return w, r
}

func TestParseParams(t *testing.T) {
	// 空参数 → 默认值（source 取 DEFAULT_SOURCE 配置，默认 txt）
	_, r := newTestContext("")
	p := parseParams(r)
	if p.Type != model.DeviceAuto || p.Source != model.SourceTxt ||
		p.Mode != model.ModeRedirect || p.Category != "default" || p.APIName != "" {
		t.Fatalf("default params = %+v", p)
	}

	// 全参数
	_, r = newTestContext("?type=pe&source=local&mode=json&category=anime,scenery&api=flickr")
	p = parseParams(r)
	if p.Type != model.DevicePE || p.Source != model.SourceLocal ||
		p.Mode != model.ModeJSON || p.Category != "anime,scenery" || p.APIName != "flickr" {
		t.Fatalf("full params = %+v", p)
	}

	// 大小写不敏感（source/mode/type 统一转小写，api 由 FindByName 的 EqualFold 匹配）
	_, r = newTestContext("?type=PE&source=TXT&mode=JSON&api=Flickr")
	p = parseParams(r)
	if p.Type != model.DevicePE || p.Source != model.SourceTxt ||
		p.Mode != model.ModeJSON || p.APIName != "Flickr" {
		t.Fatalf("case-insensitive params = %+v", p)
	}
}

func TestRenderSetupGuide(t *testing.T) {
	h := &APIHandler{}

	// JSON 模式 → 503 + JSON 提示
	w, r := newTestContext("")
	h.renderSetupGuide(w, r, model.ModeJSON)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("json mode status = %d, want 503", w.Code)
	}
	if !strings.Contains(w.Body.String(), "not configured") {
		t.Fatalf("json body = %q", w.Body.String())
	}

	// image 模式 → 200 + SVG 占位图（<img> 嵌入可显示）
	w, r = newTestContext("")
	h.renderSetupGuide(w, r, model.ModeImage)
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "image/svg+xml") {
		t.Fatalf("image mode status=%d type=%q", w.Code, w.Header().Get("Content-Type"))
	}
	if !strings.Contains(w.Body.String(), "<svg") {
		t.Fatalf("svg body missing <svg: %q", w.Body.String())
	}

	// 浏览器导航（Accept 以 text/html 开头但含 image/avif）→ HTML 引导页 503
	// （回归：Contains("image/") 会把导航请求误判为图片请求返回 SVG）
	w, r = newTestContext("")
	r.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	h.renderSetupGuide(w, r, model.ModeRedirect)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "img-api") {
		t.Fatalf("browser guide: status=%d body=%q", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "<svg") {
		t.Fatalf("browser navigation should get HTML, not SVG: %q", w.Body.String())
	}
}

func TestRenderCategoryNotFound(t *testing.T) {
	h := &APIHandler{}

	// JSON 模式 → 404 + 可用列表
	w, r := newTestContext("")
	h.renderCategoryNotFound(w, r, "anime", []string{"default"}, model.ModeJSON)
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "anime") ||
		!strings.Contains(w.Body.String(), "default") {
		t.Fatalf("json mode status=%d body=%q", w.Code, w.Body.String())
	}

	// HTML 模式 → 404，分类名中的脚本标签应被转义（XSS 回归）
	w, r = newTestContext("")
	r.Header.Set("Accept", "text/html")
	h.renderCategoryNotFound(w, r, "<script>alert(1)</script>", nil, model.ModeRedirect)
	if w.Code != http.StatusNotFound {
		t.Fatalf("html mode status = %d, want 404", w.Code)
	}
	if strings.Contains(w.Body.String(), "<script>alert") || !strings.Contains(w.Body.String(), "&lt;script&gt;") {
		t.Fatalf("category not escaped in html: %q", w.Body.String())
	}
}

func TestServeLocalFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.jpg"), []byte("imgdata"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	h := &APIHandler{rootPath: root}

	// 正常图片 → 200 + image/jpeg + 原始内容
	w, r := newTestContext("")
	h.serveLocalFile(w, r, filepath.Join(root, "a.jpg"))
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/jpeg" || w.Body.String() != "imgdata" {
		t.Fatalf("normal file: status=%d type=%q body=%q", w.Code, w.Header().Get("Content-Type"), w.Body.String())
	}

	// 根目录外 → 403
	w, r = newTestContext("")
	h.serveLocalFile(w, r, filepath.Join(root, "..", "escape.jpg"))
	if w.Code != http.StatusForbidden {
		t.Fatalf("escape status = %d, want 403", w.Code)
	}

	// 非图片扩展名 → 415（本地图源，不是网关错误，不用 502）
	w, r = newTestContext("")
	h.serveLocalFile(w, r, filepath.Join(root, "note.txt"))
	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("non-image status = %d, want 415", w.Code)
	}
}

// TestServeLocalFileSymlinkBlocked 符号链接（即使指向项目根目录内的图片）→ 403。
// Windows 创建符号链接需要开发者模式/管理员权限，跳过（Linux CI 覆盖）。
func TestServeLocalFileSymlinkBlocked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on Windows; covered on Linux CI")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "real.jpg"), []byte("imgdata"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.jpg")
	if err := os.Symlink(filepath.Join(root, "real.jpg"), link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	h := &APIHandler{rootPath: root}
	w, r := newTestContext("")
	h.serveLocalFile(w, r, link)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (symlink blocked)", w.Code)
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{45 * time.Second, "45s"},
		{5*time.Minute + 30*time.Second, "5m 30s"},
		{2*time.Hour + 15*time.Minute, "2h 15m"},
		{24 * time.Hour, "1d"},
		{45*time.Hour + 15*time.Minute, "1d 21h"},
		{30 * 24 * time.Hour, "1mo"},
		{60 * 24 * time.Hour, "2mo"},
		{365 * 24 * time.Hour, "1y"},
		{400 * 24 * time.Hour, "1y 1mo"},
		{0, "0s"},
	}
	for _, c := range cases {
		if got := formatDuration(c.in); got != c.want {
			t.Errorf("formatDuration(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestHome(t *testing.T) {
	root := t.TempDir()
	pc := filepath.Join(root, "resources", "txt", "pc")
	if err := os.MkdirAll(pc, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pc, "default.txt"), []byte("https://example.com/a.jpg\n"), 0644); err != nil {
		t.Fatal(err)
	}

	stats := service.NewStats()
	svc := service.NewRandomService(root, cache.NewMemoryCache(), nil, stats)
	h := NewAPIHandler(root, svc, stats)

	// 浏览器地址栏访问（真实浏览器 Accept：以 text/html 开头，但包含 image/avif）
	// → 教程首页 200 + 运行状态仪表盘（回归：Contains("image/") 会误判成图片请求）
	w, r := newTestContext("")
	r.URL.Path = "/"
	r.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	h.Home(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "img-api") {
		t.Fatalf("browser home: status=%d body=%q", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "服务运行中") || !strings.Contains(w.Body.String(), "总请求") {
		t.Fatalf("dashboard missing: %q", w.Body.String())
	}

	// <img> 嵌入（Accept 含 image/）→ 302 图片
	w, r = newTestContext("")
	r.URL.Path = "/"
	r.Header.Set("Accept", "image/avif,image/webp,*/*;q=0.8")
	h.Home(w, r)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "https://example.com/a.jpg" {
		t.Fatalf("img embed: status=%d location=%q", w.Code, w.Header().Get("Location"))
	}

	// 显式 mode 参数 → 走图片接口（json）
	w, r = newTestContext("?mode=json")
	r.URL.Path = "/"
	h.Home(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "example.com") {
		t.Fatalf("mode json: status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestRandomHandler(t *testing.T) {
	root := t.TempDir()
	pc := filepath.Join(root, "resources", "txt", "pc")
	if err := os.MkdirAll(pc, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pc, "default.txt"), []byte("https://example.com/a.jpg\n"), 0644); err != nil {
		t.Fatal(err)
	}

	stats := service.NewStats()
	svc := service.NewRandomService(root, cache.NewMemoryCache(), nil, stats)
	h := NewAPIHandler(root, svc, stats)

	// redirect 模式 → 302 + Location
	w, r := newTestContext("")
	h.Random(w, r)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "https://example.com/a.jpg" {
		t.Fatalf("redirect: status=%d location=%q", w.Code, w.Header().Get("Location"))
	}

	// json 模式 → 200 + 图片 URL
	w, r = newTestContext("?source=txt&mode=json")
	h.Random(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "example.com") {
		t.Fatalf("json: status=%d body=%q", w.Code, w.Body.String())
	}

	// 分类不存在 → 404 提示页
	w, r = newTestContext("?source=txt&category=missing&mode=json")
	h.Random(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing category status = %d, want 404", w.Code)
	}

	// 图源未配置（local 空）→ 503 引导页
	w, r = newTestContext("?source=local&mode=json")
	h.Random(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("empty local source status = %d, want 503", w.Code)
	}

	// 回归 Z9：超长 category 参数 → 400（防日志膨胀/响应放大）
	w, r = newTestContext("?category=" + strings.Repeat("a", 129))
	h.Random(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("oversized category status = %d, want 400", w.Code)
	}

	// 回归 Z9：超长 api 参数 → 400
	w, r = newTestContext("?source=external&api=" + strings.Repeat("b", 129))
	h.Random(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("oversized api status = %d, want 400", w.Code)
	}
}

// TestRandomLocalRedirectAutoDegrade 回归 Z10：
// source=local 且 mode 为默认 redirect 时自动降级为 image（服务端直接出图），
// 不再返回 400 破图。
func TestRandomLocalRedirectAutoDegrade(t *testing.T) {
	root := t.TempDir()
	localDir := filepath.Join(root, "resources", "local", "pc", "default")
	if err := os.MkdirAll(localDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localDir, "b.png"), []byte("pngdata"), 0644); err != nil {
		t.Fatal(err)
	}

	stats := service.NewStats()
	svc := service.NewRandomService(root, cache.NewMemoryCache(), nil, stats)
	h := NewAPIHandler(root, svc, stats)

	// 不传 mode（默认 redirect）→ 自动降级 image，直接输出图片内容
	w, r := newTestContext("?source=local")
	h.Random(w, r)
	if w.Code != http.StatusOK || w.Body.String() != "pngdata" {
		t.Fatalf("local default mode: status=%d body=%q, want 200 + pngdata", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", ct)
	}

	// mode=json → url 字段为相对图库根目录路径（回归 L11：不泄露服务器绝对路径）
	w, r = newTestContext("?source=local&mode=json")
	h.Random(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "pc/default/b.png") {
		t.Fatalf("local json: status=%d body=%q, want relative path", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), filepath.ToSlash(root)) {
		t.Fatalf("local json leaked absolute root path: %q", w.Body.String())
	}
}

// TestHomeScheme 回归 L-02：首页示例 URL 的 scheme 跟随实际连接协议，
// 不再写死 http://。未终结 TLS 时不采信 X-Forwarded-Proto（防伪造）。
func TestHomeScheme(t *testing.T) {
	root := t.TempDir()
	pc := filepath.Join(root, "resources", "txt", "pc")
	if err := os.MkdirAll(pc, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pc, "default.txt"), []byte("https://example.com/a.jpg\n"), 0644); err != nil {
		t.Fatal(err)
	}

	stats := service.NewStats()
	svc := service.NewRandomService(root, cache.NewMemoryCache(), nil, stats)
	h := NewAPIHandler(root, svc, stats)

	// 直连（无 TLS）：即使带了 X-Forwarded-Proto 也只能是 http
	w, r := newTestContext("")
	r.URL.Path = "/"
	r.Header.Set("Accept", "text/html")
	r.Header.Set("X-Forwarded-Proto", "https")
	h.Home(w, r)
	body := w.Body.String()
	if !strings.Contains(body, `src="http://example.com/random"`) {
		t.Fatalf("plain http home body missing http:// example: status=%d", w.Code)
	}
	if strings.Contains(body, `src="https://example.com/random"`) {
		t.Fatal("plain http home body leaked https:// example（X-Forwarded-Proto 不应被采信）")
	}

	// TLS 连接 → https://
	w, r = newTestContext("")
	r.URL.Path = "/"
	r.Header.Set("Accept", "text/html")
	r.TLS = &tls.ConnectionState{}
	h.Home(w, r)
	if !strings.Contains(w.Body.String(), `src="https://example.com/random"`) {
		t.Fatalf("tls home body missing https:// example: status=%d", w.Code)
	}
}

// TestRandomCategoryIllegalChars 回归 N-03：含 Windows 文件名非法字符、控制字符
// 或首尾空白的分类名在参数校验阶段直接 400（此前会由 os.Stat 抛出非 ENOENT 错误
// 落到 500 + ERROR 日志）。中文分类名必须继续被接受，因此不做 ASCII 白名单。
func TestRandomCategoryIllegalChars(t *testing.T) {
	root := t.TempDir()
	pc := filepath.Join(root, "resources", "txt", "pc")
	if err := os.MkdirAll(pc, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pc, "default.txt"), []byte("https://example.com/a.jpg\n"), 0644); err != nil {
		t.Fatal(err)
	}

	stats := service.NewStats()
	svc := service.NewRandomService(root, cache.NewMemoryCache(), nil, stats)
	h := NewAPIHandler(root, svc, stats)

	for _, category := range []string{
		"a*b", "a?b", `a"b`, "a<b", "a>b", "a|b", "a:b",
		"a\x00b", "a\tb", " a", "a ", "风景*",
	} {
		w, r := newTestContext("?source=txt&category=" + url.QueryEscape(category))
		h.Random(w, r)
		if w.Code != http.StatusBadRequest {
			t.Errorf("category %q: status = %d, want 400", category, w.Code)
			continue
		}
		if body := w.Body.String(); !strings.Contains(body, `"code":400`) {
			t.Errorf("category %q: body = %q, want 与既有 400 一致的 JSON", category, body)
		}
	}

	// 中文 / 逗号多选 / 允许的标点仍通过字符校验（不会得到 400）
	for _, category := range []string{"风景", "anime,scenery", "分类-1_2"} {
		w, r := newTestContext("?source=txt&mode=json&category=" + url.QueryEscape(category))
		h.Random(w, r)
		if w.Code == http.StatusBadRequest {
			t.Errorf("category %q: status = 400, want 通过字符校验", category)
		}
	}
}

// TestRandomImageAbortRecordsFail 回归 L-03：超限中止（panic http.ErrAbortHandler）
// 也必须记一次失败，否则这类请求在统计里凭空消失（RecordRequest 有、成功/失败都没有）。
func TestRandomImageAbortRecordsFail(t *testing.T) {
	root := t.TempDir()
	pc := filepath.Join(root, "resources", "txt", "pc")
	if err := os.MkdirAll(pc, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pc, "default.txt"), []byte("http://upstream.test/a.png\n"), 0644); err != nil {
		t.Fatal(err)
	}

	oldMax := maxProxyImageBytes
	t.Cleanup(func() { maxProxyImageBytes = oldMax })
	maxProxyImageBytes = 8
	withFakeProxy(t, http.StatusOK, "image/png", strings.NewReader("0123456789abcdefghij"))

	stats := service.NewStats()
	svc := service.NewRandomService(root, cache.NewMemoryCache(), nil, stats)
	h := NewAPIHandler(root, svc, stats)

	w, r := newTestContext("?source=txt&mode=image")
	func() {
		defer func() {
			if rec := recover(); rec != http.ErrAbortHandler {
				t.Fatalf("recover = %v, want http.ErrAbortHandler", rec)
			}
		}()
		h.Random(w, r)
	}()

	snap := stats.Snapshot()
	if snap.TotalRequests != 1 || snap.TotalFail != 1 || snap.TotalSuccess != 0 {
		t.Fatalf("stats requests=%d fail=%d success=%d, want 1/1/0",
			snap.TotalRequests, snap.TotalFail, snap.TotalSuccess)
	}
}
