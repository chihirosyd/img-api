package cache

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"img-api/internal/logger"
)

func init() {
	// NewMemoryCache 等会写日志，测试中指向丢弃设备
	logger.L = slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestMemoryCacheGetSetDelete(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryCache()

	if _, err := m.Get(ctx, "k"); !errors.Is(err, Nil) {
		t.Fatalf("Get(missing) err = %v, want Nil", err)
	}
	if err := m.Set(ctx, "k", []byte("v"), time.Minute); err != nil {
		t.Fatal(err)
	}
	got, err := m.Get(ctx, "k")
	if err != nil || string(got) != "v" {
		t.Fatalf("Get = %q, %v, want v", got, err)
	}
	if err := m.Delete(ctx, "k"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Get(ctx, "k"); !errors.Is(err, Nil) {
		t.Fatalf("Get(after delete) err = %v, want Nil", err)
	}
}

func TestMemoryCacheExpiry(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryCache()
	if err := m.Set(ctx, "k", []byte("v"), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if _, err := m.Get(ctx, "k"); !errors.Is(err, Nil) {
		t.Fatalf("Get(expired) err = %v, want Nil", err)
	}
}

func TestMemoryCacheSetOps(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryCache()
	if err := m.SAdd(ctx, "s", "a", "b", "a"); err != nil {
		t.Fatal(err)
	}
	if n, _ := m.SCard(ctx, "s"); n != 2 {
		t.Fatalf("SCard = %d, want 2", n)
	}
	// SRandMember 多次调用都应返回集合成员
	for i := 0; i < 20; i++ {
		v, err := m.SRandMember(ctx, "s")
		if err != nil || (v != "a" && v != "b") {
			t.Fatalf("SRandMember = %q, %v", v, err)
		}
	}
	if _, err := m.SRandMember(ctx, "missing"); !errors.Is(err, Nil) {
		t.Fatalf("SRandMember(missing) err = %v, want Nil", err)
	}
}
