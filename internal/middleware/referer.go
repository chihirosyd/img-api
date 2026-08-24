package middleware

import (
	"net/http"
	"net/url"
	"strings"

	"img-api/internal/config"
	"img-api/internal/logger"
)

// Referer 防止其他网站直接引用本 API 的图片（防盗链）。
//
// 白名单为空时不启用。启用后：
//   - Referer 为空 → 放行（curl、浏览器地址栏直接访问）
//   - Referer 在白名单中 → 放行
//   - Referer 不在白名单中 → 返回 403
//
// 白名单配置在 REFERER_WHITELIST，逗号分隔（如 "mysite.com,blog.mysite.com"）。
func Referer() Middleware {
	whitelist := config.C.RefererWhitelist
	// 拦截日志的 IP 与访问日志/限流器同源：仅信任 TRUSTED_PROXIES 网段的转发头。
	trusted := parseTrustedProxies(config.C.TrustedProxies)

	// 白名单为空 → 不启用防盗链
	if len(whitelist) == 0 {
		return func(next http.Handler) http.Handler { return next }
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			referer := r.Header.Get("Referer")

			// 空 Referer 放行（如直接访问、curl 请求）
			if referer == "" || isAllowed(referer, whitelist) {
				next.ServeHTTP(w, r)
				return
			}

			logger.L.Warn("referer blocked",
				"referer", referer,
				"ip", realClientIP(r, trusted),
			)

			writeJSON(w, http.StatusForbidden, map[string]any{
				"code":    403,
				"message": "access denied: referer not allowed",
			})
		})
	}
}

// isAllowed 检查 Referer 主机名是否在白名单中。
//
// 匹配规则：
//   - 精确匹配：host == allowed
//   - 子域名匹配：host 以 "."+allowed 结尾，如 www.mysite.com 匹配 mysite.com
//   - 防绕过：evilmysite.com 不会误匹配 mysite.com
func isAllowed(referer string, whitelist []string) bool {
	host := referer
	if u, err := url.Parse(referer); err == nil && u.Host != "" {
		host = u.Hostname() // 不含端口
	}
	host = strings.ToLower(host)

	for _, allowed := range whitelist {
		allowed = strings.ToLower(strings.TrimSpace(allowed))
		if allowed == "" {
			continue
		}
		if host == allowed {
			return true
		}
		// 子域名匹配：www.mysite.com 匹配 .mysite.com
		// 只有 host 中确实包含 "."+allowed 且前面至少有一级域名才放行
		if strings.HasSuffix(host, "."+allowed) {
			return true
		}
	}
	return false
}
