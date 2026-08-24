package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

// ctxKey 是 request_id 的 context key 类型（私有类型防键冲突）。
type ctxKey struct{}

// CtxKeyRequestID 是请求 ID 的 context key，供 handler 层读取。
var CtxKeyRequestID ctxKey

// RequestID 为每个 HTTP 请求分配唯一追踪 ID。
//
// 如果客户端已传 X-Request-ID 头则复用（支持分布式追踪），
// 否则生成一个 16 位十六进制随机 ID（8 字节 64bit，高 QPS 下碰撞概率更低）。
// 客户端传入的值截断到 64 字符：该值会写入访问日志与响应头，
// 超长输入会造成日志膨胀。
//
// ID 存入请求 Context（RequestIDFrom 可读取），
// 并回写到响应头 X-Request-ID，方便客户端关联日志。
func RequestID() Middleware {
	const maxIDLen = 64
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rid := r.Header.Get("X-Request-ID")
			if rid == "" {
				b := make([]byte, 8)
				_, _ = rand.Read(b)
				rid = hex.EncodeToString(b) // 16 位短 ID
			} else if len(rid) > maxIDLen {
				// 按 rune 截断，避免按字节切断多字节 UTF-8 字符（产生非法字符序列）
				runes := []rune(rid)
				rid = string(runes[:maxIDLen])
			}
			w.Header().Set("X-Request-ID", rid)
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), CtxKeyRequestID, rid)))
		})
	}
}

// RequestIDFrom 从请求上下文中取回 request_id（未设置时返回空串）。
func RequestIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(CtxKeyRequestID).(string); ok {
		return v
	}
	return ""
}
