package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// fakeSetCache 是内存版 setCache：不依赖真实 Redis 即可验证同步与清理逻辑。
type fakeSetCache struct {
	sets    map[string][]string
	saddErr bool // true 时 SAdd 返回错误（模拟网络抖动）
}

func newFakeSetCache() *fakeSetCache {
	return &fakeSetCache{sets: make(map[string][]string)}
}

func (f *fakeSetCache) Delete(_ context.Context, key string) error {
	delete(f.sets, key)
	return nil
}

func (f *fakeSetCache) SAdd(_ context.Context, key string, members ...string) error {
	if f.saddErr {
		return errors.New("sadd failed")
	}
	f.sets[key] = append([]string(nil), members...)
	return nil
}

func (f *fakeSetCache) SCard(_ context.Context, key string) (int64, error) {
	return int64(len(f.sets[key])), nil
}

func (f *fakeSetCache) ScanKeys(_ context.Context, pattern string) ([]string, error) {
	prefix := strings.TrimSuffix(pattern, "*")
	var keys []string
	for k := range f.sets {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

func (f *fakeSetCache) Rename(_ context.Context, oldKey, newKey string) error {
	members, ok := f.sets[oldKey]
	if !ok {
		return errors.New("no such key")
	}
	f.sets[newKey] = members
	delete(f.sets, oldKey)
	return nil
}

// writeTxt 在 dir 下写一个 txt 文件（lines 每行一条）。
func writeTxt(t *testing.T, dir, name string, lines ...string) {
	t.Helper()
	content := ""
	if len(lines) > 0 {
		content = strings.Join(lines, "\n") + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestSyncDeviceReplacesSetAtomically 正常路径：旧集合被整体替换，临时键不残留。
func TestSyncDeviceReplacesSetAtomically(t *testing.T) {
	dir := t.TempDir()
	writeTxt(t, dir, "anime.txt", "https://a/1.jpg", "https://a/2.jpg")
	c := newFakeSetCache()
	c.sets["img:pc:anime"] = []string{"https://old/x.jpg"}

	st := syncDevice(context.Background(), c, dir, "pc")
	if st.urls != 2 {
		t.Fatalf("urls = %d, want 2", st.urls)
	}
	if st.failures != 0 {
		t.Fatalf("failures = %d, want 0", st.failures)
	}
	got := c.sets["img:pc:anime"]
	if len(got) != 2 || got[0] != "https://a/1.jpg" || got[1] != "https://a/2.jpg" {
		t.Fatalf("集合未替换: %v", got)
	}
	if _, ok := c.sets["img:pc:anime:tmp"]; ok {
		t.Fatal("临时键未清理")
	}
}

// TestSyncDeviceKeepsSetOnSAddFailure SAdd 失败时旧集合必须原样保留。
// 回归：旧实现「先 Delete 再 SAdd」在这条路径上会把线上集合清空。
func TestSyncDeviceKeepsSetOnSAddFailure(t *testing.T) {
	dir := t.TempDir()
	writeTxt(t, dir, "anime.txt", "https://a/1.jpg")
	c := newFakeSetCache()
	c.saddErr = true
	c.sets["img:pc:anime"] = []string{"https://old/x.jpg"}

	_ = syncDevice(context.Background(), c, dir, "pc")
	got := c.sets["img:pc:anime"]
	if len(got) != 1 || got[0] != "https://old/x.jpg" {
		t.Fatalf("SAdd 失败后旧集合被改动: %v", got)
	}
}

// TestSyncDeviceKeepsSetWhenReadFails 读取失败（这里是单行超过 1MB 的行上限）
// 不等于分类已删除：旧集合不能被孤儿清理删掉。回归 R5-04 的核心风险。
func TestSyncDeviceKeepsSetWhenReadFails(t *testing.T) {
	dir := t.TempDir()
	// 1MB 行上限由 ReadTxtLines 的 scanner.Buffer 决定，超限返回错误
	writeTxt(t, dir, "broken.txt", "https://example.com/"+strings.Repeat("a", 1024*1024+10))
	c := newFakeSetCache()
	c.sets["img:pc:broken"] = []string{"https://old/x.jpg"}

	_ = syncDevice(context.Background(), c, dir, "pc")
	got := c.sets["img:pc:broken"]
	if len(got) != 1 || got[0] != "https://old/x.jpg" {
		t.Fatalf("读取失败的分类被误删: %v", got)
	}
}

// TestSyncDeviceDeletesSetForEmptyCategory 文件存在但没有有效 URL = 分类已空，
// 旧集合应被孤儿清理删除。
func TestSyncDeviceDeletesSetForEmptyCategory(t *testing.T) {
	dir := t.TempDir()
	writeTxt(t, dir, "empty.txt", "# 只有注释", "")
	c := newFakeSetCache()
	c.sets["img:pc:empty"] = []string{"https://old/x.jpg"}

	_ = syncDevice(context.Background(), c, dir, "pc")
	if _, ok := c.sets["img:pc:empty"]; ok {
		t.Fatal("空分类的旧集合应被清理")
	}
}

// TestSyncDeviceSkipsCleanupWhenNoTxt 一个 .txt 都没有（多是 -dir 指错）→
// 不能据此删除任何集合。
func TestSyncDeviceSkipsCleanupWhenNoTxt(t *testing.T) {
	dir := t.TempDir()
	c := newFakeSetCache()
	c.sets["img:pc:keep"] = []string{"https://old/x.jpg"}

	_ = syncDevice(context.Background(), c, dir, "pc")
	if _, ok := c.sets["img:pc:keep"]; !ok {
		t.Fatal("-dir 指错时不得删除任何集合")
	}
}

// TestSyncDeviceNoTxtReportsFailure 回归 L-04(b)+C-04：目录可读但一个 .txt 都没有时，
// 既不能清理任何集合（"看不到分类"≠"分类已删光"），也不能静默成功——必须报告失败，
// 让进程以非零退出码结束，否则孤儿键会一直留在 Redis 里且无人知晓。
func TestSyncDeviceNoTxtReportsFailure(t *testing.T) {
	dir := t.TempDir()
	c := newFakeSetCache()
	c.sets["img:pc:keep"] = []string{"https://old/x.jpg"}

	st := syncDevice(context.Background(), c, dir, "pc")
	if st.failures == 0 {
		t.Fatal("没有任何 .txt 时应报告失败（退出码需要非零）")
	}
	if st.urls != 0 {
		t.Fatalf("urls = %d, want 0", st.urls)
	}
	if _, ok := c.sets["img:pc:keep"]; !ok {
		t.Fatal("没有任何 .txt 时不得删除任何集合")
	}
}

// TestSyncDeviceUnreadableDirReportsFailure 目录本身读不到（-dir 指错、权限、挂载掉线）
// 必须与"目录正常但没有 txt"一样报告失败，且绝不能删除数据（C-04）。
func TestSyncDeviceUnreadableDirReportsFailure(t *testing.T) {
	c := newFakeSetCache()
	c.sets["img:pc:keep"] = []string{"https://old/x.jpg"}

	st := syncDevice(context.Background(), c, filepath.Join(t.TempDir(), "missing"), "pc")
	if st.failures == 0 {
		t.Fatal("目录读不到时应报告失败（退出码需要非零）")
	}
	if _, ok := c.sets["img:pc:keep"]; !ok {
		t.Fatal("目录读不到时不得删除任何集合")
	}
}

// TestSyncDeviceUsesOriginalFileNameAsKey 回归 L-01(c)：Redis 键必须用原始文件名
// （只去掉 .txt 后缀、不转小写），与主服务用请求原样分类名拼 key 的口径一致。
func TestSyncDeviceUsesOriginalFileNameAsKey(t *testing.T) {
	dir := t.TempDir()
	writeTxt(t, dir, "Anime.txt", "https://a/1.jpg")
	c := newFakeSetCache()

	st := syncDevice(context.Background(), c, dir, "pc")
	if st.urls != 1 || st.failures != 0 {
		t.Fatalf("deviceSync = {urls:%d failures:%d}, want {1 0}", st.urls, st.failures)
	}
	if _, ok := c.sets["img:pc:Anime"]; !ok {
		t.Fatalf("键必须使用原始文件名 img:pc:Anime，实际键: %v", c.sets)
	}
	if _, ok := c.sets["img:pc:anime"]; ok {
		t.Fatal("不应再写入小写化后的键 img:pc:anime")
	}
}

// TestSyncDeviceReportsPerCategoryFailure 一个分类成功 + 一个分类读取失败 =
// 部分成功：urls>0 且 failures>0，汇总退出码为 2。
func TestSyncDeviceReportsPerCategoryFailure(t *testing.T) {
	dir := t.TempDir()
	writeTxt(t, dir, "anime.txt", "https://a/1.jpg")
	writeTxt(t, dir, "broken.txt", "https://example.com/"+strings.Repeat("a", 1024*1024+10))
	c := newFakeSetCache()

	st := syncDevice(context.Background(), c, dir, "pc")
	if st.urls != 1 || st.failures != 1 {
		t.Fatalf("deviceSync = {urls:%d failures:%d}, want {1 1}", st.urls, st.failures)
	}
	// deviceSync 与 runStats 字段完全一致，直接类型转换（staticcheck S1016）
	code := runStats(st).exitCode()
	if code != exitPartial {
		t.Fatalf("exitCode = %d, want %d（部分成功）", code, exitPartial)
	}
}

// TestRunStatsExitCodes 覆盖 C-04 的退出码契约与汇总行口径（无需真实 Redis）。
func TestRunStatsExitCodes(t *testing.T) {
	cases := []struct {
		name     string
		st       runStats
		wantCode int
		wantIn   string // 汇总行必须包含
		notIn    string // 汇总行不得包含
	}{
		{"全部成功", runStats{urls: 12}, exitOK, "🎉 同步完成", ""},
		{"部分成功", runStats{urls: 5, failures: 1}, exitPartial, "部分成功", "🎉"},
		{"全部失败", runStats{failures: 2}, exitFatal, "同步失败", "🎉"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.st.exitCode(); got != tc.wantCode {
				t.Fatalf("exitCode = %d, want %d", got, tc.wantCode)
			}
			line := tc.st.summaryLine()
			if !strings.Contains(line, tc.wantIn) {
				t.Fatalf("汇总行 %q 应包含 %q", line, tc.wantIn)
			}
			if tc.notIn != "" && strings.Contains(line, tc.notIn) {
				t.Fatalf("汇总行 %q 不应包含 %q", line, tc.notIn)
			}
		})
	}
}

// TestRunHelpExitsZero -h 是主动查看用法，不属于失败。
func TestRunHelpExitsZero(t *testing.T) {
	t.Setenv("APP_ROOT", t.TempDir())
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-h"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("-h 应返回 0，实际 %d", code)
	}
}

// TestRunReturnsNonZeroWhenRedisNotConfigured C-04：Redis 未配置必须返回非零退出码。
// 旧实现提示后 os.Exit(0)，脚本/CI 无法感知"其实什么都没同步"。
func TestRunReturnsNonZeroWhenRedisNotConfigured(t *testing.T) {
	t.Setenv("APP_ROOT", t.TempDir()) // 隔离 .env 与日志目录，避免往仓库里写文件
	t.Setenv("REDIS_ADDR", "")

	var stdout, stderr bytes.Buffer
	code := run([]string{"-dir", t.TempDir()}, &stdout, &stderr)
	if code != exitFatal {
		t.Fatalf("Redis 未配置时退出码 = %d, want %d", code, exitFatal)
	}
	if !strings.Contains(stderr.String(), "Redis 未配置") {
		t.Fatalf("stderr 未说明 Redis 未配置: %q", stderr.String())
	}
}
