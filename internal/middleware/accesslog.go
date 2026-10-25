package middleware

import (
	"io"
	"net/http"
	"time"

	"img-api/internal/config"
	"img-api/internal/logger"
)

// maxLoggedQueryLen 是访问日志中 query 的最大记录长度（超出截断）。
// handler 已对 category/api 限长 128 字节，但其他参数未限，截断避免日志膨胀。
const maxLoggedQueryLen = 256

// statusRecorder 包装 ResponseWriter 以捕获响应状态码。
// Unwrap 使 http.NewResponseController 能透传底层 Flush/Hijack 能力。
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// ReadFrom 透传 io.ReaderFrom 快速路径（sendfile / 底层缓冲直接落盘）。
//
// handler 的图片输出走 io.Copy(w, &io.LimitedReader{...})：若本包装器不实现 io.ReaderFrom，
// io.Copy 会退化为 32KB 用户态缓冲反复 Write，拿不到 net.TCPConn.ReadFrom 的
// sendfile 优化。中间件链上每一层包装都必须透传，否则整条链断掉。
func (s *statusRecorder) ReadFrom(src io.Reader) (int64, error) {
	if rf, ok := s.ResponseWriter.(io.ReaderFrom); ok {
		// ReadFrom 不经过 WriteHeader：状态码就是隐式 200，无需改动 s.status
		// （初始化即 http.StatusOK），直接委托以保留 sendfile 快速路径。
		return rf.ReadFrom(src)
	}
	// 底层不支持：断开设递归（用只含 Writer 的视图，避免再次匹配到本方法）
	return io.Copy(struct{ io.Writer }{s}, src)
}

// AccessLog 记录每个请求的基础访问日志：方法、路径、query、状态码、耗时、IP、RequestID。
//
// 应注册在 RequestID 之后，这样日志携带的 request_id 与响应头一致，
// 便于排障时关联单次请求。
// 健康检查路径（/health）不写访问日志：
// Docker healthcheck 每 30s 探测一次，记录会造成日志噪声。
//
// query 会一并记录（超长截断）：/random 的全部语义都在 query 里
// （source/category/api/mode/type），只记 Path 时看到 "status=502"
// 无法判断是哪个参数/图源出的问题。
func AccessLog() Middleware {
	// 与限流器一致：仅当请求来自 TRUSTED_PROXIES 网段时才信任转发头，
	// 反代部署下日志才能记录真实客户端 IP（未配置则按 TCP 对端 IP，防伪造）。
	trusted := parseTrustedProxies(config.C.TrustedProxies)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 健康检查探测不记录日志，直接放行（精确匹配，避免误伤 /healthz 等未来路径）
			if r.URL.Path == "/health" {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			// 落在 defer 里：超限出图时 handler 会 panic(http.ErrAbortHandler) 主动断连，
			// 顺序执行路径会被 panic 跳过，这类请求在访问日志中完全消失。
			defer func() {
				logger.L.Info("access",
					"request_id", RequestIDFrom(r.Context()),
					"method", r.Method,
					"path", r.URL.Path,
					"query", truncateQuery(r.URL.RawQuery),
					"status", rec.status,
					"latency_ms", time.Since(start).Milliseconds(),
					"ip", realClientIP(r, trusted),
				)
			}()
			next.ServeHTTP(rec, r)
		})
	}
}

// truncateQuery 截断过长的 query（按字节；日志用途足够，尾部可能截断在多字节字符中间，
// 不影响排障，故不做 UTF-8 边界修正）。
func truncateQuery(raw string) string {
	if len(raw) <= maxLoggedQueryLen {
		return raw
	}
	return raw[:maxLoggedQueryLen] + "...(truncated)"
}
