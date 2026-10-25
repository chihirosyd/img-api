// Package logger 封装 slog 结构化日志。
//
// 特性：
//   - 双输出：控制台 + 按天轮转文件
//   - 可配置单文件大小上限（超限自动切分）
//   - 可配置保留天数（超期自动清理）
//   - 后台定时检查，不阻塞业务
package logger

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// L 是全局日志实例，Init() 后即可使用。
// 未调用 Init 时为 nil，调用方不应直接使用（app.NewServer 保证先 Init 再写日志）。
var L *slog.Logger

// logDir 和配置缓存（供后台 goroutine 使用）。
var (
	logDirCache string
	maxAgeDays  int
	maxSizeMB   int
)

// 后台清理协程的存活状态：Init 时启动，Close 时停止。
var (
	workerMu   sync.Mutex
	workerStop chan struct{} // 当前清理协程的停止信号（nil = 未启动）
	workerDone chan struct{} // 当前清理协程的退出信号
	activeFile *fileWriter   // 当前文件写入器（Close 与重复 Init 时关闭，释放文件句柄）
)

// fileWriter 将日志写入"今天"的日志文件，支持运行期轮转：
//   - 跨天时自动切换到新日期文件
//   - 超出 LOG_MAX_SIZE 时自动切分为 .1/.2 ... 备份（旧实现仅启动时检查一次）
//
// Write 与 rotateIfNeeded 通过互斥锁保护，切分瞬间不丢失写入。
type fileWriter struct {
	mu       sync.Mutex
	dir      string
	file     *os.File
	path     string
	fallback bool // 打开文件失败时降级为 stderr，不再轮转
	closed   bool // Close 之后不再写入（write 原样返回，避免向已关闭文件写）
}

// newFileWriter 创建文件写入器并打开今日日志文件。
// 打开失败时回退到 stderr（不阻塞服务启动），但会明确告知一次——
// 静默回退在 GUI 场景等于日志全丢且无人知晓。
func newFileWriter(dir string) *fileWriter {
	w := &fileWriter{dir: dir}
	if err := w.open(); err != nil {
		w.file = os.Stderr
		w.fallback = true
		fmt.Fprintf(os.Stderr, "⚠️  无法写入日志文件（目录 %q），已回退到 stderr：%v\n", dir, err)
	}
	return w
}

// Write 实现 io.Writer：先检查是否需要轮转，再写入当前文件。
func (w *fileWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	// Close 之后仍可能有并发的写日志调用：原样返回，不写已关闭的文件
	if w.closed {
		return len(p), nil
	}
	if err := w.rotateIfNeededLocked(); err != nil {
		return 0, err
	}
	return w.file.Write(p)
}

// Close 关闭当前日志文件并标记为已关闭（幂等）。
// 供进程优雅退出与测试使用：Windows 下未关闭的文件句柄会让后续删除失败。
func (w *fileWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	if w.file == nil || w.fallback {
		return nil // stderr 不归我们关；文件未打开时无需关闭
	}
	err := w.file.Close()
	w.file = nil
	return err
}

// rotateIfNeeded 对外供后台协程调用（自动加锁）。
func (w *fileWriter) rotateIfNeeded() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	_ = w.rotateIfNeededLocked()
}

// rotateIfNeededLocked 检查并执行轮转（调用方必须持有 w.mu）。
func (w *fileWriter) rotateIfNeededLocked() error {
	if w.fallback {
		return nil
	}
	today := filepath.Join(w.dir, time.Now().Format("2006-01-02")+".log")

	if w.file == nil {
		return w.open()
	}

	// 日期变更：关闭旧文件，打开新日期文件（旧文件保留原名）
	if w.path != today {
		_ = w.file.Close()
		w.file = nil
		return w.open()
	}

	// 大小超限：关闭后改名备份，再打开新文件
	if maxSizeMB > 0 {
		if info, err := os.Stat(w.path); err == nil && info.Size() >= int64(maxSizeMB)*1024*1024 {
			_ = w.file.Close()
			w.file = nil
			backupFile(w.path)
			return w.open()
		}
	}
	return nil
}

// open 打开"今天"的日志文件（调用方必须持有 w.mu）。
func (w *fileWriter) open() error {
	w.path = filepath.Join(w.dir, time.Now().Format("2006-01-02")+".log")
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	w.file = f
	return nil
}

// backupFile 将日志文件重命名为 path.1、path.2 ...（首个不存在的编号）。
func backupFile(path string) {
	for i := 1; i < 100; i++ {
		backup := path + "." + strconv.Itoa(i)
		if _, err := os.Stat(backup); os.IsNotExist(err) {
			_ = os.Rename(path, backup)
			return
		}
	}
}

// Init 初始化日志系统。
//
// level: debug / info / warn / error
// logDir: 日志文件目录
// maxAge: 保留天数（0=永久）
// sizeMB: 单文件最大 MB（0=不限制）
func Init(level, logDir string, maxAge, sizeMB int) error {
	// 进程内重复 Init：先停掉上一次的清理协程、关闭旧的文件写入器，
	// 再改写下面被协程读取的全局配置（顺序颠倒会让二者并发读写）；
	// 漏掉这一步则每轮 Init 都泄漏一个 goroutine 与一个日志文件句柄。
	stopWorker()
	closeActiveFile()

	logDirCache = logDir
	maxAgeDays = maxAge
	maxSizeMB = sizeMB

	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	if err := os.MkdirAll(logDir, 0755); err != nil {
		return err
	}

	// 文件写入器：运行期支持跨天切换与大小切分
	fw := newFileWriter(logDir)

	stopCh := make(chan struct{})
	doneCh := make(chan struct{})
	workerMu.Lock()
	workerStop = stopCh
	workerDone = doneCh
	activeFile = fw
	workerMu.Unlock()

	// 启动时执行一次清理，之后每 5 分钟：清理过期日志 + 检查轮转
	go func() {
		defer close(doneCh)
		cleanupLogs()
		fw.rotateIfNeeded()
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				cleanupLogs()
				fw.rotateIfNeeded()
			case <-stopCh:
				return
			}
		}
	}()

	multi := io.MultiWriter(os.Stdout, fw)

	opts := &slog.HandlerOptions{
		Level:     lvl,
		AddSource: lvl == slog.LevelDebug,
	}

	handler := slog.NewJSONHandler(multi, opts)
	L = slog.New(handler)
	slog.SetDefault(L)
	return nil
}

// Close 停止后台清理协程并关闭日志文件（幂等）。
//
// 用途：进程优雅退出，以及需要删除日志文件的测试——Windows 下未关闭的文件句柄
// 会让删除失败。关闭后 L 仍可写（写入被丢弃），不会 panic。
func Close() {
	stopWorker()
	closeActiveFile()
}

// closeActiveFile 关闭当前文件写入器（幂等；未初始化时为空操作）。
//
// 先取出并置 nil，再在不持有 workerMu 的情况下关文件（避免与 fileWriter.mu 形成锁序），
// 同一写入器因此只会被取出一次；fileWriter.Close 自身也幂等，
// 所以 Init 与 Close 竞争时不会重复关闭同一个 *os.File。
func closeActiveFile() {
	workerMu.Lock()
	fw := activeFile
	activeFile = nil
	workerMu.Unlock()
	if fw != nil {
		_ = fw.Close()
	}
}

// stopWorker 停止当前后台清理协程并等待其退出（幂等；未启动时为空操作）。
func stopWorker() {
	workerMu.Lock()
	stopCh, doneCh := workerStop, workerDone
	workerStop, workerDone = nil, nil
	if stopCh != nil {
		close(stopCh)
	}
	workerMu.Unlock()
	if doneCh != nil {
		<-doneCh
	}
}

// cleanupLogs 清理过期日志 + 大小超限的当日日志。
func cleanupLogs() {
	entries, err := os.ReadDir(logDirCache)
	if err != nil {
		return
	}

	cutoff := time.Now().Add(-time.Duration(maxAgeDays) * 24 * time.Hour)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		datePart := name
		if len(name) > 10 && name[10] == '.' {
			datePart = name[:10]
		}
		if len(datePart) < 10 {
			continue
		}
		t, err := time.Parse("2006-01-02", datePart[:10])
		if err != nil {
			continue
		}
		if maxAgeDays > 0 && t.Before(cutoff) {
			_ = os.Remove(filepath.Join(logDirCache, name))
		}
	}
}
