// Package middleware 提供 Gin 框架的 HTTP 中间件。
//
// 中间件执行顺序（main.go 中注册的顺序）：
//   Recovery → RequestID → AccessLog → CORS → RateLimiter → Referer → Handler
//
// Auth 不在全局链上：仅注册于图片接口分组（/random 与 /），
// /health 保持匿名可访问（Docker healthcheck、探针等场景）。
//
// 每个中间件职责单一，可独立启用/禁用。
package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"img-api/internal/config"
)

// CORS 处理跨域资源共享和安全响应头。
//
// 允许任意 Origin 的 GET 请求（图片 API 通常需要被各种网站引用）。
// OPTIONS 预检请求直接返回 204，不进入后续中间件链。
//
// 当 CORS_ENABLED=false 时直接跳过跨域头（如前方已有 Nginx 处理 CORS），
// 但安全响应头仍会添加。
//
// 安全提示：Token 鉴权（AUTH_ENABLED=true）开启时，
// Access-Control-Allow-Origin: * 与 credentials 不兼容，
// 建议在 Nginx 层根据 Origin 动态设置 Allow-Origin。
func CORS() gin.HandlerFunc {
	if !config.C.CorsEnabled {
		return func(c *gin.Context) {
			addSecurityHeaders(c)
			// CORS 关闭时也优雅处理预检请求，避免 OPTIONS 落到业务路由返回 404
			if c.Request.Method == http.MethodOptions {
				c.AbortWithStatus(http.StatusNoContent)
				return
			}
			c.Next()
		}
	}
	return func(c *gin.Context) {
		// CORS 头
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Token, X-Request-ID, X-Health-Secret")
		c.Header("Access-Control-Max-Age", "86400")

		// 安全响应头
		addSecurityHeaders(c)

		// 预检请求直接返回 204
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// addSecurityHeaders 添加安全相关的 HTTP 响应头。
func addSecurityHeaders(c *gin.Context) {
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("X-Frame-Options", "DENY")
	c.Header("Referrer-Policy", "strict-origin-when-cross-origin")

	// 当鉴权开启时，建议限制跨域来源（提示性头）
	if config.C.AuthEnabled {
		c.Header("Cross-Origin-Resource-Policy", "cross-origin")
	}
}
