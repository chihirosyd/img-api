// Package service — 请求统计服务（线程安全）。
//
// 使用 atomic.Int64 实现无锁计数，通过 Snapshot() 获取一致性快照。
// 统计数据通过 /health 接口暴露，方便接入 Prometheus 等监控系统。
package service

import (
	"sync/atomic"
	"time"
)

// Stats 收集并暴露运行时统计指标。
//
// 所有 RecordXxx 方法均线程安全，可在多个 goroutine 中并发调用。
type Stats struct {
	totalRequests atomic.Int64 // 自启动以来接收到的总请求数
	totalSuccess  atomic.Int64 // 成功返回图片的次数
	totalFail     atomic.Int64 // 失败的次数（含 4xx + 5xx）
	circuitTrips  atomic.Int64 // 熔断器拦截的次数
	startTime     time.Time    // 服务启动时间（用于计算 uptime）
}

// NewStats 创建统计实例，记录当前时间为启动时间。
func NewStats() *Stats {
	return &Stats{startTime: time.Now()}
}

// RecordRequest 总请求 +1。在 handler 入口处调用。
func (s *Stats) RecordRequest() { s.totalRequests.Add(1) }

// RecordSuccess 成功 +1。图片返回成功时调用。
func (s *Stats) RecordSuccess() { s.totalSuccess.Add(1) }

// RecordFail 失败 +1。参数校验失败、仓库异常、熔断等均算失败。
func (s *Stats) RecordFail() { s.totalFail.Add(1) }

// RecordCircuitTrip 熔断器拦截 +1。仅外部 API 来源触发。
func (s *Stats) RecordCircuitTrip() { s.circuitTrips.Add(1) }

// Snapshot 返回所有计数器的当前快照。
//
// 注意：各计数器独立读取，快照并非严格原子，
// 在高并发下可能产生微小偏差（但作为监控指标足够）。
func (s *Stats) Snapshot() StatsSnapshot {
	return StatsSnapshot{
		TotalRequests: s.totalRequests.Load(),
		TotalSuccess:  s.totalSuccess.Load(),
		TotalFail:     s.totalFail.Load(),
		CircuitTrips:  s.circuitTrips.Load(),
		UptimeSeconds: int64(time.Since(s.startTime).Seconds()),
	}
}

// StatsSnapshot 是一次统计快照，序列化为 JSON 返回给 /health。
type StatsSnapshot struct {
	TotalRequests int64 `json:"total_requests"`
	TotalSuccess  int64 `json:"total_success"`
	TotalFail     int64 `json:"total_fail"`
	CircuitTrips  int64 `json:"circuit_trips"`
	UptimeSeconds int64 `json:"uptime_seconds"`
}
