// Package circuit 实现经典的三态熔断器模式。
//
// 状态转换：
//
//	CLOSED  ──(失败数达阈值)──→  OPEN
//	OPEN    ──(超时后)────────→  HALF_OPEN
//	HALF_OPEN ──(成功)────────→  CLOSED
//	HALF_OPEN ──(失败)────────→  OPEN
//
// 用途：当外部 API 不可用时，熔断器立即拒绝请求，
// 避免大量请求堆积等待超时，保护服务稳定性。
package circuit

import (
	"fmt"
	"sync"
	"time"
)

// State 表示熔断器当前所处状态。
// 状态机转换路径：CLOSED → OPEN → HALF_OPEN → CLOSED
type State int

const (
	StateClosed   State = iota // 正常通行，计数连续失败
	StateOpen                   // 断路中，立即拒绝所有请求
	StateHalfOpen               // 探测恢复，放行有限请求测试远端是否恢复
)

// String 返回状态的可读英文名（CLOSED / OPEN / HALF_OPEN）。
// 主要用于日志和 /health 健康检查响应。
func (s State) String() string {
	switch s {
	case StateClosed:
		return "CLOSED"
	case StateOpen:
		return "OPEN"
	case StateHalfOpen:
		return "HALF_OPEN"
	}
	return "UNKNOWN"
}

// Breaker 实现经典的三态熔断器，保护外部 API 调用。
//
// 所有字段通过 sync.RWMutex 保护，保证并发安全。
type Breaker struct {
	mu sync.RWMutex // 保护以下所有字段

	failureThreshold int           // CLOSED 状态下连续失败 N 次后进入 OPEN
	timeout          time.Duration // OPEN 状态持续多久后自动进入 HALF_OPEN
	halfOpenMax      int           // HALF_OPEN 状态最多放行多少个探测请求

	state       State     // 当前状态
	failures    int       // CLOSED 状态下连续失败计数（成功后重置）
	lastFailAt  time.Time // 最近一次失败的时间（用于判断 OPEN→HALF_OPEN 超时）
	halfTries   int       // HALF_OPEN 状态下已放行的探测次数
	lastStateAt time.Time // 最后一次状态变更的时间（调试用）
}

// NewBreaker 创建熔断器实例。
//
// 参数说明：
//   failureThreshold — CLOSED 状态连续失败多少次后转为 OPEN（建议 3~10）
//   timeoutSeconds   — OPEN 状态持续多少秒后尝试 HALF_OPEN（建议 30~120）
//   halfOpenMax      — HALF_OPEN 最多放行多少请求，任一成功即恢复 CLOSED（建议 1~5）
func NewBreaker(failureThreshold, timeoutSeconds, halfOpenMax int) *Breaker {
	return &Breaker{
		failureThreshold: failureThreshold,
		timeout:          time.Duration(timeoutSeconds) * time.Second,
		halfOpenMax:      halfOpenMax,
		state:            StateClosed,
	}
}

// Call 执行受熔断器保护的操作。
// 如果熔断器打开，返回 ErrCircuitOpen 而不执行 fn。
// 原子性地检查状态 + 记录半开尝试次数，消除 TOCTOU 竞态。
func (b *Breaker) Call(fn func() error) error {
	b.mu.Lock()

	switch b.state {
	case StateClosed:
		// 正常放行
	case StateOpen:
		if time.Since(b.lastFailAt) > b.timeout {
			b.state = StateHalfOpen
			b.halfTries = 0
		} else {
			b.mu.Unlock()
			return ErrCircuitOpen
		}
	case StateHalfOpen:
		if b.halfTries >= b.halfOpenMax {
			b.mu.Unlock()
			return ErrCircuitOpen
		}
	}

	if b.state == StateHalfOpen {
		b.halfTries++ // 原子性计数，防止并发探测超过限制
	}
	b.mu.Unlock()

	// 2. 执行实际操作（不持锁，IO 不应阻塞状态管理）
	//    recover：fn panic 时视为失败，防止熔断器状态卡死。
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("circuit: fn panicked: %v", r)
			}
		}()
		err = fn()
	}()

	// 3. 记录结果
	b.mu.Lock()
	defer b.mu.Unlock()

	if err != nil {
		b.onFailure()
	} else {
		b.onSuccess()
	}

	return err
}

// onSuccess 请求成功时的状态处理（调用方已持锁）
func (b *Breaker) onSuccess() {
	switch b.state {
	case StateHalfOpen:
		b.state = StateClosed
		b.failures = 0
		b.halfTries = 0
	case StateClosed:
		b.failures = 0
	}
	b.lastStateAt = time.Now()
}

// onFailure 请求失败时的状态处理（调用方已持锁）
func (b *Breaker) onFailure() {
	b.failures++
	b.lastFailAt = time.Now()

	switch b.state {
	case StateClosed:
		if b.failures >= b.failureThreshold {
			b.state = StateOpen
			b.lastStateAt = time.Now()
		}
	case StateHalfOpen:
		b.state = StateOpen
		b.halfTries = 0
		b.lastStateAt = time.Now()
	}
}

// State 返回当前状态
func (b *Breaker) State() State {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.state
}

// Failures 返回当前连续失败数
func (b *Breaker) Failures() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.failures
}

// ErrCircuitOpen 熔断器打开时返回的错误
var ErrCircuitOpen = &CircuitOpenError{}

// CircuitOpenError 熔断打开错误
type CircuitOpenError struct{}

func (e *CircuitOpenError) Error() string {
	return "circuit breaker is open, request rejected"
}
