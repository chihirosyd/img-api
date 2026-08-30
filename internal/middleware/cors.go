package middleware

import (
	"net/http"

	"img-api/internal/config"
)

// CORS 处理跨域资源共享和安全响应头。
//
// 允许任意 Origin 的 GET 请求（图片 API 通常需要被各种网站引用）。
// 注意：<img> 标签嵌图不经过 CORS 校验，仅 JS 的 fetch/XHR 跨域请求需要这些头；
// 因此纯嵌图场景关闭 CORS_ENABLED 不影响使用。
// OPTIONS 预检请求直接返回 204，不进入后续中间件链。
//
// 当 CORS_ENABLED=false 时直接跳过跨域头（如前方已有 Nginx 处理 CORS），
// 但安全响应头仍会添加。
func CORS() Middleware {
	if !config.C.CorsEnabled {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				addSecurityHeaders(w)
				// CORS 关闭时也优雅处理预检请求，避免 OPTIONS 落到业务路由返回 404
				if r.Method == http.MethodOptions {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				next.ServeHTTP(w, r)
			})
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// CORS 头
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", "*")
			h.Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID")
			h.Set("Access-Control-Max-Age", "86400")

			// 安全响应头
			addSecurityHeaders(w)

			// 预检请求直接返回 204
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// addSecurityHeaders 添加安全相关的 HTTP 响应头。
func addSecurityHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
}
