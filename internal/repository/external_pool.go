package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/viper"

	"img-api/internal/logger"
	"img-api/internal/model"
	"img-api/internal/netx"
)

// ExternalAPIConfig 描述一个外部图片 API 端点（从 image.yaml 反序列化）。
type ExternalAPIConfig struct {
	Name            string            `mapstructure:"name"`             // API 标识
	URL             string            `mapstructure:"url"`              // 请求模板（{width}/{height}/{category} 占位符）
	Headers         map[string]string `mapstructure:"headers"`          // 自定义请求头
	ResponseType    string            `mapstructure:"response_type"`    // redirect / json
	URLField        string            `mapstructure:"url_field"`        // JSON 中 URL 字段路径
	Categories      []string          `mapstructure:"categories"`       // 支持的分类（空=匹配所有，["all"]=匹配所有）
	CategoryParam   string            `mapstructure:"category_param"`   // 分类对应的 query 参数名（如 "query"）
	DefaultCategory []string          `mapstructure:"default_category"` // 默认分类（多值随机选一；空=回退 "default"）
}

// ExternalPool 管理一组外部图片 API 端点。
//
// 每次 Random 调用随机选择一个 API，实现简单的负载分散。
// 不为单个 API 做故障转移（失败由熔断器统一处理）。
type ExternalPool struct {
	apis   []ExternalAPIConfig // 从 image.yaml 加载的 API 列表
	client *http.Client        // 共享的 HTTP 客户端（10s 超时）
}

// APIs 返回池中所有端点（用于 /health 展示数量）。
func (p *ExternalPool) APIs() []ExternalAPIConfig {
	return p.apis
}

// FindByName 按名称查找 API 配置（大小写不敏感）。
// 返回配置指针和是否找到。
func (p *ExternalPool) FindByName(name string) (*ExternalAPIConfig, bool) {
	for i := range p.apis {
		if strings.EqualFold(p.apis[i].Name, name) {
			return &p.apis[i], true
		}
	}
	return nil, false
}

// LoadExternalPool 从 Viper 配置中解析 image.yaml。
//
// 如果没有配置任何外部 API，返回一个空池（不报错）。
// 此时 source=external 的请求会得到 ErrExternalNotConfigured，
// 由 Handler 层返回"开始使用"引导页。
func LoadExternalPool(v *viper.Viper) (*ExternalPool, error) {
	var config struct {
		ExternalAPIs []ExternalAPIConfig `mapstructure:"external_apis"`
	}

	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("parse image.yaml: %w", err)
	}

	if len(config.ExternalAPIs) == 0 {
		// 返回空池：source=external 请求将返回"开始使用"引导页
		logger.L.Warn("no external APIs configured, source=external will return a setup hint page")
		return &ExternalPool{
			apis:   []ExternalAPIConfig{},
			client: netx.NewClient(10 * time.Second),
		}, nil
	}

	logger.L.Info("external API pool loaded", "count", len(config.ExternalAPIs))
	return &ExternalPool{
		apis:   config.ExternalAPIs,
		client: netx.NewClient(10 * time.Second),
	}, nil
}

// Random 从外部 API 池获取图片。
//
// apiName 为空时从所有 API 中随机选取；非空时按名称精确匹配（大小写不敏感）。
// category 为空时使用 API 的 default_category（多值随机选一），仍为空则回退 "default"。
// deviceType 决定 {width}/{height} 占位符：PC=800x600, PE=400x800。
func (p *ExternalPool) Random(ctx context.Context, apiName, category string, deviceType model.DeviceType) (*model.Image, error) {
	if len(p.apis) == 0 {
		return nil, model.ErrExternalNotConfigured
	}

	// ── 第 1 步：确定目标 API ──
	var api *ExternalAPIConfig

	if apiName != "" {
		// 指定 API 名称 → 精确查找
		found, ok := p.FindByName(apiName)
		if !ok {
			return nil, &model.ErrAPINotFound{Name: apiName}
		}
		api = found
		// 校验分类：如果用户指定了 category 且 API 有白名单，检查是否匹配
		if category != "" && category != "default" && len(api.Categories) > 0 && !p.apiMatchesCategory(*api, category) {
			return nil, fmt.Errorf("external api %s does not support category %s", api.Name, category)
		}
	} else {
		// 未指定 → 按分类筛选后随机选
		candidates := p.filterByCategory(category)
		if len(candidates) == 0 {
			return nil, &model.ErrNoImage{Source: "external", Category: category}
		}
		api = &candidates[rand.Intn(len(candidates))]
	}

	// ── 第 2 步：确定实际使用的分类 ──
	effectiveCategory := category
	if effectiveCategory == "" || effectiveCategory == "default" {
		effectiveCategory = p.pickDefaultCategory(api)
	}

	// ── 第 3 步：构建 URL ──
	width, height := "800", "600"
	widthI, heightI := 800, 600
	if deviceType == model.DevicePE {
		width, height = "400", "800"
		widthI, heightI = 400, 800
	}
	// 注意：局部变量命名避开 url，否则会遮蔽 net/url 包（url.PathEscape 等无法使用）
	reqURL := strings.ReplaceAll(api.URL, "{width}", width)
	reqURL = strings.ReplaceAll(reqURL, "{height}", height)
	// 分类可能含中文/空格等字符，需按路径片段转义后替换占位符
	reqURL = strings.ReplaceAll(reqURL, "{category}", url.PathEscape(effectiveCategory))

	// 如果配置了 category_param，追加 query 参数
	if api.CategoryParam != "" && effectiveCategory != "" && effectiveCategory != "default" {
		if strings.Contains(reqURL, "?") {
			reqURL += "&"
		} else {
			reqURL += "?"
		}
		reqURL += api.CategoryParam + "=" + url.QueryEscape(effectiveCategory)
	}

	logger.L.Debug("external pool pick",
		"api", api.Name,
		"url", reqURL,
		"device", deviceType,
		"category", effectiveCategory,
		"requested_category", category,
	)

	// 根据响应类型处理
	switch api.ResponseType {
	case "json":
		return p.fetchJSON(ctx, *api, reqURL, widthI, heightI)
	default:
		return p.fetchRedirect(ctx, reqURL, widthI, heightI)
	}
}

// pickDefaultCategory 从 API 的默认分类列表中随机选一个。
// 未配置时回退到 "default"。
func (p *ExternalPool) pickDefaultCategory(api *ExternalAPIConfig) string {
	if len(api.DefaultCategory) == 0 {
		return "default"
	}
	if len(api.DefaultCategory) == 1 {
		return api.DefaultCategory[0]
	}
	return api.DefaultCategory[rand.Intn(len(api.DefaultCategory))]
}

// filterByCategory 从 API 池中筛选支持指定分类的端点。
//
// 筛选规则：
//   - category 为空或 "default" → 返回全部 API（不做筛选）
//   - API 的 categories 为空或包含 "all" → 匹配所有分类
//   - 否则：API 的 categories 列表中必须包含请求的 category
func (p *ExternalPool) filterByCategory(category string) []ExternalAPIConfig {
	if category == "" || category == "default" {
		return p.apis
	}

	var result []ExternalAPIConfig
	for _, api := range p.apis {
		if p.apiMatchesCategory(api, category) {
			result = append(result, api)
		}
	}
	return result
}

// apiMatchesCategory 判断单个 API 是否支持指定分类。
func (p *ExternalPool) apiMatchesCategory(api ExternalAPIConfig, category string) bool {
	// 未声明 categories → 匹配所有
	if len(api.Categories) == 0 {
		return true
	}
	for _, c := range api.Categories {
		c = strings.TrimSpace(c)
		if strings.EqualFold(c, "all") || strings.EqualFold(c, category) {
			return true
		}
	}
	return false
}

// fetchRedirect 获取重定向后的最终图片 URL。
// 优先用 HEAD（省流量）；部分图床不支持 HEAD（返回 405 等），
// 失败时自动降级为 GET 重试一次（不读取响应体）。
func (p *ExternalPool) fetchRedirect(ctx context.Context, url string, width, height int) (*model.Image, error) {
	finalURL, err := p.finalURL(ctx, http.MethodHead, url)
	if err != nil {
		finalURL, err = p.finalURL(ctx, http.MethodGet, url)
	}
	if err != nil {
		return nil, err
	}

	return &model.Image{
		URL:      finalURL,
		Category: "external",
		Width:    width,
		Height:   height,
	}, nil
}

// finalURL 发起请求并返回重定向后的最终 URL（不读取响应体）。
func (p *ExternalPool) finalURL(ctx context.Context, method, url string) (string, error) {
	// NewRequestWithContext 第 4 个参数为请求体（GET/HEAD 无 body，传 nil）
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return "", fmt.Errorf("external %s request: %w", method, err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("external %s: %w", method, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("external %s returned HTTP %d", method, resp.StatusCode)
	}

	return resp.Request.URL.String(), nil
}

// fetchJSON 发送 GET 请求并解析 JSON 响应体提取图片 URL。
// 支持自定义请求头（如 Unsplash 的 Authorization）。
// 响应体限制 10MB，防止 OOM。
func (p *ExternalPool) fetchJSON(ctx context.Context, api ExternalAPIConfig, url string, width, height int) (*model.Image, error) {
	// NewRequestWithContext 第 4 个参数为请求体（GET 无 body，传 nil）
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("external GET request: %w", err)
	}

	// 添加自定义请求头
	for k, v := range api.Headers {
		req.Header.Set(k, v)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("external GET: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("external GET returned HTTP %d", resp.StatusCode)
	}

	// 解析 JSON（限制 10MB 防 OOM）
	var raw map[string]any
	limited := io.LimitReader(resp.Body, 10<<20)
	if err := json.NewDecoder(limited).Decode(&raw); err != nil {
		return nil, fmt.Errorf("external JSON decode: %w", err)
	}

	// 按路径提取 URL 字段（如 "urls.raw" → raw["urls"]["raw"]）
	imageURL := extractNested(raw, api.URLField)
	if imageURL == "" {
		return nil, fmt.Errorf("external JSON: field %s not found", api.URLField)
	}

	return &model.Image{
		URL:      imageURL,
		Category: "external",
		Width:    width,
		Height:   height,
	}, nil
}

// extractNested 从嵌套 map 中按点号路径提取字符串值。
//
// 示例：
//   extractNested({"urls": {"raw": "https://..."}}, "urls.raw") → "https://..."
//   extractNested(data, "") → 自动探测常见字段名
func extractNested(data map[string]any, path string) string {
	if path == "" {
		// 尝试常见字段名
		for _, key := range []string{"url", "urls.raw", "urls.full", "download_url", "src"} {
			if val := extractNested(data, key); val != "" {
				return val
			}
		}
		return ""
	}

	parts := strings.SplitN(path, ".", 2)
	current, ok := data[parts[0]]
	if !ok {
		return ""
	}

	if len(parts) == 1 {
		// 最后一级，转字符串
		if s, ok := current.(string); ok {
			return s
		}
		return ""
	}

	// 继续递归
	if nested, ok := current.(map[string]any); ok {
		return extractNested(nested, parts[1])
	}

	return ""
}
