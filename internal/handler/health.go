// Package handler — 健康检查处理器
package handler

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"img-api/internal/config"
	"img-api/internal/service"
)

// HealthHandler 处理健康检查请求。
// 提供各仓库、熔断器、缓存的状态以及运行时统计。
type HealthHandler struct {
	svc     *service.RandomService // 用于获取各仓库健康状态
	stats   *service.Stats         // 运行时统计
	startAt time.Time              // handler 创建时间（近似等于服务启动时间）
}

// NewHealthHandler 创建健康检查处理器。
func NewHealthHandler(svc *service.RandomService, stats *service.Stats) *HealthHandler {
	return &HealthHandler{
		svc:     svc,
		stats:   stats,
		startAt: time.Now(),
	}
}

// Health 处理 GET /health（公开模式：极简状态）。
//
// 如果配置了 HEALTH_SECRET，公开模式仅返回：
//
//	{"status":"ok","version":"1.0.0"}
//
// 完整内部状态需通过 /health-{secret} 访问。
func (h *HealthHandler) Health(c *gin.Context) {
	if config.C.HealthSecret != "" {
		// 有密钥配置 → 公开模式限制输出
		c.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"version": config.C.Version,
		})
		return
	}
	// 无密钥配置 → 直接返回完整状态
	h.fullHealth(c)
}

// FullHealth 处理 GET /health-{secret}（私有模式）。
//
// 密钥校验优先级：
//  1. 请求头 X-Health-Secret（推荐，避免密钥出现在 URL 中被代理日志记录）
//  2. URL 路径 /health-{secret}（兼容旧版）
//
// 未配置 HEALTH_SECRET 时等同于公开模式。
func (h *HealthHandler) FullHealth(c *gin.Context) {
	secret := c.Param("secret")
	// Header 优先（避免密钥出现在 URL 中）
	if headerSecret := c.GetHeader("X-Health-Secret"); headerSecret != "" {
		secret = headerSecret
	}
	// 常量时间比较，防止时序攻击
	if config.C.HealthSecret != "" && subtle.ConstantTimeCompare([]byte(secret), []byte(config.C.HealthSecret)) != 1 {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "invalid health secret"})
		return
	}
	h.fullHealth(c)
}

// fullHealth 返回完整内部状态（被 Health 和 FullHealth 复用）。
func (h *HealthHandler) fullHealth(c *gin.Context) {
	ctx := c.Request.Context()
	checks := h.svc.Health(ctx)

	// 只对图源仓库的状态做健康判定（txt / local / external）。
	// external_pool、circuit_breaker、cache 等属于信息性状态，
	// 不参与"是否健康"的判断，否则会永远误报 degraded。
	allHealthy := true
	for _, source := range []string{"txt", "local", "external"} {
		status, ok := checks[source]
		if !ok {
			continue // 该图源从未被请求，未初始化，不参与判定
		}
		if status != "healthy" && !strings.HasPrefix(status, "healthy ") {
			allHealthy = false
			break
		}
	}

	status := "ok"
	if !allHealthy {
		status = "degraded"
	}

	uptime := formatDuration(time.Since(h.startAt))
	snapshot := h.stats.Snapshot()

	c.JSON(http.StatusOK, gin.H{
		"status":  status,
		"version": config.C.Version,
		"uptime":  uptime,
		"checks":  checks,
		"stats":   snapshot,
	})
}

// formatDuration 将 Duration 格式化为 "Xh Ym Zs" 的可读形式。
// 小于 1 小时时不展示小时部分，小于 1 分钟时不展示分钟部分。
func formatDuration(d time.Duration) string {
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	if hours > 0 {
		return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}
