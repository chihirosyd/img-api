package cache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"img-api/internal/logger"
)

// TestMain 集中初始化测试日志（替代 init：显式生命周期，避免与
// 其他并行测试包共享全局 logger 的隐式耦合）。
func TestMain(m *testing.M) {
	logger.L = slog.New(slog.NewTextHandler(io.Discard, nil))
	os.Exit(m.Run())
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

// TestMemoryCacheExpiredDeleteKeepsFreshValue 回归 Z1：
// 过期条目的删除必须按条目身份（CompareAndDelete），
// 不能把并发 Set 刚写入的新值误删，也不能双扣计数。
func TestMemoryCacheExpiredDeleteKeepsFreshValue(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryCache()
	defer m.Close()

	if err := m.Set(ctx, "k", []byte("old"), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if _, err := m.Get(ctx, "k"); !errors.Is(err, Nil) {
		t.Fatalf("Get(expired) err = %v, want Nil", err)
	}

	// 过期删除后写入新值，必须能正常读到
	if err := m.Set(ctx, "k", []byte("fresh"), 0); err != nil {
		t.Fatal(err)
	}
	got, err := m.Get(ctx, "k")
	if err != nil || string(got) != "fresh" {
		t.Fatalf("fresh value lost: %q, %v", got, err)
	}
	if s := m.Size(); s != 1 {
		t.Fatalf("Size() = %d, want 1（过期删除+重新写入不能双扣计数）", s)
	}
}

// TestMemoryCacheSetOverwriteKeepsCount 覆盖写入同一 key 不重复计数。
func TestMemoryCacheSetOverwriteKeepsCount(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryCache()
	defer m.Close()

	for i := 0; i < 3; i++ {
		if err := m.Set(ctx, "k", []byte(fmt.Sprintf("v%d", i)), 0); err != nil {
			t.Fatal(err)
		}
	}
	if s := m.Size(); s != 1 {
		t.Fatalf("Size() = %d, want 1", s)
	}
	got, err := m.Get(ctx, "k")
	if err != nil || string(got) != "v2" {
		t.Fatalf("Get = %q, %v, want newest v2", got, err)
	}
}

// TestMemoryCacheConcurrentSetGetExpire 并发 Set/Get/过期删除的压测
// （配合 -race 验证无竞态，计数不为负）。
func TestMemoryCacheConcurrentSetGetExpire(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryCache()
	defer m.Close()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				k := fmt.Sprintf("k%d", (i+j)%16)
				_ = m.Set(ctx, k, []byte("v"), 2*time.Millisecond)
				_, _ = m.Get(ctx, k)
			}
		}(i)
	}
	wg.Wait()
	if s := m.Size(); s < 0 {
		t.Fatalf("Size() = %d, count must not go negative", s)
	}
}

// TestMemoryCacheCloseIdempotent Close 可重复调用且不 panic。
func TestMemoryCacheCloseIdempotent(t *testing.T) {
	m := NewMemoryCache()
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestMemoryCacheEviction 容量上限生效：超过 maxItems 后条目数不再增长。
func TestMemoryCacheEviction(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryCache()
	defer m.Close()
	m.maxItems = 3

	for i := 0; i < 20; i++ {
		if err := m.Set(ctx, fmt.Sprintf("k%d", i), []byte("v"), 0); err != nil {
			t.Fatal(err)
		}
	}
	if s := m.Size(); s > 3 {
		t.Fatalf("Size() = %d, want <= maxItems 3（淘汰未生效）", s)
	}
}

// TestMemoryCacheNegativeTTL 负 ttl 视为永不过期（内存实现语义；
// 与 Redis 的差异已在 interface 文档声明）。
func TestMemoryCacheNegativeTTL(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryCache()
	defer m.Close()

	if err := m.Set(ctx, "k", []byte("v"), -time.Minute); err != nil {
		t.Fatal(err)
	}
	got, err := m.Get(ctx, "k")
	if err != nil || string(got) != "v" {
		t.Fatalf("Get = %q, %v, want v（负 ttl 应视为永不过期）", got, err)
	}
}
