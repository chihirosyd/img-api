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
				// 限流器在 CORS 外层：命中时 CORS 中间件根本不会执行，
				// 必须在这里补齐跨域头与安全头，否则浏览器 fetch 只会报
				// "CORS 错误"，读不到 429 与 Retry-After（前端无法优雅退避）。
				if config.C.CorsEnabled {
					setCORSHeaders(w)
				}
				addSecurityHeaders(w)
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
	if !isTrustedIP(remoteIP, trusted) {
		return remoteIP.String() // 直连客户端：不信任任何转发头
	}

	// 可信代理来源：从右向左找第一个"不可信"地址作为客户端 IP。
	//
	// 不能直接取最左段：反代若用 $proxy_add_x_forwarded_for（追加语义），
	// 头会变成 "客户端自带XFF, 真实客户端IP"，最左段完全由攻击者控制——
	// 每次换一个伪造 IP 就换一个限流桶，限流形同虚设，访问日志也会被写脏。
	// 逐段向左跳过属于 TRUSTED_PROXIES 的地址，第一个不可信地址才是最接近
	// 真实客户端、且无法被伪造的那一跳。
	//
	// 注意：若真实客户端本身就落在 TRUSTED_PROXIES 网段内（例如内网访客经
	// 同一网关访问），它会被当作代理跳过，最终回退到网关 IP 共用一个桶。
	// 这是"宁可误伤不可绕过"的取舍，网段应只覆盖网关本身。
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		// X-Forwarded-For 格式: "client, proxy1, proxy2"
		parts := strings.Split(fwd, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			ip := net.ParseIP(strings.TrimSpace(parts[i]))
			if ip == nil {
				continue // 非 IP（垃圾/伪造值）：跳过，不据此建限流桶
			}
			if isTrustedIP(ip, trusted) {
				continue // 可信代理自身，继续向左
			}
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

// isTrustedIP 判断 IP 是否属于任一可信代理网段。
func isTrustedIP(ip net.IP, trusted []*net.IPNet) bool {
	for _, cidr := range trusted {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
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
