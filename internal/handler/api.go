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
	"context"
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

	"github.com/gin-gonic/gin"

	"img-api/internal/config"
	"img-api/internal/device"
	"img-api/internal/logger"
	"img-api/internal/model"
	"img-api/internal/netx"
	"img-api/internal/service"
)

// proxyClient 用于 mode=image 的 HTTP 客户端。
// 安全：拨号前校验 IP（防 DNS rebinding）+ 10s 超时 + 连接池复用。
var proxyClient = netx.NewClient(10 * time.Second)

// imageMimeTypes 是本地图片扩展名 → 标准 MIME 类型的映射。
// 注意 .jpg 的标准 MIME 是 image/jpeg（"image/jpg" 并非规范值，
// 部分严格客户端会拒绝）。
var imageMimeTypes = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".gif":  "image/gif",
	".webp": "image/webp",
	".bmp":  "image/bmp",
	".svg":  "image/svg+xml",
}

// ApiHandler 处理图片相关的 HTTP 请求。
// 持有 Service 引用以获取随机图片，持有 Stats 引用以记录调用统计。
type ApiHandler struct {
	svc   *service.RandomService // 随机图片服务
	stats *service.Stats         // 请求统计（共享实例）
}

// NewApiHandler 创建 API 处理器。
func NewApiHandler(svc *service.RandomService, stats *service.Stats) *ApiHandler {
	return &ApiHandler{svc: svc, stats: stats}
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
//	token    — 鉴权密钥（Auth 中间件消费）
func (h *ApiHandler) Random(c *gin.Context) {
	h.stats.RecordRequest()

	params := parseParams(c)

	// 校验参数
	if !params.Source.Valid() {
		h.stats.RecordFail()
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "invalid source, must be: txt / local / external",
		})
		return
	}
	if !params.Mode.Valid() {
		h.stats.RecordFail()
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "invalid mode, must be: redirect / json / image",
		})
		return
	}
	if !params.Type.Valid() {
		h.stats.RecordFail()
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "invalid type, must be: auto / pc / pe",
		})
		return
	}
	// local 图源返回的是文件系统路径，302 重定向后浏览器无法访问该路径，
	// 因此 local 源必须使用 mode=image（服务端直接输出）或 mode=json。
	if params.Source == model.SourceLocal && params.Mode == model.ModeRedirect {
		h.stats.RecordFail()
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "local source does not support mode=redirect, use mode=image or mode=json",
		})
		return
	}
	// 分类名安全校验：拒绝路径分隔符与 ..（分类仅用于拼接文件路径/Redis key）
	if strings.ContainsAny(params.Category, `/\`) || strings.Contains(params.Category, "..") {
		h.stats.RecordFail()
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "invalid category: must not contain path separators or '..'",
		})
		return
	}

	deviceType := device.Resolve(params.Type, c.GetHeader("User-Agent"))

	// 委托 Service 层获取随机图片
	ctx := c.Request.Context()
	img, err := h.svc.Random(ctx, params.Source, params.ApiName, params.Category, deviceType)
	if err != nil {
		h.stats.RecordFail()

		// 当前请求的图源未配置 → 综合"开始使用"引导页（介绍三种方式）
		if h.svc.SourceEmpty(params.Source) {
			h.renderSetupGuide(c, params.Mode)
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
			h.renderAPINotFound(c, apiNotFound.Name, available, params.Mode)
			return
		}

		// 图源有内容，但请求的分类不存在 → "分类不存在"提示页。
		// 分类在另一设备目录存在、当前设备目录缺失（如 pc 有、pe 没有）时，
		// 对该设备同样返回"分类不存在"提示，而不是笼统的 500。
		if !h.svc.CategoryExists(params.Source, params.Category) ||
			!h.svc.CategoryExistsFor(params.Source, params.Category, deviceType) {
			// 仅 Debug 模式列出可用分类（避免向外部暴露图库分类清单）
			var available []string
			if config.C.Debug {
				available = h.svc.AvailableCategories(params.Source)
			}
			h.renderCategoryNotFound(c, params.Category, available, params.Mode)
			return
		}

		// 其它错误（如临时故障）→ 普通错误
		logger.L.Error("random failed",
			"request_id", c.GetString("request_id"),
			"source", params.Source,
			"category", params.Category,
			"device", deviceType,
			"error", err,
		)
		msg := "failed to get random image"
		if config.C.Debug {
			msg += ": " + err.Error()
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": msg,
		})
		return
	}

	h.stats.RecordSuccess()

	switch params.Mode {
	case model.ModeRedirect:
		c.Redirect(http.StatusFound, img.URL)
	case model.ModeJSON:
		c.JSON(http.StatusOK, model.RandomResponse{
			Code:    200,
			Message: "success",
			Data:    img,
		})
	case model.ModeImage:
		if params.Source == model.SourceLocal {
			// 本地文件：直接读取并输出（无需 HTTP 代理）
			h.serveLocalFile(c, img.URL)
		} else {
			h.proxyImage(c, img.URL)
		}
	default:
		c.Redirect(http.StatusFound, img.URL)
	}
}

// proxyImage 代理输出远程图片（mode=image）。
//
// 安全：仅允许 http/https、阻止内网 IP、校验 Content-Type、50MB 上限，
// SVG 响应附加 CSP sandbox，防止脚本在 API 域名下执行。
func (h *ApiHandler) proxyImage(c *gin.Context, imageURL string) {
	// SSRF 防护：只允许 http/https
	parsed, err := url.Parse(imageURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "message": "invalid image URL"})
		return
	}

	// SSRF 防护：阻止访问内网地址
	if isPrivateHost(c.Request.Context(), parsed.Hostname()) {
		logger.L.Warn("ssrf blocked: private IP", "url", imageURL)
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "message": "invalid image URL"})
		return
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, imageURL, nil)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "message": "failed to create request"})
		return
	}

	resp, err := proxyClient.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "message": "failed to fetch image"})
		return
	}
	defer resp.Body.Close()

	// 仅允许图片类型，防止 XSS（如 text/html）
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" || !strings.HasPrefix(contentType, "image/") {
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "message": "upstream returned non-image content"})
		return
	}

	// 上游声明超限直接拒绝（CopyN 仍保留 50MB 硬上限兜底）
	if resp.ContentLength > 50<<20 {
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "message": "upstream image too large (max 50MB)"})
		return
	}

	c.Header("Content-Type", contentType)
	c.Header("Cache-Control", "public, max-age=300")
	// SVG 可内嵌脚本：沙箱化，防止直接访问时脚本在 API 域名下执行（XSS）
	if strings.HasPrefix(contentType, "image/svg+xml") {
		c.Header("Content-Security-Policy", "sandbox")
	}

	c.Status(http.StatusOK)
	// 限制最大 50MB，防 OOM
	_, _ = io.CopyN(c.Writer, resp.Body, 50<<20)
}

// serveLocalFile 直接输出本地文件（source=local + mode=image）。
// 安全：防路径穿越、拒绝符号链接、仅允许图片扩展名、50MB 上限，
// SVG 响应附加 CSP sandbox，防止脚本在 API 域名下执行。
func (h *ApiHandler) serveLocalFile(c *gin.Context, filePath string) {
	cleanPath := filepath.Clean(filePath)

	// 安全检查 1：防路径穿越。按路径分隔符切分后逐段检查，
	// 避免子串匹配误伤合法文件名（如 "photo..jpg"）。
	for _, seg := range strings.FieldsFunc(cleanPath, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			logger.L.Warn("path traversal attempt blocked", "path", filePath)
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "access denied"})
			return
		}
	}

	// 安全检查 2：仅允许图片扩展名
	ext := strings.ToLower(filepath.Ext(cleanPath))
	if !model.ImageExts[ext] {
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "message": "unsupported local file type"})
		return
	}

	// 安全检查 3：拒绝符号链接（防止链接指向图库目录外的文件）
	if fi, err := os.Lstat(cleanPath); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		logger.L.Warn("symlink blocked", "path", cleanPath)
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "access denied"})
		return
	}

	f, err := os.Open(cleanPath)
	if err != nil {
		logger.L.Warn("local file open failed", "path", cleanPath, "error", err)
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "message": "local file not found"})
		return
	}
	defer f.Close()

	// 超限文件直接拒绝（CopyN 仍保留 50MB 硬上限兜底）
	if fi, err := f.Stat(); err == nil && fi.Size() > 50<<20 {
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "message": "local image too large (max 50MB)"})
		return
	}

	// 根据扩展名推断标准 Content-Type（如 .jpg → image/jpeg）
	contentType := imageMimeTypes[ext]
	if contentType == "" {
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "message": "unsupported local file type"})
		return
	}
	c.Header("Content-Type", contentType)
	c.Header("Cache-Control", "public, max-age=300")
	// SVG 可内嵌脚本：沙箱化，防止直接访问时脚本在 API 域名下执行（XSS）
	if ext == ".svg" {
		c.Header("Content-Security-Policy", "sandbox")
	}

	c.Status(http.StatusOK)
	_, _ = io.CopyN(c.Writer, f, 50<<20)
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
func (h *ApiHandler) renderSetupGuide(c *gin.Context, mode model.Mode) {
	if mode == model.ModeJSON {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"code":    503,
			"message": "img-api is not configured yet",
			"hint":    "add images via one of the three sources: txt, local, or external (see docs/CONFIG.md)",
		})
		return
	}

	if mode == model.ModeImage || acceptsImage(c) {
		h.writePlaceholderSVG(c, "图片源未配置", []string{
			"img-api 服务已运行，但当前图源还没有图片",
			"请联系站长配置图源后重试",
		})
		return
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusServiceUnavailable)
	_, _ = c.Writer.WriteString(setupGuidePage)
}

// renderCategoryNotFound 返回"分类不存在"的提示页。
//
// 图源有内容，但请求的 category 参数在渠道中不存在时触发。
// available 仅在 Debug 模式由调用方传入（生产环境不暴露分类清单）。
func (h *ApiHandler) renderCategoryNotFound(c *gin.Context, category string, available []string, mode model.Mode) {
	if mode == model.ModeJSON {
		resp := gin.H{
			"code":    404,
			"message": "category not found: " + category,
		}
		if len(available) > 0 {
			resp["available"] = available
		}
		c.JSON(http.StatusNotFound, resp)
		return
	}

	if mode == model.ModeImage || acceptsImage(c) {
		lines := []string{fmt.Sprintf("你请求的分类 %q 不存在", category)}
		if len(available) > 0 {
			lines = append(lines, "可用分类："+strings.Join(available, "、"))
		} else {
			lines = append(lines, "请联系站长获取正确的分类名")
		}
		h.writePlaceholderSVG(c, "分类不存在", lines)
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

	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusNotFound)
	_, _ = c.Writer.WriteString(page)
}

// renderAPINotFound 返回"指定的外部 API 不存在"提示页。
//
// 触发条件：source=external 且 api 参数指定的名称在 image.yaml 池中不存在。
// 属于配置错误而非上游故障，因此返回 404 提示而非 500。
// available 仅在 Debug 模式由调用方传入（生产环境不暴露 API 清单）。
func (h *ApiHandler) renderAPINotFound(c *gin.Context, apiName string, available []string, mode model.Mode) {
	if mode == model.ModeJSON {
		resp := gin.H{
			"code":    404,
			"message": "external api not found: " + apiName,
		}
		if len(available) > 0 {
			resp["available"] = available
		}
		c.JSON(http.StatusNotFound, resp)
		return
	}

	if mode == model.ModeImage || acceptsImage(c) {
		lines := []string{fmt.Sprintf("你指定的 API %q 不存在", apiName)}
		if len(available) > 0 {
			lines = append(lines, "可用 API："+strings.Join(available, "、"))
		} else {
			lines = append(lines, "请检查 configs/image.yaml 中的 name 字段")
		}
		h.writePlaceholderSVG(c, "API 不存在", lines)
		return
	}

	// 可用 API 列表：仅 Debug 模式传入，否则提示检查配置文件
	var listHTML strings.Builder
	if len(available) == 0 {
		listHTML.WriteString("请检查 configs/image.yaml 中的 name 字段")
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

	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusNotFound)
	_, _ = c.Writer.WriteString(page)
}

// acceptsImage 判断客户端是否期望图片响应（如浏览器 <img> 标签嵌入场景）。
// 浏览器加载 <img src="..."> 时 Accept 头形如 "image/avif,image/webp,...,*/*;q=0.8"。
func acceptsImage(c *gin.Context) bool {
	accept := c.GetHeader("Accept")
	return strings.Contains(accept, "image/")
}

// writePlaceholderSVG 输出一张内嵌文字的 SVG 占位图。
//
// 用于 <img> 标签嵌入 API URL 的场景：HTML 提示页无法在 <img> 中显示，
// 而 SVG 是图片格式，文字提示可直接渲染，博主/访客都能看到"未配置"提示。
//
// ⚠️ 状态码固定为 200：浏览器对 <img> 标签的非 2xx 响应不渲染 body（显示裂图），
// 因此虽然语义上是"未配置"，也必须用 200 才能让提示图正常显示。
func (h *ApiHandler) writePlaceholderSVG(c *gin.Context, title string, lines []string) {
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

	c.Header("Content-Type", "image/svg+xml; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusOK)
	_, _ = c.Writer.WriteString(b.String())
}

// parseParams 从 HTTP query string 中提取并组装 RandomParams。
// 缺失参数使用 DefaultRandomParams() 的默认值。
func parseParams(c *gin.Context) model.RandomParams {
	params := model.DefaultRandomParams()

	if v := c.Query("type"); v != "" {
		params.Type = model.DeviceType(v)
	}
	if v := c.Query("source"); v != "" {
		params.Source = model.SourceType(v)
	} else {
		// 使用配置的默认来源
		params.Source = model.SourceType(config.C.DefaultSource)
	}
	if v := c.Query("mode"); v != "" {
		params.Mode = model.Mode(v)
	}
	if v := c.Query("category"); v != "" {
		params.Category = v
	}
	if v := c.Query("api"); v != "" {
		params.ApiName = v
	}

	return params
}

// isPrivateHost 检查主机名是否解析为禁止访问的地址（防 SSRF）。
// 任一解析结果为内网/保留地址即拒绝（与 netx.SafeDialContext 规则一致）。
// DNS 解析带 ctx 超时，避免异常 DNS 卡住 handler goroutine。
func isPrivateHost(ctx context.Context, host string) bool {
	// 先尝试直接解析 IP（如 127.0.0.1、10.0.0.1、[::1]）
	if ip := net.ParseIP(host); ip != nil {
		return netx.IsBlockedIP(ip)
	}

	// 不是 IP 字面量，尝试 DNS 解析（所有结果都必须合法）
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(ips) == 0 {
		return true // 解析失败，保守拒绝
	}
	for _, ip := range ips {
		if netx.IsBlockedIP(ip.IP) {
			return true
		}
	}
	return false
}
