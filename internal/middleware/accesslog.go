package middleware

import (
	"net/http"
	"strings"
	"time"

	"img-api/internal/config"
	"img-api/internal/logger"
)

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

// AccessLog 记录每个请求的基础访问日志：方法、路径、状态码、耗时、IP、RequestID。
//
// 应注册在 RequestID 之后，这样日志携带的 request_id 与响应头一致，
// 便于排障时关联单次请求。
// 健康检查路径（/health）不写访问日志：
// Docker healthcheck 每 30s 探测一次，记录会造成日志噪声。
func AccessLog() Middleware {
	// 与限流器一致：仅当请求来自 TRUSTED_PROXIES 网段时才信任转发头，
	// 反代部署下日志才能记录真实客户端 IP（未配置则按 TCP 对端 IP，防伪造）。
	trusted := parseTrustedProxies(config.C.TrustedProxies)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 健康检查探测不记录日志，直接放行
			if strings.HasPrefix(r.URL.Path, "/health") {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)

			logger.L.Info("access",
				"request_id", RequestIDFrom(r.Context()),
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"latency_ms", time.Since(start).Milliseconds(),
				"ip", realClientIP(r, trusted),
			)
		})
	}
}
