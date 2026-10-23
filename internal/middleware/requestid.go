package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"unicode"
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
// 超长输入会造成日志膨胀。含控制字符（如 CR/LF）的非法值重新生成：
// 该值会回写响应头，控制字符可能破坏响应头结构（防御性清洗）。
//
// ID 存入请求 Context（RequestIDFrom 可读取），
// 并回写到响应头 X-Request-ID，方便客户端关联日志。
func RequestID() Middleware {
	const maxIDLen = 64
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rid := r.Header.Get("X-Request-ID")
			if rid == "" || hasControlRunes(rid) {
				b := make([]byte, 8)
				_, _ = rand.Read(b)
				rid = hex.EncodeToString(b) // 16 位短 ID
			} else if len(rid) > maxIDLen {
				// len() 是字节数，maxIDLen 是字符数：中文/emoji 等多字节字符会让
				// 字节数远大于字符数。按 rune 边界截断，既不会切断 UTF-8 字符，
				// 也不会在"字节超限、字符却不足 64"时越界（此前 runes[:64] 会 panic：
				// 22 个中文字符 = 66 字节 / 22 rune 即触发）。
				// 用下标遍历而非 []rune 转换：客户端可送近 1MB 的头部，
				// 转 []rune 会按 4 字节/字符放大内存，这里零额外分配。
				count, cut := 0, len(rid)
				for i := range rid {
					if count == maxIDLen {
						cut = i
						break
					}
					count++
				}
				rid = rid[:cut]
			}
			w.Header().Set("X-Request-ID", rid)
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), CtxKeyRequestID, rid)))
		})
	}
}

// hasControlRunes 判断字符串是否含控制字符（C0/C1 控制区）。
// 客户端 ID 会回写响应头，控制字符（尤其 CR/LF）会破坏响应头结构。
func hasControlRunes(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

// RequestIDFrom 从请求上下文中取回 request_id（未设置时返回空串）。
func RequestIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(CtxKeyRequestID).(string); ok {
		return v
	}
	return ""
}
