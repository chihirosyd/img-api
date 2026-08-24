package middleware

import (
	"net/http"
	"runtime/debug"

	"img-api/internal/logger"
)

// Recovery 捕获处理链中的 panic，记录堆栈并返回 500，
// 避免单个请求的 panic 拖垮整个服务。
//
// 若响应已部分写出则只能断连（写 500 会失败，属可接受的边界情况）。
func Recovery() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.L.Error("panic recovered",
						"error", rec,
						"path", r.URL.Path,
						"stack", string(debug.Stack()),
					)
					http.Error(w, `{"code":500,"message":"internal server error"}`, http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
