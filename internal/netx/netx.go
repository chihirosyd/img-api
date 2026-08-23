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
	"time"
)

// IsBlockedIP 判断 IP 是否属于禁止出站访问的地址（SSRF 防护）。
//
// 拦截范围：回环、内网（RFC1918）、链路本地、未指定（0.0.0.0 / ::）、
// 全部组播（224.0.0.0/4 与 ff00::/8）、IPv6 ULA（fc00::/7）、
// IPv4 0.0.0.0/8 与 CGNAT（100.64.0.0/10）。
// 注意：0.0.0.0 在 Linux 上等价于本机，必须拦截；ULA 虽可路由但属内部地址。
func IsBlockedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		// 0.0.0.0/8 与 100.64.0.0/10（CGNAT）
		return v4[0] == 0 || (v4[0] == 100 && v4[1]&0xc0 == 64)
	}
	if v6 := ip.To16(); v6 != nil {
		// ULA fc00::/7
		return v6[0]&0xfe == 0xfc
	}
	return false
}

// SafeDialContext 在建立 TCP 连接前校验目标 IP。
//
// 工作流程：
//  1. 解析主机名 → 得到全部 IP
//  2. 任一 IP 为内网地址 → 拒绝拨号
//  3. 逐个尝试直连合法 IP（而非重新解析域名，防范 DNS rebinding）
//
// 注意：http.Transport 的 TLS ServerName 仍取请求 URL 的原始主机名，
// 因此直连 IP 不影响 HTTPS 证书校验与 SNI。
func SafeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("netx: parse addr %s: %w", addr, err)
	}

	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
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
