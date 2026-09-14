// Package middleware 提供标准库（net/http）风格的 HTTP 中间件。
//
// 中间件执行顺序（server.go 中包装的顺序，先包装的在最外层）：
//
//	Recovery → RequestID → AccessLog → RateLimiter → CORS → Referer → Handler
//
// 每个中间件职责单一，可独立启用/禁用。
package middleware

import (
	"net/http"

	"img-api/internal/httpx"
)

// Middleware 是标准库风格的 HTTP 中间件：
// 接收下一个处理器，返回包装后的处理器。
type Middleware func(http.Handler) http.Handler

// writeJSON 是共享实现（internal/httpx）的包内别名，
// 与 handler 层共用同一实现，保证响应契约一致。
var writeJSON = httpx.WriteJSON
