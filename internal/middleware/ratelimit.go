package middleware

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"img-api/internal/config"
	"img-api/internal/logger"
)

// RateLimiter 返回限流中间件与停止函数。
// 停止函数用于服务优雅关闭：终止后台清理协程（GUI 反复重启服务时防止 goroutine 泄漏）。
func RateLimiter() (Middleware, func()) {
	if !config.C.RateLimitEnabled {
		return func(next http.Handler) http.Handler { return next }, func() {}
	}

	limiter := &slidingWindowLimiter{
		window:         1 * time.Minute,
		maxReqs:        config.C.RateLimitMax,
		ips:            make(map[string]*timestamps),
		trustedProxies: parseTrustedProxies(config.C.TrustedProxies),
		stopCh:         make(chan struct{}),
		doneCh:         make(chan struct{}),
	}

	// 后台定期清理过期 IP 记录
	go limiter.cleanup(5 * time.Minute)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 健康检查探针不占限流配额：探针被 429 会连累容器被误判 unhealthy
			if r.URL.Path == "/health" {
				next.ServeHTTP(w, r)
				return
			}

			ip := realClientIP(r, limiter.trustedProxies)

			if !limiter.allow(ip) {
				logger.L.Warn("rate limit exceeded", "ip", ip)
				// Retry-After：告知客户端窗口长度，浏览器/爬虫可自动退避重试
				w.Header().Set("Retry-After", "60")
				writeJSON(w, http.StatusTooManyRequests, map[string]any{
					"code":    429,
					"message": "too many requests, please try again later",
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}, limiter.stop
}

// ── 滑动窗口内部实现 ──

// timestamps 存储单个 IP 的请求时间戳（有序，最早在前）。
type timestamps struct {
	times []time.Time
}

// slidingWindowLimiter 基于滑动窗口日志的限流器。
// 每次请求记录时间戳，判断过去 window 时长内的请求数是否超限。
// 相比固定窗口，不会在窗口边界产生突发 2× 的问题。
type slidingWindowLimiter struct {
	mu             sync.Mutex
	window         time.Duration
	maxReqs        int
	ips            map[string]*timestamps
	trustedProxies []*net.IPNet // 可信反代网段（来自 TRUSTED_PROXIES 配置）

	stopOnce sync.Once     // 保证 stop 只关闭一次 stopCh
	stopCh   chan struct{} // 通知清理协程退出
	doneCh   chan struct{} // 清理协程退出信号
}

// stop 停止后台清理协程（幂等；限流功能本身仍可用）。
func (l *slidingWindowLimiter) stop() {
	l.stopOnce.Do(func() { close(l.stopCh) })
	<-l.doneCh
}

// allow 检查 IP 在滑动窗口内是否允许通过。
func (l *slidingWindowLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-l.window)

	ts, ok := l.ips[ip]
	if !ok {
		l.ips[ip] = &timestamps{times: []time.Time{now}}
		return true
	}

	// 移除窗口外的旧时间戳
	idx := 0
	for idx < len(ts.times) && ts.times[idx].Before(cutoff) {
		idx++
	}
	ts.times = ts.times[idx:]

	// 判断是否超限
	if len(ts.times) >= l.maxReqs {
		return false
	}

	// 记录本次请求
	ts.times = append(ts.times, now)
	return true
}

// cleanup 定期清理无活跃请求的 IP 记录，防止内存泄漏。
func (l *slidingWindowLimiter) cleanup(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer func() {
		ticker.Stop()
		close(l.doneCh)
	}()
	for {
		select {
		case <-ticker.C:
			l.mu.Lock()
			cutoff := time.Now().Add(-l.window)
			for ip, ts := range l.ips {
				// 移除过期的
				idx := 0
				for idx < len(ts.times) && ts.times[idx].Before(cutoff) {
					idx++
				}
				ts.times = ts.times[idx:]
				// 无活跃请求 → 删除 IP 记录
				if len(ts.times) == 0 {
					delete(l.ips, ip)
				}
			}
			l.mu.Unlock()
		case <-l.stopCh:
			return
		}
	}
}

// realClientIP 获取真实客户端 IP（兼容 Nginx 反代，同时防 XFF 伪造）。
//
// 安全策略：只有当请求直接来自 TRUSTED_PROXIES 配置的网段时，
// 才信任 X-Forwarded-For / X-Real-IP 头；否则一律使用 TCP 对端地址。
// 否则攻击者可随意伪造 XFF 头绕过限流。
func realClientIP(r *http.Request, trusted []*net.IPNet) string {
	remoteIP := parseRemoteAddr(r.RemoteAddr)
	if remoteIP == nil {
		// 解析失败时的最后兜底（RemoteAddr 异常时极少发生）
		return r.RemoteAddr
	}

	// 对端是否可信代理？
	isTrusted := false
	for _, cidr := range trusted {
		if cidr.Contains(remoteIP) {
			isTrusted = true
			break
		}
	}
	if !isTrusted {
		return remoteIP.String() // 直连客户端：不信任任何转发头
	}

	// 可信代理来源：X-Forwarded-For 第一段为客户真实 IP。
	// 首段必须是合法 IP：垃圾/伪造值回退对端 IP，防止攻击者轮换任意
	// 字符串制造无限限流桶（限流器 map 膨胀）。
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		// X-Forwarded-For 格式: "client, proxy1, proxy2"
		first := fwd
		if idx := strings.IndexByte(fwd, ','); idx > 0 {
			first = fwd[:idx]
		}
		if ip := net.ParseIP(strings.TrimSpace(first)); ip != nil {
			return ip.String()
		}
	}
	// X-Real-IP 同样校验合法性
	if real := r.Header.Get("X-Real-IP"); real != "" {
		if ip := net.ParseIP(strings.TrimSpace(real)); ip != nil {
			return ip.String()
		}
	}
	return remoteIP.String()
}

// parseRemoteAddr 从 "ip:port"（IPv6 为 "[ip]:port"）解析 IP。
func parseRemoteAddr(remoteAddr string) net.IP {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	return net.ParseIP(strings.TrimSpace(host))
}

// parseTrustedProxies 将逗号分隔的 CIDR 列表解析为 net.IPNet 切片。
// 非法项静默跳过（配置错误不至于导致服务不可用）。
func parseTrustedProxies(list []string) []*net.IPNet {
	var nets []*net.IPNet
	for _, s := range list {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, cidr, err := net.ParseCIDR(s); err == nil {
			nets = append(nets, cidr)
		}
	}
	return nets
}
