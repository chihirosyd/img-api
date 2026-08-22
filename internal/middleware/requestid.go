package middleware

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/gin-gonic/gin"
)

// RequestID 为每个 HTTP 请求分配唯一追踪 ID。
//
// 如果客户端已传 X-Request-ID 头则复用（支持分布式追踪），
// 否则生成一个 16 位十六进制随机 ID（8 字节 64bit，高 QPS 下碰撞概率更低）。
//
// ID 存入 Gin Context（c.GetString("request_id")），
// 并回写到响应头 X-Request-ID，方便客户端关联日志。
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := c.GetHeader("X-Request-ID")
		if rid == "" {
			b := make([]byte, 8)
			_, _ = rand.Read(b)
			rid = hex.EncodeToString(b) // 16 位短 ID
		}
		c.Set("request_id", rid)
		c.Header("X-Request-ID", rid)
		c.Next()
	}
}
