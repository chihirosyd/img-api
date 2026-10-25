package logger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// onlyLogFile 返回 dir 下唯一的 .log 文件路径。
// 按目录列举而不是按日期拼名字，避免测试跨零点时路径与实际写入的文件不一致。
func onlyLogFile(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取日志目录失败：%v", err)
	}
	var found []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".log") {
			found = append(found, filepath.Join(dir, e.Name()))
		}
	}
	if len(found) != 1 {
		t.Fatalf("日志目录 %q 下的 .log 文件数 = %d（%v），期望 1 个", dir, len(found), found)
	}
	return found[0]
}

// TestInitTwiceReleasesOldFileHandle 回归测试（对应 CODE_REVIEW N-05）。
//
// 修复前的行为：第二次 Init 只停掉清理协程，从不关闭上一次的文件写入器，
// 旧 *os.File 句柄泄漏到进程结束（GUI 每次「启动/保存设置并重启」都会 NewServer
// ⇒ 每次都漏一个句柄），而 Close() 是唯一关闭句柄的路径，Shutdown 却从不调用它。
//
// Windows 下被打开的文件无法删除，因此用「删除旧日志文件是否成功」断言句柄已释放。
func TestInitTwiceReleasesOldFileHandle(t *testing.T) {
	oldDir, newDir := t.TempDir(), t.TempDir()
	// 后注册先执行：Close 必须先于 t.TempDir 的 RemoveAll，否则 Windows 下目录删不掉
	t.Cleanup(Close)

	if err := Init("info", oldDir, 0, 0); err != nil {
		t.Fatalf("首次 Init 失败：%v", err)
	}
	L.Info("首次 Init 的日志")
	oldPath := onlyLogFile(t, oldDir)

	if err := Init("info", newDir, 0, 0); err != nil {
		t.Fatalf("第二次 Init 失败：%v", err)
	}
	L.Info("第二次 Init 的日志")

	// 旧句柄必须已释放，否则这里报 "The process cannot access the file..."
	if err := os.Remove(oldPath); err != nil {
		t.Fatalf("第二次 Init 后旧日志文件仍被占用（旧文件句柄未关闭）：%v", err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("旧日志文件应已被删除，stat err = %v", err)
	}

	// 新写入器仍正常工作：日志落到新目录
	data, err := os.ReadFile(onlyLogFile(t, newDir))
	if err != nil {
		t.Fatalf("读取新日志文件失败：%v", err)
	}
	if !strings.Contains(string(data), "第二次 Init 的日志") {
		t.Errorf("新日志文件未包含第二次 Init 后写入的内容：%q", data)
	}
}

// TestInitTwiceSameDirCloseIdempotent 覆盖同一目录重复 Init 与多次 Close：
// Init 已经关闭的旧写入器不会被后续 Close() 再关一次（activeFile 已置 nil），
// Close 之后所有文件句柄释放，日志文件可删除。
func TestInitTwiceSameDirCloseIdempotent(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(Close)

	if err := Init("info", dir, 0, 0); err != nil {
		t.Fatalf("首次 Init 失败：%v", err)
	}
	if err := Init("info", dir, 0, 0); err != nil {
		t.Fatalf("第二次 Init 失败：%v", err)
	}
	L.Info("重复 Init 之后的日志")
	path := onlyLogFile(t, dir)

	Close()
	Close() // 幂等：第二次为空操作，不得重复关闭同一个 *os.File

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取日志文件失败：%v", err)
	}
	if !strings.Contains(string(data), "重复 Init 之后的日志") {
		t.Errorf("日志文件未包含重复 Init 之后写入的内容：%q", data)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("Close 后日志文件仍被占用（句柄未释放）：%v", err)
	}
}
