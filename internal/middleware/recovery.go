package middleware

import (
	"net/http"
	"runtime/debug"

	"img-api/internal/logger"
)

// Recovery 捕获处理链中的 panic，记录堆栈并返回 500 JSON，
// 避免单个请求的 panic 拖垮整个服务。
//
// 边界情况：
//   - 响应已部分写出时无法再写 500（写入会失败），直接断开连接
//   - http.ErrAbortHandler（主动断连信号）原样重新抛出，交由 net/http 关闭连接
func Recovery() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rw := &recoveryWriter{ResponseWriter: w}
			defer func() {
				if rec := recover(); rec != nil {
					// 主动断连信号：重新抛出（panic 一个特殊值约定，见 net/http）
					if rec == http.ErrAbortHandler {
						panic(rec)
					}
					logger.L.Error("panic recovered",
						"error", rec,
						"path", r.URL.Path,
						"stack", string(debug.Stack()),
					)
					if rw.wroteHeader {
						// 响应已提交：无法再写 500，直接断开连接
						if conn, _, err := http.NewResponseController(rw.ResponseWriter).Hijack(); err == nil {
							_ = conn.Close()
						}
						return
					}
					writeJSON(w, http.StatusInternalServerError, map[string]any{
						"code":    500,
						"message": "internal server error",
					})
				}
			}()
			next.ServeHTTP(rw, r)
		})
	}
}

// recoveryWriter 记录响应是否已提交，供 panic 恢复时判断
// 还能否继续写 500（已提交则只能断连，避免拼接出两个 body）。
// Unwrap 使 http.NewResponseController 能透传底层 Flush/Hijack 能力。
type recoveryWriter struct {
	http.ResponseWriter
	wroteHeader bool
}

func (rw *recoveryWriter) WriteHeader(code int) {
	if rw.wroteHeader {
		return
	}
	rw.wroteHeader = true
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *recoveryWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}

func (rw *recoveryWriter) Unwrap() http.ResponseWriter { return rw.ResponseWriter }
