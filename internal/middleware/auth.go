package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"img-api/internal/config"
	"img-api/internal/logger"
)

// extractBearer 从 Authorization 头提取 Token（大小写不敏感，自动 Trim）。
func extractBearer(auth string) string {
	if len(auth) < 7 {
		return ""
	}
	if strings.EqualFold(auth[:6], "Bearer") {
		return strings.TrimSpace(auth[6:])
	}
	return ""
}

// Auth 验证请求是否携带正确的鉴权 Token。
//
// 如果 AUTH_ENABLED=false（默认），此中间件直接放行所有请求。
// Token 查找优先级（Header 优先，URL 仅作兼容）：
//   1. 请求头 X-Token: xxx
//   2. 请求头 Authorization: Bearer xxx（大小写不敏感）
//   3. query 参数 ?token=xxx（不推荐：会出现在 URL 中，可能被反向代理日志记录）
//
// Token 不匹配返回 401。
func Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !config.C.AuthEnabled {
			c.Next()
			return
		}

		// 优先从 Header 读取
		token := c.GetHeader("X-Token")
		if token == "" {
			token = extractBearer(c.GetHeader("Authorization"))
		}
		// 兼容 URL 参数（不推荐，会泄露在日志中）
		if token == "" {
			token = c.Query("token")
		}

		// 常量时间比较，降低时序攻击风险
		if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(config.C.AuthToken)) != 1 {
			logger.L.Warn("auth failed", "ip", c.ClientIP())
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "invalid or missing token"})
			c.Abort()
			return
		}

		c.Next()
	}
}
