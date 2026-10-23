package app

import (
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
