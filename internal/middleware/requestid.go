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
// 客户端传入的值截断到 64 字符：该值会写入访问日志与响应头，
// 超长输入会造成日志膨胀。
//
// ID 存入 Gin Context（c.GetString("request_id")），
// 并回写到响应头 X-Request-ID，方便客户端关联日志。
func RequestID() gin.HandlerFunc {
	const maxIDLen = 64
	return func(c *gin.Context) {
		rid := c.GetHeader("X-Request-ID")
		if rid == "" {
			b := make([]byte, 8)
			_, _ = rand.Read(b)
			rid = hex.EncodeToString(b) // 16 位短 ID
		} else if len(rid) > maxIDLen {
			// 按 rune 截断，避免按字节切断多字节 UTF-8 字符（产生非法字符序列）
			runes := []rune(rid)
			rid = string(runes[:maxIDLen])
		}
		c.Set("request_id", rid)
		c.Header("X-Request-ID", rid)
		c.Next()
	}
}
