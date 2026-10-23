// Package netx 提供带安全校验的出站 HTTP 客户端。
//
// 核心能力：
//   - 拨号前校验解析出的 IP（防 DNS rebinding 绕过 SSRF 检查）
//   - 拨号时直连已验证的 IP（规避"检查后再解析"的 TOCTOU 窗口）
//   - 合理的连接池参数，避免高并发下连接耗尽
//
// 用于 mode=image 代理和外部 API 池两个出站场景。
package netx

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// IsBlockedIP 判断 IP 是否属于禁止出站访问的地址（SSRF 防护）。
//
// 拦截范围：回环、内网（RFC1918）、链路本地、未指定（0.0.0.0 / ::）、
// 全部组播（224.0.0.0/4 与 ff00::/8）、IPv6 ULA（fc00::/7）、
// IPv4 0.0.0.0/8 与 CGNAT（100.64.0.0/10），以及不可公网路由的保留段：
// 192.0.0.0/24、198.18.0.0/15（基准测试）、192.88.99.0/24（废弃 6to4 中继）、
// TEST-NET（192.0.2.0/24、198.51.100.0/24、203.0.113.0/24）、240.0.0.0/4、
// 255.255.255.255、IPv6 文档段 2001:db8::/32 与废弃站点本地 fec0::/10。
// 注意：0.0.0.0 在 Linux 上等价于本机，必须拦截；ULA 虽可路由但属内部地址。
func IsBlockedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		switch {
		case v4[0] == 0: // 0.0.0.0/8
			return true
		case v4[0] == 100 && v4[1]&0xc0 == 64: // 100.64.0.0/10 CGNAT
			return true
		case v4[0] == 192 && v4[1] == 0 && v4[2] == 0: // 192.0.0.0/24
			return true
		case v4[0] == 192 && v4[1] == 0 && v4[2] == 2: // 192.0.2.0/24 TEST-NET-1
			return true
		case v4[0] == 192 && v4[1] == 88 && v4[2] == 99: // 192.88.99.0/24
			return true
		case v4[0] == 198 && (v4[1] == 18 || v4[1] == 19): // 198.18.0.0/15
			return true
		case v4[0] == 198 && v4[1] == 51 && v4[2] == 100: // 198.51.100.0/24 TEST-NET-2
			return true
		case v4[0] == 203 && v4[1] == 0 && v4[2] == 113: // 203.0.113.0/24 TEST-NET-3
			return true
		case v4[0] == 255 && v4[1] == 255 && v4[2] == 255 && v4[3] == 255:
			return true
		case v4[0] >= 240: // 240.0.0.0/4 保留段
			return true
		}
		return false
	}
	if v6 := ip.To16(); v6 != nil {
		// ULA fc00::/7；文档段 2001:db8::/32；废弃站点本地 fec0::/10
		if v6[0]&0xfe == 0xfc {
			return true
		}
		if v6[0] == 0x20 && v6[1] == 0x01 && v6[2] == 0x0d && v6[3]&0xf0 == 0xb0 {
			return true
		}
		if v6[0] == 0xfe && v6[1]&0xc0 == 0xc0 {
			return true
		}
		// Teredo 2001::/32：地址里编码的是取反后的 IPv4（服务端/客户端各一段），
		// 解码易错且合法用途极少，整段拦截。
		if v6[0] == 0x20 && v6[1] == 0x01 && v6[2] == 0x00 && v6[3] == 0x00 {
			return true
		}
		// v4-compatible ::/96（RFC 4291 已废弃）：::a.b.c.d 等价于 a.b.c.d。
		// 整段拦截（:: 与 ::1 已由 IsUnspecified/IsLoopback 捕获）。
		if isZeroBytes(v6[:12]) {
			return true
		}
		// NAT64 64:ff9b::/96 与 6to4 2002::/16：解出内嵌 IPv4，按 IPv4 规则再判一次。
		// 否则 64:ff9b::a9fe:a9fe 这类地址会绕过黑名单直达 169.254.169.254。
		if v4 := embeddedIPv4(v6); v4 != nil {
			return IsBlockedIP(v4)
		}
	}
	return false
}

// embeddedIPv4 从 IPv6 转换地址中解出内嵌的 IPv4（无内嵌则返回 nil）。
//
// 覆盖两类可公开路由的转换前缀：
//   - NAT64 64:ff9b::/96 — 后 4 字节是 IPv4
//   - 6to4  2002::/16    — 第 2~5 字节是 IPv4
func embeddedIPv4(v6 net.IP) net.IP {
	// NAT64：前缀 64:ff9b:: + 中间 8 字节为 0
	if v6[0] == 0x00 && v6[1] == 0x64 && v6[2] == 0xff && v6[3] == 0x9b && isZeroBytes(v6[4:12]) {
		return net.IP(v6[12:16])
	}
	// 6to4
	if v6[0] == 0x20 && v6[1] == 0x02 {
		return net.IP(v6[2:6])
	}
	return nil
}

// isZeroBytes 判断字节切片是否全为 0。
func isZeroBytes(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

// SafeDialer 是带安全校验与短 TTL DNS 缓存的出站拨号器。
//
// 工作流程（DialContext）：
//  1. 解析主机名 → 得到全部 IP（带 30s 缓存，避免每次建连全量解析）
//  2. 任一 IP 为内网地址 → 拒绝拨号
//  3. 逐个尝试直连合法 IP（而非重新解析域名，防范 DNS rebinding）
//
// 注意：http.Transport 的 TLS ServerName 仍取请求 URL 的原始主机名，
// 因此直连 IP 不影响 HTTPS 证书校验与 SNI。
// resolver 为 nil 时使用 net.DefaultResolver（测试可注入自定义解析器）。
type SafeDialer struct {
	resolver *net.Resolver

	cacheMu sync.Mutex
	cache   map[string]dnsCacheEntry
}

// dnsCacheEntry 是单主机名的解析缓存条目。
type dnsCacheEntry struct {
	ips    []net.IPAddr
	expire time.Time
}

// dnsCacheTTL 是 DNS 解析缓存的有效时长。
const dnsCacheTTL = 30 * time.Second

// NewSafeDialer 创建安全拨号器（resolver 传 nil 使用系统默认解析器）。
func NewSafeDialer(resolver *net.Resolver) *SafeDialer {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return &SafeDialer{
		resolver: resolver,
		cache:    make(map[string]dnsCacheEntry),
	}
}

// defaultDialer 是包级默认实例（SafeDialContext / NewClient 共用）。
var defaultDialer = NewSafeDialer(nil)

// DialContext 实现拨号：解析（带缓存）→ 校验全部 IP → 逐个直连。
func (d *SafeDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("netx: parse addr %s: %w", addr, err)
	}

	ips, err := d.lookupIP(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("netx: resolve %s: %w", host, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("netx: resolve %s: no addresses", host)
	}

	// 校验阶段：全部解析结果都必须是公网地址
	for _, ip := range ips {
		if IsBlockedIP(ip.IP) {
			return nil, fmt.Errorf("netx: blocked private address %s", ip.IP)
		}
	}

	// 拨号阶段：逐个尝试合法 IP（第一个连不上时回退到下一个）
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	var lastErr error
	for _, ip := range ips {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("netx: no addresses to dial for %s", host)
	}
	return nil, fmt.Errorf("netx: dial %s: %w", host, lastErr)
}

// lookupIP 解析主机名（带短 TTL 缓存；仅缓存成功结果，
// 失败不缓存以允许瞬时 DNS 故障快速恢复）。
func (d *SafeDialer) lookupIP(ctx context.Context, host string) ([]net.IPAddr, error) {
	d.cacheMu.Lock()
	if e, ok := d.cache[host]; ok && time.Now().Before(e.expire) {
		ips := e.ips
		d.cacheMu.Unlock()
		return ips, nil
	}
	d.cacheMu.Unlock()

	ips, err := d.resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}

	d.cacheMu.Lock()
	d.cache[host] = dnsCacheEntry{ips: ips, expire: time.Now().Add(dnsCacheTTL)}
	// 顺手清理过期条目，限制缓存增长
	now := time.Now()
	for k, e := range d.cache {
		if now.After(e.expire) {
			delete(d.cache, k)
		}
	}
	d.cacheMu.Unlock()

	return ips, nil
}

// SafeDialContext 使用默认拨号器在建立 TCP 连接前校验目标 IP（兼容旧调用方）。
func SafeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return defaultDialer.DialContext(ctx, network, addr)
}

// NewClient 创建带安全拨号和连接池配置的 HTTP 客户端。
//
// timeout — 整体请求超时（含 DNS、连接、读写的总时长）
func NewClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			// 显式禁用环境代理（HTTP_PROXY/HTTPS_PROXY）：
			// 通过代理出站时 SafeDialContext 校验的是代理 IP 而非目标 IP，
			// 内网目标会绕过 SSRF 拦截。注意 Transport.Proxy 字段为 nil
			// 时 Go 默认走 ProxyFromEnvironment，必须用空函数覆盖。
			Proxy:               func(*http.Request) (*url.URL, error) { return nil, nil },
			DialContext:         SafeDialContext,
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 20,
			IdleConnTimeout:     90 * time.Second,
		},
	}
}
