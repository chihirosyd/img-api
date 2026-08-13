package circuit

import (
	"errors"
	"testing"
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
