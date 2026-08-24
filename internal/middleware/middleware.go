// Package middleware 提供标准库（net/http）风格的 HTTP 中间件。
//
// 中间件执行顺序（server.go 中包装的顺序，先包装的在最外层）：
//
//	Recovery → RequestID → AccessLog → CORS → RateLimiter → Referer → Handler
//
// 每个中间件职责单一，可独立启用/禁用。
package middleware

import (
	"encoding/json"
	"net/http"
)

// Middleware 是标准库风格的 HTTP 中间件：
// 接收下一个处理器，返回包装后的处理器。
type Middleware func(http.Handler) http.Handler

// writeJSON 输出 JSON 响应（Content-Type 与状态码）。
func writeJSON(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(data)
}
