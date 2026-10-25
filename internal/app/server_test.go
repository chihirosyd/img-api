package app

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"img-api/internal/logger"
)

// TestNewServerLoggerInitFailureIsRetryable 回归测试（对应 CODE_REVIEW 附录 A 的 D1 / 路线图 P4-1）。
//
// 修复前的行为：首次 logger.Init 失败后 logger.L 保持 nil，而 sync.Once 已被消费，
// 第二次 NewServer 会跳过 Init 且略过错误分支 → 在 logger.L.Info 处空指针 panic
// （GUI「启动服务」按钮点两次即崩）。
//
// 修复后的契约：
//  1. 首次失败必须返回 error（不得 panic）；
//  2. 第二次调用必须重试 Init —— 条件恢复后正常返回，而不是继续用 nil logger；
//  3. 同一根目录重复调用不重复 Init（避免文件写入器与清理协程泄漏）。
//
// 注 1：使用 os.Setenv + t.Cleanup 而非 t.Setenv —— 后者在子测试结束后立即恢复环境变量，
// 无法表达「同一测试内先失败后成功」的两次调用序列。本包无并行测试，进程级环境变量安全。
// 注 2：清理时先 logger.Close() 释放日志文件句柄 —— Windows 下被打开的文件无法删除，
// 否则 t.TempDir 的 RemoveAll 会报 "file is being used by another process" 而判失败。
func TestNewServerLoggerInitFailureIsRetryable(t *testing.T) {
	root := t.TempDir()

	// 用一个普通文件占据 storage/logs 的父路径，使 os.MkdirAll(LOG_DIR) 必然失败
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldLogDir, hadLogDir := os.LookupEnv("LOG_DIR")
	t.Cleanup(func() {
		// 先停清理协程并关闭日志文件，再让 t.TempDir 删除目录
		logger.Close()
		if hadLogDir {
			os.Setenv("LOG_DIR", oldLogDir)
		} else {
			os.Unsetenv("LOG_DIR")
		}
	})

	// ── 场景 1：日志目录不可创建 → 必须返回 error，且不得 panic ──
	os.Setenv("LOG_DIR", filepath.Join("blocker", "logs"))
	srv, err := NewServer(root)
	if err == nil {
		t.Fatalf("日志目录不可创建时 NewServer 应返回 error，实际 err=nil srv=%v", srv)
	}
	t.Logf("场景1（不可创建）err = %v", err)

	// ── 场景 2：条件恢复后重试 → 必须成功（修复前此处 panic）──
	os.Setenv("LOG_DIR", "storage/logs")
	srv2, err2 := NewServer(root)
	if err2 != nil {
		t.Fatalf("首次失败后条件恢复，第二次 NewServer 应成功并重试 Init，实际 err=%v", err2)
	}
	if srv2 == nil {
		t.Fatal("第二次 NewServer 返回 nil server 且无 error")
	}
	if loggerInitDir != root {
		t.Fatalf("loggerInitDir = %q, 期望 %q（成功后应记住根目录，避免重复 Init）", loggerInitDir, root)
	}
	t.Logf("场景2（恢复后重试）成功，loggerInitDir 已记录")

	// ── 场景 3：同一根目录重复调用不应重复 Init ──
	if _, err3 := NewServer(root); err3 != nil {
		t.Fatalf("同一根目录重复调用 NewServer 应复用已初始化的日志系统，实际 err=%v", err3)
	}
}

// countingCloser 是只用于观测 Close 调用次数的 io.Closer 替身，
// 不持有任何真实资源，因此可在未启动的 Server 上安全复用。
// 计数器为普通 int：cleanup 在 Shutdown 内同步执行，无需原子操作。
type countingCloser struct {
	closes int
}

func (c *countingCloser) Close() error {
	c.closes++
	return nil
}

// TestShutdownCleansUpWithoutStart 回归测试（缺陷 N-01）。
//
// 修复前的行为：Shutdown 开头有 if !s.running.Load() { return nil }，
// 从未 Start（或 Start 失败）的 Server 直接返回，limiterStop / svc.Close /
// cacheCloser.Close 一个都不执行——NewServer 在监听之前就已创建的限流清理协程
// 与缓存清理协程全部泄漏（GUI 端口被占用时反复点「启动」即逐次累积）。
//
// 修复后的契约：
//  1. 未 Start 的 Server 调用 Shutdown 也必须无条件执行 cleanup；
//  2. limiterStop 恰好被调用 1 次，cacheCloser.Close 恰好被调用 1 次；
//  3. Shutdown 返回 nil（未运行时没有 HTTP 关闭错误可报）。
//
// 注：本测试刻意不初始化 logger —— 未运行的 Server 走 Shutdown 不会进 wasRunning
// 分支，因此不依赖 logger.L 非 nil。
func TestShutdownCleansUpWithoutStart(t *testing.T) {
	limiterStops := 0
	cache := &countingCloser{}
	srv := &Server{
		limiterStop: func() { limiterStops++ },
		cacheCloser: cache,
	}

	if err := srv.Shutdown(context.Background()); err != nil {
		t.Fatalf("未启动的 Server 调用 Shutdown 应返回 nil，实际 err=%v", err)
	}
	if limiterStops != 1 {
		t.Fatalf("limiterStop 调用次数 = %d，期望 1（修复前为 0，即限流清理协程泄漏）", limiterStops)
	}
	if cache.closes != 1 {
		t.Fatalf("cacheCloser.Close 调用次数 = %d，期望 1（修复前为 0，即缓存清理协程泄漏）", cache.closes)
	}
	if srv.Running() {
		t.Fatal("未启动的 Server 经 Shutdown 后 Running() 应为 false")
	}
}

// TestShutdownCleanupIsIdempotent 回归测试（缺陷 N-01 的配套保底）。
//
// 修复前的行为：Shutdown 以 running 作为「是否已善后」的判据，
// 而 running 在 Start 失败路径上从未被置位，于是每次点击「停止」/
// 「启动」失败后的善后都会重复执行释放逻辑——对关闭两次的缓存连接
// 重复 Close、对已停止的限流协程重复停止，属于未定义行为。
//
// 修复后的契约：
//  1. Shutdown 可安全重复调用，后台资源只释放一次（sync.Once 生效）；
//  2. 连续两次 Shutdown 后两个计数器都停在 1，而不是 2；
//  3. 两次调用都返回 nil。
func TestShutdownCleanupIsIdempotent(t *testing.T) {
	limiterStops := 0
	cache := &countingCloser{}
	srv := &Server{
		limiterStop: func() { limiterStops++ },
		cacheCloser: cache,
	}

	for i := 1; i <= 2; i++ {
		if err := srv.Shutdown(context.Background()); err != nil {
			t.Fatalf("第 %d 次 Shutdown 应返回 nil，实际 err=%v", i, err)
		}
	}
	if limiterStops != 1 {
		t.Fatalf("limiterStop 调用次数 = %d，期望 1（sync.Once 失效会重复停止清理协程）", limiterStops)
	}
	if cache.closes != 1 {
		t.Fatalf("cacheCloser.Close 调用次数 = %d，期望 1（sync.Once 失效会重复关闭缓存连接）", cache.closes)
	}
}

// TestShutdownAfterFailedStartCleansUp 回归测试（缺陷 N-01 的真实路径）。
//
// 完整复现 GUI 场景：先占住一个端口，再用该地址构造 Server，Start 必然因
// 端口占用返回 error（running 保持 false），随后调用方仍会调用 Shutdown 善后。
//
// 修复前的行为：Start 失败后调用 Shutdown，因 running=false 直接 return nil，
// limiterStop / cacheCloser.Close 均为 0 次——用户每点一次「启动」就泄漏一组
// 限流清理协程 + 缓存清理协程（Redis 场景还含连接），直至进程耗尽资源。
//
// 修复后的契约：
//  1. Start 必须返回非 nil error（端口占用如实上报，不得吞错）；
//  2. 该失败路径上的 Shutdown 同样完成清理：两个计数器各 1 次；
//  3. Shutdown 返回 nil（未进入 Serve，不存在 HTTP 关闭错误）。
func TestShutdownAfterFailedStartCleansUp(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("占位监听失败：%v", err)
	}
	defer occupied.Close()
	addr := occupied.Addr().String()

	limiterStops := 0
	cache := &countingCloser{}
	srv := &Server{
		httpSrv:     &http.Server{Addr: addr},
		limiterStop: func() { limiterStops++ },
		cacheCloser: cache,
	}

	if err := srv.Start(); err == nil {
		t.Fatalf("端口 %s 已被占用，Start 应返回 error，实际 err=nil", addr)
	} else {
		t.Logf("Start 在端口 %s 上按预期失败：%v", addr, err)
	}
	if srv.Running() {
		t.Fatal("Start 失败后 Running() 应为 false")
	}

	if err := srv.Shutdown(context.Background()); err != nil {
		t.Fatalf("Start 失败后的 Shutdown 应返回 nil，实际 err=%v", err)
	}
	if limiterStops != 1 {
		t.Fatalf("Start 失败路径上 limiterStop 调用次数 = %d，期望 1（修复前为 0，即限流清理协程泄漏）", limiterStops)
	}
	if cache.closes != 1 {
		t.Fatalf("Start 失败路径上 cacheCloser.Close 调用次数 = %d，期望 1（修复前为 0，即缓存清理协程泄漏）", cache.closes)
	}
}
