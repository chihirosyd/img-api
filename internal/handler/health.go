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

// durUnit 是时长单位（名称 + 该单位的秒数）。
type durUnit struct {
	name string
	secs int64
}

// formatDuration 将 Duration 格式化为人类可读的运行时长。
//
// 按量级自动选择单位，每级保留两位有效单位（如 45h → "1d 21h"、30d → "1mo"）：
//
//	< 1 分钟 → "Xs"；< 1 小时 → "Xm Ys"；< 1 天 → "Xh Ym"
//	< 30 天 → "Xd Yh"；< 365 天 → "Xmo Yd"；≥ 365 天 → "Xy Ymo"
//
// 月按 30 天、年按 365 天近似计算（展示用途，精度足够）。
func formatDuration(d time.Duration) string {
	sec := int64(d.Seconds())
	if sec < 0 {
		sec = 0
	}

	const (
		minute = int64(60)
		hour   = 60 * minute
		day    = 24 * hour
		month  = 30 * day
		year   = 365 * day
	)

	if sec < minute {
		return fmt.Sprintf("%ds", sec)
	}

	// 每级的单位组合：[高位单位, 低位单位]
	var units []durUnit
	switch {
	case sec < hour:
		units = []durUnit{{"m", minute}, {"s", 1}}
	case sec < day:
		units = []durUnit{{"h", hour}, {"m", minute}}
	case sec < month:
		units = []durUnit{{"d", day}, {"h", hour}}
	case sec < year:
		units = []durUnit{{"mo", month}, {"d", day}}
	default:
		units = []durUnit{{"y", year}, {"mo", month}}
	}

	var b strings.Builder
	for _, u := range units {
		if sec < u.secs {
			continue // 低位单位为 0 时省略（如 "1d" 而非 "1d 0h"）
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%d%s", sec/u.secs, u.name)
		sec %= u.secs
	}
	return b.String()
}
