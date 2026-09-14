package circuit

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBreakerOpensAfterThreshold(t *testing.T) {
	b := NewBreaker(3, 30, 1)

	for i := 0; i < 3; i++ {
		if err := b.Call(func() error { return errors.New("boom") }); err == nil {
			t.Fatal("expected failure error")
		}
	}
	if b.State() != StateOpen {
		t.Fatalf("state = %s, want OPEN", b.State())
	}

	// 断路期间直接拒绝，不执行 fn
	if err := b.Call(func() error { return nil }); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("err = %v, want ErrCircuitOpen", err)
	}
}

func TestBreakerSuccessResetsFailures(t *testing.T) {
	b := NewBreaker(3, 30, 1)

	for i := 0; i < 2; i++ {
		if err := b.Call(func() error { return errors.New("boom") }); err == nil {
			t.Fatal("expected failure error")
		}
	}
	if err := b.Call(func() error { return nil }); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.Failures() != 0 {
		t.Fatalf("failures = %d, want 0", b.Failures())
	}
}

func TestBreakerHalfOpenRecovery(t *testing.T) {
	// timeout=0：断路后立即进入半开探测
	b := NewBreaker(1, 0, 2)

	if err := b.Call(func() error { return errors.New("boom") }); err == nil {
		t.Fatal("expected failure error")
	}
	if b.State() != StateOpen {
		t.Fatalf("state = %s, want OPEN", b.State())
	}

	// 半开探测成功 → 恢复 CLOSED
	if err := b.Call(func() error { return nil }); err != nil {
		t.Fatalf("probe should succeed: %v", err)
	}
	if b.State() != StateClosed {
		t.Fatalf("state = %s, want CLOSED", b.State())
	}
}

func TestBreakerHalfOpenFailureReopens(t *testing.T) {
	b := NewBreaker(1, 0, 2)

	if err := b.Call(func() error { return errors.New("boom") }); err == nil {
		t.Fatal("expected failure error")
	}
	// 半开探测失败 → 重新 OPEN
	if err := b.Call(func() error { return errors.New("boom") }); err == nil {
		t.Fatal("expected probe failure error")
	}
	if b.State() != StateOpen {
		t.Fatalf("state = %s, want OPEN", b.State())
	}
}

// TestBreakerClampsInvalidParams 回归 L4：构造入参非法值被钳位——
// halfOpenMax=0 时熔断器会永久卡在 HALF_OPEN（外部图源永久不可用），
// failureThreshold=0 时一次失败即熔断。
func TestBreakerClampsInvalidParams(t *testing.T) {
	b := NewBreaker(0, 0, 0)
	if b.failureThreshold != 1 {
		t.Fatalf("failureThreshold = %d, want clamped 1", b.failureThreshold)
	}
	if b.halfOpenMax != 1 {
		t.Fatalf("halfOpenMax = %d, want clamped 1", b.halfOpenMax)
	}

	// 钳位后熔断器可用：失败一次 → OPEN，半开探测能放行（不会永久卡死）
	if err := b.Call(func() error { return errors.New("boom") }); err == nil {
		t.Fatal("expected failure error")
	}
	if err := b.Call(func() error { return nil }); err != nil {
		t.Fatalf("probe should succeed after clamping: %v", err)
	}
	if b.State() != StateClosed {
		t.Fatalf("state = %s, want CLOSED", b.State())
	}
}

// TestBreakerConcurrentHalfOpenCap 并发半开探测不超 halfOpenMax：
// 探测槽位在状态检查时原子占用，最多放行 halfOpenMax 个。
func TestBreakerConcurrentHalfOpenCap(t *testing.T) {
	b := NewBreaker(1, 0, 2) // timeout=0：断路后立即半开
	_ = b.Call(func() error { return errors.New("boom") }) // 首次失败 → OPEN（预期错误）

	var executed atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = b.Call(func() error {
				executed.Add(1)
				time.Sleep(10 * time.Millisecond)
				return errors.New("still failing")
			})
		}()
	}
	wg.Wait()

	if n := executed.Load(); n > 2 {
		t.Fatalf("executed = %d, want <= halfOpenMax 2", n)
	}
	if n := executed.Load(); n < 1 {
		t.Fatalf("executed = %d, want at least 1 probe", n)
	}
}

// TestBreakerPanicTreatedAsFailure fn panic 视为失败：返回错误且不卡死状态机。
func TestBreakerPanicTreatedAsFailure(t *testing.T) {
	b := NewBreaker(3, 30, 1)
	err := b.Call(func() error { panic("boom") })
	if err == nil || !strings.Contains(err.Error(), "panicked") {
		t.Fatalf("err = %v, want panic-converted failure", err)
	}
	if b.Failures() != 1 {
		t.Fatalf("failures = %d, want 1（panic 计入失败）", b.Failures())
	}
}

// TestBreakerTimeoutTransitionWithClock 注入假时钟验证 OPEN→HALF_OPEN 超时转移。
func TestBreakerTimeoutTransitionWithClock(t *testing.T) {
	b := NewBreaker(1, 30, 1)
	var fake atomic.Int64 // 假时钟：纳秒
	b.now = func() time.Time { return time.Unix(0, fake.Load()) }

	if err := b.Call(func() error { return errors.New("boom") }); err == nil {
		t.Fatal("expected failure")
	}
	if b.State() != StateOpen {
		t.Fatalf("state = %s, want OPEN", b.State())
	}

	// 未到超时：仍拒绝
	if err := b.Call(func() error { return nil }); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("err = %v, want ErrCircuitOpen before timeout", err)
	}

	// 拨快 31 秒：进入半开，探测成功 → CLOSED
	fake.Store(int64(31 * time.Second))
	if err := b.Call(func() error { return nil }); err != nil {
		t.Fatalf("probe after timeout should succeed: %v", err)
	}
	if b.State() != StateClosed {
		t.Fatalf("state = %s, want CLOSED after recovery", b.State())
	}
}

// TestBreakerCanceledNotCounted 客户端取消/超时不改变熔断状态：
// 不把"用户放弃请求"记成上游故障。
func TestBreakerCanceledNotCounted(t *testing.T) {
	b := NewBreaker(3, 30, 1)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := b.Call(func() error { return ctx.Err() }); err == nil {
		t.Fatal("expected canceled error")
	}
	if b.Failures() != 0 {
		t.Fatalf("failures = %d, want 0（Canceled 不计失败）", b.Failures())
	}

	if err := b.Call(func() error { return context.DeadlineExceeded }); err == nil {
		t.Fatal("expected deadline error")
	}
	if b.Failures() != 0 {
		t.Fatalf("failures = %d, want 0（DeadlineExceeded 不计失败）", b.Failures())
	}

	// Trips 计数：真实失败达到阈值后进入 OPEN，trips +1
	for i := 0; i < 3; i++ {
		if err := b.Call(func() error { return errors.New("boom") }); err == nil {
			t.Fatal("expected failure")
		}
	}
	if b.State() != StateOpen || b.Trips() != 1 {
		t.Fatalf("state=%s trips=%d, want OPEN + trips 1", b.State(), b.Trips())
	}
}
