// Package handler — 健康检查处理器
package handler

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"img-api/internal/config"
	"img-api/internal/model"
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

// 健康状态字符串常量。
//
// 输出契约（/health 的 checks 字段，外部监控可能依赖该格式）：
//
//	"healthy"          — 图源可用（判定用这个精确值）
//	"unhealthy: <原因>" — 图源不可用（由 RandomService.Health 拼接）
//	"disabled" / "unconfigured (...)" / "healthy (N APIs)" — 信息性状态，不参与判定
//
// 历史注记：判定曾用 `strings.HasPrefix(status, "healthy ")`，要求 "healthy" 后恰好
// 跟一个空格——仓库返回的是不带后缀的 "healthy"，这种前缀判断一改状态串就会静默误判。
// 现在只比较 healthHealthy 精确值。
const (
	healthHealthy = "healthy"
)

// fullHealth 返回完整内部状态。
func (h *HealthHandler) fullHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	checks := h.svc.Health(ctx)

	// 只对"已配置内容"的图源做健康判定：
	//   - 未初始化的图源（checks 里没有这个键）→ 信息性状态，不参与判定；
	//   - 没有任何内容的图源（SourceEmpty）→ 同属"未配置"，不参与判定。
	// 因此 status=ok 的含义是"**已配置的图源**都能取图"，而不是"三种图源都必须存在"。
	//
	// 历史：此前把未初始化的图源一律按不健康处理，本意是防止"local 完全不可用也
	// 恒报 ok"；但 external 仓库从不注册（external 走 ExternalPool，不经 repos），
	// checks 里永远没有 "external"，于是 /health 恒为 degraded，Docker healthcheck
	// （compose 带 -strict）把容器判成永久 unhealthy——连 image.yaml 配好的实例也一样。
	allHealthy := true
	for _, source := range []model.SourceType{model.SourceTxt, model.SourceLocal, model.SourceExternal} {
		status, ok := checks[string(source)]
		if !ok || h.svc.SourceEmpty(source) {
			continue // 未初始化 / 未配置内容：不参与判定
		}
		if status != healthHealthy {
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
