package middleware

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"img-api/internal/logger"
)

// AccessLog 记录每个请求的基础访问日志：方法、路径、状态码、耗时、IP、RequestID。
//
// 应注册在 RequestID 之后，这样日志携带的 request_id 与响应头一致，
// 便于排障时关联单次请求。
// 健康检查路径（/health、/health-{secret}）不写访问日志：
// Docker healthcheck 每 30s 探测一次，记录会造成日志噪声。
func AccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 健康检查探测不记录日志，直接放行
		if strings.HasPrefix(c.Request.URL.Path, "/health") {
			c.Next()
			return
		}

		start := time.Now()
		c.Next()

		logger.L.Info("access",
			"request_id", c.GetString("request_id"),
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"latency_ms", time.Since(start).Milliseconds(),
			"ip", c.ClientIP(),
		)
	}
}
