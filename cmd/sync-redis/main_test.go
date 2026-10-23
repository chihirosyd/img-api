package main

import (
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

	n := syncDevice(context.Background(), c, dir, "pc")
	if n != 2 {
		t.Fatalf("total = %d, want 2", n)
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
