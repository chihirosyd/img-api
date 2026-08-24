// Package handler — 健康检查处理器
package handler

import (
	"fmt"
	"net/http"
	"strings"
	"time"

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

// Health 处理 GET /health — 返回服务健康状态与运行时统计。
func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	h.fullHealth(w, r)
}

// fullHealth 返回完整内部状态。
func (h *HealthHandler) fullHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	checks := h.svc.Health(ctx)

	// 只对图源仓库的状态做健康判定（txt / local / external）。
	// external_pool、circuit_breaker、cache 等属于信息性状态，
	// 不参与"是否健康"的判断，否则将持续误报 degraded。
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

	writeJSON(w, http.StatusOK, map[string]any{
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
