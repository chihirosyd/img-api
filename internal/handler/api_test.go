package handler

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

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
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

func newTestContext(query string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/random"+query, nil)
	return c, w
}

func TestParseParams(t *testing.T) {
	// 空参数 → 默认值（source 取 DEFAULT_SOURCE 配置，默认 txt）
	c, _ := newTestContext("")
	p := parseParams(c)
	if p.Type != model.DeviceAuto || p.Source != model.SourceTxt ||
		p.Mode != model.ModeRedirect || p.Category != "default" || p.APIName != "" {
		t.Fatalf("default params = %+v", p)
	}

	// 全参数
	c, _ = newTestContext("?type=pe&source=local&mode=json&category=anime,scenery&api=flickr")
	p = parseParams(c)
	if p.Type != model.DevicePE || p.Source != model.SourceLocal ||
		p.Mode != model.ModeJSON || p.Category != "anime,scenery" || p.APIName != "flickr" {
		t.Fatalf("full params = %+v", p)
	}

	// 大小写不敏感（source/mode/type 统一转小写，api 由 FindByName 的 EqualFold 匹配）
	c, _ = newTestContext("?type=PE&source=TXT&mode=JSON&api=Flickr")
	p = parseParams(c)
	if p.Type != model.DevicePE || p.Source != model.SourceTxt ||
		p.Mode != model.ModeJSON || p.APIName != "Flickr" {
		t.Fatalf("case-insensitive params = %+v", p)
	}
}

func TestRenderSetupGuide(t *testing.T) {
	h := &APIHandler{}

	// JSON 模式 → 503 + JSON 提示
	c, w := newTestContext("")
	h.renderSetupGuide(c, model.ModeJSON)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("json mode status = %d, want 503", w.Code)
	}
	if !strings.Contains(w.Body.String(), "not configured") {
		t.Fatalf("json body = %q", w.Body.String())
	}

	// image 模式 → 200 + SVG 占位图（<img> 嵌入可显示）
	c, w = newTestContext("")
	h.renderSetupGuide(c, model.ModeImage)
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "image/svg+xml") {
		t.Fatalf("image mode status=%d type=%q", w.Code, w.Header().Get("Content-Type"))
	}
	if !strings.Contains(w.Body.String(), "<svg") {
		t.Fatalf("svg body missing <svg: %q", w.Body.String())
	}

	// 浏览器导航（Accept 以 text/html 开头但含 image/avif）→ HTML 引导页 503
	// （回归：Contains("image/") 会把导航请求误判为图片请求返回 SVG）
	c, w = newTestContext("")
	c.Request.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	h.renderSetupGuide(c, model.ModeRedirect)
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
	c, w := newTestContext("")
	h.renderCategoryNotFound(c, "anime", []string{"default"}, model.ModeJSON)
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "anime") ||
		!strings.Contains(w.Body.String(), "default") {
		t.Fatalf("json mode status=%d body=%q", w.Code, w.Body.String())
	}

	// HTML 模式 → 404，分类名中的脚本标签应被转义（XSS 回归）
	c, w = newTestContext("")
	c.Request.Header.Set("Accept", "text/html")
	h.renderCategoryNotFound(c, "<script>alert(1)</script>", nil, model.ModeRedirect)
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
	c, w := newTestContext("")
	h.serveLocalFile(c, filepath.Join(root, "a.jpg"))
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/jpeg" || w.Body.String() != "imgdata" {
		t.Fatalf("normal file: status=%d type=%q body=%q", w.Code, w.Header().Get("Content-Type"), w.Body.String())
	}

	// 根目录外 → 403
	c, w = newTestContext("")
	h.serveLocalFile(c, filepath.Join(root, "..", "escape.jpg"))
	if w.Code != http.StatusForbidden {
		t.Fatalf("escape status = %d, want 403", w.Code)
	}

	// 非图片扩展名 → 502
	c, w = newTestContext("")
	h.serveLocalFile(c, filepath.Join(root, "note.txt"))
	if w.Code != http.StatusBadGateway {
		t.Fatalf("non-image status = %d, want 502", w.Code)
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
	c, w := newTestContext("")
	c.Request.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	h.Home(c)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "img-api") {
		t.Fatalf("browser home: status=%d body=%q", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "服务运行中") || !strings.Contains(w.Body.String(), "总请求") {
		t.Fatalf("dashboard missing: %q", w.Body.String())
	}

	// 配置 HEALTH_SECRET → 首页仅极简状态，不暴露统计等内部信息
	config.C.HealthSecret = "secret"
	defer func() { config.C.HealthSecret = "" }()
	c, w = newTestContext("")
	c.Request.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	h.Home(c)
	if !strings.Contains(w.Body.String(), "服务运行中") || strings.Contains(w.Body.String(), "总请求") {
		t.Fatalf("secret mode dashboard should hide stats: %q", w.Body.String())
	}

	// <img> 嵌入（Accept 含 image/）→ 302 图片
	c, w = newTestContext("")
	c.Request.Header.Set("Accept", "image/avif,image/webp,*/*;q=0.8")
	h.Home(c)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "https://example.com/a.jpg" {
		t.Fatalf("img embed: status=%d location=%q", w.Code, w.Header().Get("Location"))
	}

	// 显式 mode 参数 → 走图片接口（json）
	c, w = newTestContext("?mode=json")
	h.Home(c)
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
	c, w := newTestContext("")
	h.Random(c)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "https://example.com/a.jpg" {
		t.Fatalf("redirect: status=%d location=%q", w.Code, w.Header().Get("Location"))
	}

	// json 模式 → 200 + 图片 URL
	c, w = newTestContext("?source=txt&mode=json")
	h.Random(c)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "example.com") {
		t.Fatalf("json: status=%d body=%q", w.Code, w.Body.String())
	}

	// 分类不存在 → 404 提示页
	c, w = newTestContext("?source=txt&category=missing&mode=json")
	h.Random(c)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing category status = %d, want 404", w.Code)
	}

	// 图源未配置（local 空）→ 503 引导页
	c, w = newTestContext("?source=local&mode=json")
	h.Random(c)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("empty local source status = %d, want 503", w.Code)
	}
}
