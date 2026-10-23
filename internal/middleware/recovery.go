package middleware

import (
	"io"
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

// ReadFrom 透传 io.ReaderFrom 快速路径（sendfile / 底层缓冲直接落盘）。
//
// 与 statusRecorder 同理：中间件链上任一层包装缺少该方法，handler 的
// io.Copy(w, &io.LimitedReader{...}) 就会退化为 32KB 用户态缓冲反复 Write。
func (rw *recoveryWriter) ReadFrom(src io.Reader) (int64, error) {
	if !rw.wroteHeader {
		// ReadFrom 不经过 Write/WriteHeader：先标记已提交（隐式 200），
		// 否则 panic 恢复时会把 500 追加到已写出的响应体之后。
		rw.WriteHeader(http.StatusOK)
	}
	if rf, ok := rw.ResponseWriter.(io.ReaderFrom); ok {
		return rf.ReadFrom(src)
	}
	// 底层不支持：断开设递归（用只含 Writer 的视图，避免再次匹配到本方法）
	return io.Copy(struct{ io.Writer }{rw}, src)
}
