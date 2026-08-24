// Package handler 定义 HTTP 请求处理器。
//
// 处理流程：解析 query 参数 → 参数校验 → 设备检测 → 调用 Service → 写响应。
// handler 层不包含业务逻辑，仅做参数绑定和响应格式化。
//
// 错误处理策略：
//   - 图源未配置 → "开始使用"引导页（HTML / JSON / SVG 占位图，按客户端期望协商）
//   - 分类不存在 → "分类不存在"提示页（404，可用分类列表仅在 Debug 模式展示）
//   - 指定的 API 不存在 → "API 不存在"提示页（404，可用 API 列表仅在 Debug 模式展示）
//   - 其它错误  → 普通 500（Debug 模式附带详细错误信息）
package handler

import (
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"img-api/internal/config"
	"img-api/internal/device"
	"img-api/internal/logger"
	"img-api/internal/middleware"
	"img-api/internal/model"
	"img-api/internal/netx"
	"img-api/internal/service"
)

// proxyClient 用于 mode=image 的 HTTP 客户端。
// 安全：拨号前校验 IP（防 DNS rebinding）+ 10s 超时 + 连接池复用。
var proxyClient = netx.NewClient(10 * time.Second)

// APIHandler 处理图片相关的 HTTP 请求。
// 持有 Service 引用以获取随机图片，持有 Stats 引用以记录调用统计。
type APIHandler struct {
	rootPath string                 // 项目根目录（本地文件输出越界校验的锚点）
	svc      *service.RandomService // 随机图片服务
	stats    *service.Stats         // 请求统计（共享实例）
}

// NewAPIHandler 创建 API 处理器。rootPath 是项目根目录，
// 用于校验 source=local 返回的文件路径确实位于项目内（防越界）。
func NewAPIHandler(rootPath string, svc *service.RandomService, stats *service.Stats) *APIHandler {
	return &APIHandler{rootPath: filepath.Clean(rootPath), svc: svc, stats: stats}
}

// Home 处理 GET / —— 按访问者身份分流：
//   - 显式带了 mode 参数，或 Accept 头含 image/（浏览器 <img> 嵌入）→ 走图片接口
//   - 浏览器地址栏直接访问 → 返回项目介绍/教程首页（含运行状态仪表盘）
//
// 这样根路径既是"首页"又能直接嵌图，两不误。
func (h *APIHandler) Home(w http.ResponseWriter, r *http.Request) {
	// Go 1.22 mux 中 "GET /" 是兜底模式，会匹配任意路径；仅根路径返回首页
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	if r.URL.Query().Get("mode") != "" || acceptsImage(r) {
		h.Random(w, r)
		return
	}

	host := r.Host
	if host == "" {
		host = "localhost:8080"
	}
	page := strings.ReplaceAll(homePage, "{{HOST}}", html.EscapeString(host))
	page = strings.ReplaceAll(page, "{{STATUS}}", h.homeStatusHTML(r))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, page)
}

// homeStatusHTML 构建首页"运行状态"区块。
//
// 分类清单仅在 Debug 模式展示（与提示页策略一致）。
func (h *APIHandler) homeStatusHTML(r *http.Request) string {
	version := html.EscapeString(config.C.Version)

	checks := h.svc.Health(r.Context())
	snap := h.stats.Snapshot()

	var b strings.Builder
	fmt.Fprintf(&b, `<div class="status-line"><span class="dot"></span>服务运行中`+
		`<span class="ver">v%s</span></div>`, version)
	fmt.Fprintf(&b, `<p class="sub">已运行 %s</p>`,
		formatDuration(time.Duration(snap.UptimeSeconds)*time.Second))

	b.WriteString(`<table class="status-table"><tbody>`)
	// external 渠道未被请求过时仓库未初始化，退而展示 external_pool 配置状态
	extStatus := checks["external"]
	if extStatus == "" {
		extStatus = checks["external_pool"]
	}
	if extStatus == "" {
		extStatus = "未初始化"
	}
	for _, row := range []struct{ name, status string }{
		{"txt", checks["txt"]},
		{"local", checks["local"]},
		{"external", extStatus},
	} {
		fmt.Fprintf(&b, `<tr><td>%s</td><td>%s</td></tr>`, row.name, statusBadge(row.status))
	}
	b.WriteString(`</tbody></table>`)

	b.WriteString(`<div class="stat-grid">`)
	fmt.Fprintf(&b, `<div class="stat-card"><div class="num">%d</div><div class="label">总请求</div></div>`, snap.TotalRequests)
	fmt.Fprintf(&b, `<div class="stat-card"><div class="num">%d</div><div class="label">成功</div></div>`, snap.TotalSuccess)
	fmt.Fprintf(&b, `<div class="stat-card"><div class="num">%d</div><div class="label">失败</div></div>`, snap.TotalFail)
	fmt.Fprintf(&b, `<div class="stat-card"><div class="num">%d</div><div class="label">熔断</div></div>`, snap.CircuitTrips)
	b.WriteString(`</div>`)

	if config.C.Debug {
		if cats := h.svc.AvailableCategories(model.SourceTxt); len(cats) > 0 {
			escaped := make([]string, len(cats))
			for i, cat := range cats {
				escaped[i] = html.EscapeString(cat)
			}
			fmt.Fprintf(&b, `<p class="sub">TXT 分类：%s</p>`, strings.Join(escaped, "、"))
		}
	}

	return b.String()
}

// statusBadge 将仓库健康状态渲染为状态徽章（原始状态保留在 title 提示中）。
func statusBadge(status string) string {
	var cls, text string
	switch {
	case strings.HasPrefix(status, "healthy"):
		cls, text = "ok", "健康"
	case status == "":
		cls, text = "gray", "未初始化"
	case strings.Contains(status, "unconfigured") || strings.Contains(status, "disabled"):
		cls, text = "gray", "未配置"
	default:
		cls, text = "warn", "异常"
	}
	return fmt.Sprintf(`<span class="badge %s" title="%s">%s</span>`,
		cls, html.EscapeString(status), text)
}

// Random 处理 GET /random — 获取一张随机图片。
//
// 参数：
//
//	type     — auto（默认）/ pc / pe
//	source   — txt（默认）/ local / external
//	mode     — redirect（默认）/ json / image
//	category — 分类名，逗号分隔多选（默认 "default"）
//	api      — 外部 API 名称（source=external 时可选，空=随机选取）
func (h *APIHandler) Random(w http.ResponseWriter, r *http.Request) {
	h.stats.RecordRequest()

	params := parseParams(r)

	// 校验参数
	if !params.Source.Valid() {
		h.stats.RecordFail()
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"code":    400,
			"message": "invalid source, must be: txt / local / external",
		})
		return
	}
	if !params.Mode.Valid() {
		h.stats.RecordFail()
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"code":    400,
			"message": "invalid mode, must be: redirect / json / image",
		})
		return
	}
	if !params.Type.Valid() {
		h.stats.RecordFail()
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"code":    400,
			"message": "invalid type, must be: auto / pc / pe",
		})
		return
	}
	// local 图源返回的是文件系统路径，302 重定向后浏览器无法访问该路径，
	// 因此 local 源必须使用 mode=image（服务端直接输出）或 mode=json。
	if params.Source == model.SourceLocal && params.Mode == model.ModeRedirect {
		h.stats.RecordFail()
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"code":    400,
			"message": "local source does not support mode=redirect, use mode=image or mode=json",
		})
		return
	}
	// 分类名安全校验：拒绝路径分隔符与 ..（分类仅用于拼接文件路径/Redis key）
	if strings.ContainsAny(params.Category, `/\`) || strings.Contains(params.Category, "..") {
		h.stats.RecordFail()
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"code":    400,
			"message": "invalid category: must not contain path separators or '..'",
		})
		return
	}

	deviceType := device.Resolve(params.Type, r.Header.Get("User-Agent"))

	// 委托 Service 层获取随机图片
	ctx := r.Context()
	img, err := h.svc.Random(ctx, params.Source, params.APIName, params.Category, deviceType)
	if err != nil {
		h.stats.RecordFail()

		// 当前请求的图源未配置 → 综合"开始使用"引导页（介绍三种方式）
		if h.svc.SourceEmpty(params.Source) {
			h.renderSetupGuide(w, r, params.Mode)
			return
		}

		// 指定的外部 API 名称不存在（配置错误）→ "API 不存在"提示页
		var apiNotFound *model.ErrAPINotFound
		if errors.As(err, &apiNotFound) {
			// 仅 Debug 模式列出可用 API（避免向外部暴露配置清单）
			var available []string
			if config.C.Debug {
				available = h.svc.AvailableAPIs()
			}
			h.renderAPINotFound(w, r, apiNotFound.Name, available, params.Mode)
			return
		}

		// 图源有内容，但请求的分类不存在 → "分类不存在"提示页。
		// 分类在另一设备目录存在、当前设备目录缺失（如 pc 有、pe 没有）时，
		// 对该设备同样返回"分类不存在"提示，而不是笼统的 500。
		// 外部 API 渠道：分类不在白名单中（ErrCategoryNotSupported）同样返回此提示页。
		var categoryNotSupported *model.ErrCategoryNotSupported
		if errors.As(err, &categoryNotSupported) ||
			!h.svc.CategoryExists(params.Source, params.Category) ||
			!h.svc.CategoryExistsFor(params.Source, params.Category, deviceType) {
			// 仅 Debug 模式列出可用分类（避免向外部暴露图库分类清单）
			var available []string
			if config.C.Debug {
				available = h.svc.AvailableCategories(params.Source)
			}
			h.renderCategoryNotFound(w, r, params.Category, available, params.Mode)
			return
		}

		// 其它错误（如临时故障）→ 普通错误
		logger.L.Error("random failed",
			"request_id", middleware.RequestIDFrom(ctx),
			"source", params.Source,
			"category", params.Category,
			"device", deviceType,
			"error", err,
		)
		msg := "failed to get random image"
		if config.C.Debug {
			msg += ": " + err.Error()
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"code":    500,
			"message": msg,
		})
		return
	}

	h.stats.RecordSuccess()

	switch params.Mode {
	case model.ModeRedirect:
		http.Redirect(w, r, img.URL, http.StatusFound)
	case model.ModeJSON:
		writeJSON(w, http.StatusOK, model.RandomResponse{
			Code:    200,
			Message: "success",
			Data:    img,
		})
	case model.ModeImage:
		if params.Source == model.SourceLocal {
			// 本地文件：直接读取并输出（无需 HTTP 代理）
			h.serveLocalFile(w, r, img.URL)
		} else {
			h.proxyImage(w, r, img.URL)
		}
	default:
		http.Redirect(w, r, img.URL, http.StatusFound)
	}
}

// proxyImage 代理输出远程图片（mode=image）。
//
// 安全：仅允许 http/https、阻止内网 IP、校验 Content-Type、50MB 上限，
// SVG 响应附加 CSP sandbox，降低脚本在 API 域名下执行的风险。
func (h *APIHandler) proxyImage(w http.ResponseWriter, r *http.Request, imageURL string) {
	// SSRF 防护：只允许 http/https
	parsed, err := url.Parse(imageURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		writeJSON(w, http.StatusBadGateway, map[string]any{"code": 502, "message": "invalid image URL"})
		return
	}

	// SSRF 防护：IP 字面量直接拒绝。域名解析与内网拦截由 netx 拨号层统一完成
	// （SafeDialContext 会校验每个重定向跳转的目标 IP，比预检覆盖更全面），
	// 避免同一次代理请求重复解析 DNS。
	if ip := net.ParseIP(parsed.Hostname()); ip != nil && netx.IsBlockedIP(ip) {
		logger.L.Warn("ssrf blocked: private IP", "url", imageURL)
		writeJSON(w, http.StatusBadGateway, map[string]any{"code": 502, "message": "invalid image URL"})
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, imageURL, nil)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"code": 502, "message": "failed to create request"})
		return
	}

	resp, err := proxyClient.Do(req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"code": 502, "message": "failed to fetch image"})
		return
	}
	defer resp.Body.Close()

	// 仅允许图片类型，降低 XSS 风险（如 text/html）
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" || !strings.HasPrefix(contentType, "image/") {
		writeJSON(w, http.StatusBadGateway, map[string]any{"code": 502, "message": "upstream returned non-image content"})
		return
	}

	// 上游声明超限直接拒绝（CopyN 仍保留 50MB 硬上限兜底）
	if resp.ContentLength > 50<<20 {
		writeJSON(w, http.StatusBadGateway, map[string]any{"code": 502, "message": "upstream image too large (max 50MB)"})
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=300")
	// SVG 可内嵌脚本：沙箱化，降低直接访问时脚本在 API 域名下执行的风险（XSS）
	if strings.HasPrefix(contentType, "image/svg+xml") {
		w.Header().Set("Content-Security-Policy", "sandbox")
	}

	w.WriteHeader(http.StatusOK)
	// 限制最大 50MB，防 OOM。多读 1 字节探测超限：上游图超过 50MB 时直接断开连接，
	// 避免客户端把截断的半张图误当作完整图片（Content-Length 声明超限已在前面拒绝）。
	written, _ := io.CopyN(w, resp.Body, 50<<20+1)
	if written > 50<<20 {
		logger.L.Warn("upstream image exceeded 50MB limit, closing connection", "url", imageURL)
		// NewResponseController 会透传中间件包装（statusRecorder.Unwrap），直达底层 Hijacker
		if conn, _, hijackErr := http.NewResponseController(w).Hijack(); hijackErr == nil {
			conn.Close()
		}
		return
	}
}

// serveLocalFile 直接输出本地文件（source=local + mode=image）。
// 安全：防路径穿越、拒绝符号链接、仅允许图片扩展名、50MB 上限，
// SVG 响应附加 CSP sandbox，降低脚本在 API 域名下执行的风险。
func (h *APIHandler) serveLocalFile(w http.ResponseWriter, r *http.Request, filePath string) {
	cleanPath := filepath.Clean(filePath)

	// 安全检查 0：文件必须位于项目根目录内（防绝对路径越界）
	if rel, relErr := filepath.Rel(h.rootPath, cleanPath); relErr != nil ||
		rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		logger.L.Warn("local file outside project root blocked", "path", filePath)
		writeJSON(w, http.StatusForbidden, map[string]any{"code": 403, "message": "access denied"})
		return
	}

	// 安全检查 1：防路径穿越。按路径分隔符切分后逐段检查，
	// 避免子串匹配误伤合法文件名（如 "photo..jpg"）。
	for _, seg := range strings.FieldsFunc(cleanPath, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			logger.L.Warn("path traversal attempt blocked", "path", filePath)
			writeJSON(w, http.StatusForbidden, map[string]any{"code": 403, "message": "access denied"})
			return
		}
	}

	// 安全检查 2：仅允许图片扩展名
	ext := strings.ToLower(filepath.Ext(cleanPath))
	if !model.ImageExts[ext] {
		writeJSON(w, http.StatusBadGateway, map[string]any{"code": 502, "message": "unsupported local file type"})
		return
	}

	// 安全检查 3：拒绝符号链接（防止链接指向图库目录外的文件）。
	// 从文件本身逐级向上校验到项目根目录：任何一级父目录是符号链接都可能越界。
	for dir := cleanPath; ; dir = filepath.Dir(dir) {
		if fi, err := os.Lstat(dir); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			logger.L.Warn("symlink blocked", "path", dir)
			writeJSON(w, http.StatusForbidden, map[string]any{"code": 403, "message": "access denied"})
			return
		}
		if dir == h.rootPath {
			break
		}
		if parent := filepath.Dir(dir); parent == dir {
			break // 已到卷根（越界路径已被安全检查 0 拦截）
		}
	}

	f, err := os.Open(cleanPath)
	if err != nil {
		logger.L.Warn("local file open failed", "path", cleanPath, "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]any{"code": 502, "message": "local file not found"})
		return
	}
	defer f.Close()

	// 超限文件直接拒绝（CopyN 仍保留 50MB 硬上限兜底）
	if fi, err := f.Stat(); err == nil && fi.Size() > 50<<20 {
		writeJSON(w, http.StatusBadGateway, map[string]any{"code": 502, "message": "local image too large (max 50MB)"})
		return
	}

	// 根据扩展名推断标准 Content-Type（如 .jpg → image/jpeg）
	contentType := model.ImageMimeTypes[ext]
	if contentType == "" {
		writeJSON(w, http.StatusBadGateway, map[string]any{"code": 502, "message": "unsupported local file type"})
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=300")
	// SVG 可内嵌脚本：沙箱化，降低直接访问时脚本在 API 域名下执行的风险（XSS）
	if ext == ".svg" {
		w.Header().Set("Content-Security-Policy", "sandbox")
	}

	w.WriteHeader(http.StatusOK)
	_, _ = io.CopyN(w, f, 50<<20)
}

// renderSetupGuide 返回"开始使用"引导页。
//
// 当请求的图源（txt / local / external）未配置任何内容时触发，
// 面向首次部署的站长/用户，展示三种方式如何添加图片。
//
// 响应格式按客户端期望协商：
//   - mode=json         → JSON
//   - 图片请求（Accept 含 image/ 或 mode=image）→ SVG 占位图（博客 <img> 嵌入也能看到提示）
//   - 其它              → HTML 引导页
func (h *APIHandler) renderSetupGuide(w http.ResponseWriter, r *http.Request, mode model.Mode) {
	if mode == model.ModeJSON {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"code":    503,
			"message": "img-api is not configured yet",
			"hint":    "add images via one of the three sources: txt, local, or external (see docs/CONFIG.md)",
		})
		return
	}

	if mode == model.ModeImage || acceptsImage(r) {
		h.writePlaceholderSVG(w, r, "图片源未配置", []string{
			"img-api 服务已运行，但当前图源还没有图片",
			"请联系站长配置图源后重试",
		})
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = io.WriteString(w, setupGuidePage)
}

// renderCategoryNotFound 返回"分类不存在"的提示页。
//
// 图源有内容，但请求的 category 参数在渠道中不存在时触发。
// available 仅在 Debug 模式由调用方传入（生产环境不暴露分类清单）。
func (h *APIHandler) renderCategoryNotFound(w http.ResponseWriter, r *http.Request, category string, available []string, mode model.Mode) {
	if mode == model.ModeJSON {
		resp := map[string]any{
			"code":    404,
			"message": "category not found: " + category,
		}
		if len(available) > 0 {
			resp["available"] = available
		}
		writeJSON(w, http.StatusNotFound, resp)
		return
	}

	if mode == model.ModeImage || acceptsImage(r) {
		lines := []string{fmt.Sprintf("你请求的分类 %q 不存在", category)}
		if len(available) > 0 {
			lines = append(lines, "可用分类："+strings.Join(available, "、"))
		} else {
			lines = append(lines, "请联系站长获取正确的分类名")
		}
		h.writePlaceholderSVG(w, r, "分类不存在", lines)
		return
	}

	// 可用分类列表：仅 Debug 模式传入，否则提示联系站长
	var listHTML strings.Builder
	if len(available) == 0 {
		listHTML.WriteString("请参考文档或联系站长获取")
	} else {
		for i, cat := range available {
			if i > 0 {
				listHTML.WriteString("、")
			}
			fmt.Fprintf(&listHTML, "<code>%s</code>", html.EscapeString(cat))
		}
	}

	page := strings.ReplaceAll(categoryNotFoundPage, "{{CATEGORY}}", html.EscapeString(category))
	page = strings.ReplaceAll(page, "{{AVAILABLE}}", listHTML.String())

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNotFound)
	_, _ = io.WriteString(w, page)
}

// renderAPINotFound 返回"指定的外部 API 不存在"提示页。
//
// 触发条件：source=external 且 api 参数指定的名称在 image.yaml 池中不存在。
// 属于配置错误而非上游故障，因此返回 404 提示而非 500。
// available 仅在 Debug 模式由调用方传入（生产环境不暴露 API 清单）。
func (h *APIHandler) renderAPINotFound(w http.ResponseWriter, r *http.Request, apiName string, available []string, mode model.Mode) {
	if mode == model.ModeJSON {
		resp := map[string]any{
			"code":    404,
			"message": "external api not found: " + apiName,
		}
		if len(available) > 0 {
			resp["available"] = available
		}
		writeJSON(w, http.StatusNotFound, resp)
		return
	}

	if mode == model.ModeImage || acceptsImage(r) {
		lines := []string{fmt.Sprintf("你指定的 API %q 不存在", apiName)}
		if len(available) > 0 {
			lines = append(lines, "可用 API："+strings.Join(available, "、"))
		} else {
			lines = append(lines, "请检查 config/image.yaml 中的 name 字段")
		}
		h.writePlaceholderSVG(w, r, "API 不存在", lines)
		return
	}

	// 可用 API 列表：仅 Debug 模式传入，否则提示检查配置文件
	var listHTML strings.Builder
	if len(available) == 0 {
		listHTML.WriteString("请检查 config/image.yaml 中的 name 字段")
	} else {
		for i, name := range available {
			if i > 0 {
				listHTML.WriteString("、")
			}
			fmt.Fprintf(&listHTML, "<code>%s</code>", html.EscapeString(name))
		}
	}

	page := strings.ReplaceAll(apiNotFoundPage, "{{API}}", html.EscapeString(apiName))
	page = strings.ReplaceAll(page, "{{AVAILABLE}}", listHTML.String())

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNotFound)
	_, _ = io.WriteString(w, page)
}

// acceptsImage 判断客户端是否期望图片响应（如浏览器 <img> 标签嵌入场景）。
//
// 判据：Accept 以 "image/" 开头。不能只用 Contains("image/")：现代浏览器
// 地址栏导航的 Accept 也含 image/avif、image/webp 等条目（以 text/html 开头），
// 用 Contains 会把导航请求误判为图片请求，导致 / 首页与错误提示页返回 SVG 而非 HTML。
func acceptsImage(r *http.Request) bool {
	return strings.HasPrefix(strings.TrimSpace(r.Header.Get("Accept")), "image/")
}

// writePlaceholderSVG 输出一张内嵌文字的 SVG 占位图。
//
// 用于 <img> 标签嵌入 API URL 的场景：HTML 提示页无法在 <img> 中显示，
// 而 SVG 是图片格式，文字提示可直接渲染，博主/访客都能看到"未配置"提示。
//
// ⚠️ 状态码固定为 200：浏览器对 <img> 标签的非 2xx 响应不渲染 body（显示裂图），
// 因此虽然语义上是"未配置"，也必须用 200 才能让提示图正常显示。
func (h *APIHandler) writePlaceholderSVG(w http.ResponseWriter, r *http.Request, title string, lines []string) {
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="800" height="400" viewBox="0 0 800 400">`)
	b.WriteString(`<rect width="800" height="400" fill="#f6f7f9"/>`)
	b.WriteString(`<rect x="60" y="50" width="680" height="300" rx="12" fill="#ffffff" stroke="#e4e7eb"/>`)

	// 标题（大字）
	fmt.Fprintf(&b, `<text x="400" y="130" text-anchor="middle" font-family="sans-serif" font-size="30" font-weight="bold" fill="#333">%s</text>`,
		html.EscapeString(title))

	// 正文（逐行）
	y := 180
	for _, line := range lines {
		fmt.Fprintf(&b, `<text x="400" y="%d" text-anchor="middle" font-family="sans-serif" font-size="16" fill="#666">%s</text>`,
			y, html.EscapeString(line))
		y += 30
	}

	b.WriteString(`</svg>`)

	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, b.String())
}

// parseParams 从 HTTP query string 中提取并组装 RandomParams。
// 缺失参数使用 DefaultRandomParams() 的默认值。
func parseParams(r *http.Request) model.RandomParams {
	params := model.DefaultRandomParams()
	q := r.URL.Query()

	if v := q.Get("type"); v != "" {
		params.Type = model.DeviceType(strings.ToLower(strings.TrimSpace(v)))
	}
	if v := q.Get("source"); v != "" {
		params.Source = model.SourceType(strings.ToLower(strings.TrimSpace(v)))
	} else {
		// 使用配置的默认来源
		params.Source = model.SourceType(strings.ToLower(strings.TrimSpace(config.C.DefaultSource)))
	}
	if v := q.Get("mode"); v != "" {
		params.Mode = model.Mode(strings.ToLower(strings.TrimSpace(v)))
	}
	if v := q.Get("category"); v != "" {
		params.Category = v
	}
	if v := q.Get("api"); v != "" {
		params.APIName = v
	}

	return params
}
