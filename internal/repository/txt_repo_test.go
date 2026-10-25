package repository

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"img-api/internal/cache"
	"img-api/internal/logger"
	"img-api/internal/model"
)

// TestMain 集中初始化测试日志（替代 init：显式生命周期，避免与
// 其他并行测试包共享全局 logger 的隐式耦合）。
func TestMain(m *testing.M) {
	logger.L = slog.New(slog.NewTextHandler(io.Discard, nil))
	os.Exit(m.Run())
}

func TestReadTxtLines(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.txt")
	content := "# 注释\nhttps://a.com/1.jpg\n\nhttps://b.com/2.jpg\n"
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	lines, err := ReadTxtLines(p)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"https://a.com/1.jpg", "https://b.com/2.jpg"}
	if len(lines) != len(want) || lines[0] != want[0] || lines[1] != want[1] {
		t.Fatalf("ReadTxtLines = %v, want %v", lines, want)
	}
}

func TestTxtRepositoryRandom(t *testing.T) {
	root := t.TempDir()
	pc := filepath.Join(root, "pc")
	if err := os.MkdirAll(pc, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pc, "anime.txt"), []byte("https://a.com/1.jpg\nhttps://a.com/2.jpg\n"), 0644); err != nil {
		t.Fatal(err)
	}

	repo := NewTxtRepository(root, nil)
	ctx := context.Background()

	img, err := repo.Random(ctx, "anime", model.DevicePC)
	if err != nil {
		t.Fatal(err)
	}
	if img.URL != "https://a.com/1.jpg" && img.URL != "https://a.com/2.jpg" {
		t.Fatalf("unexpected URL %q", img.URL)
	}
	if img.Category != "anime" {
		t.Fatalf("category = %q, want anime", img.Category)
	}

	// PE 目录没有 anime → 报错（不是 panic）
	if _, err := repo.Random(ctx, "anime", model.DevicePE); err == nil {
		t.Fatal("expected error for missing pe/anime.txt")
	}
	// 空分类回退 default，default 文件不存在 → 报错
	if _, err := repo.Random(ctx, "", model.DevicePC); err == nil {
		t.Fatal("expected error for missing default")
	}
	// 文件内容缓存：修改文件后应感知变化（mtime 变化）
	if err := os.WriteFile(filepath.Join(pc, "anime.txt"), []byte("https://a.com/3.jpg\n"), 0644); err != nil {
		t.Fatal(err)
	}
	img2, err := repo.Random(ctx, "anime", model.DevicePC)
	if err != nil {
		t.Fatal(err)
	}
	if img2.URL != "https://a.com/3.jpg" {
		t.Fatalf("URL after file change = %q, want 3.jpg", img2.URL)
	}
}

// failCache 是仅用于测试的缓存桩：SRandMember 恒返回错误，其余方法来自内嵌接口。
type failCache struct {
	cache.Cache
	err   error
	calls int
}

func (f *failCache) SRandMember(ctx context.Context, key string) (string, error) {
	f.calls++
	return "", f.err
}

// TestTxtRepositoryRedisCooldown 回归 Z4：Redis 故障后进入冷却，
// 冷却期内直走文件通道（不再每次请求都等待 Redis 超时）。
func TestTxtRepositoryRedisCooldown(t *testing.T) {
	root := t.TempDir()
	pc := filepath.Join(root, "pc")
	if err := os.MkdirAll(pc, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pc, "anime.txt"), []byte("https://a.com/1.jpg\n"), 0644); err != nil {
		t.Fatal(err)
	}

	fc := &failCache{err: errors.New("redis down")}
	repo := NewTxtRepository(root, fc)
	ctx := context.Background()

	// 第一次请求：尝试 Redis 失败后回落文件，正常出图
	img, err := repo.Random(ctx, "anime", model.DevicePC)
	if err != nil {
		t.Fatal(err)
	}
	if img.URL != "https://a.com/1.jpg" {
		t.Fatalf("URL = %q, want fallback file url", img.URL)
	}
	if fc.calls != 1 {
		t.Fatalf("first request should try redis once, calls = %d", fc.calls)
	}

	// 冷却期内：直走文件，不再询问 Redis（此处只关心调用次数，不需要结果）
	_, err = repo.Random(ctx, "anime", model.DevicePC)
	if err != nil {
		t.Fatal(err)
	}
	if fc.calls != 1 {
		t.Fatalf("cooldown should skip redis, calls = %d", fc.calls)
	}
}

// hitCache 是 Redis 命中路径的测试桩：SRandMember 恒返回固定 URL。
type hitCache struct {
	cache.Cache
	url   string
	calls int
}

func (h *hitCache) SRandMember(ctx context.Context, key string) (string, error) {
	h.calls++
	return h.url, nil
}

// TestTxtRepositoryRedisHit Redis 快速通道命中且分类文件仍在时，直接返回 Set 成员，
// 不读文件内容（SRandMember O(1) 加速路径）。
//
// 注意文件里放的是另一个 URL：命中时返回 redis 成员即证明没有走文件内容。
func TestTxtRepositoryRedisHit(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pc"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pc", "anime.txt"), []byte("https://file.example/9.jpg\n"), 0644); err != nil {
		t.Fatal(err)
	}

	hc := &hitCache{url: "https://redis.example/1.jpg"}
	repo := NewTxtRepository(root, hc)
	ctx := context.Background()

	img, err := repo.Random(ctx, "anime", model.DevicePC)
	if err != nil {
		t.Fatal(err)
	}
	if img.URL != "https://redis.example/1.jpg" {
		t.Fatalf("URL = %q, want redis member", img.URL)
	}
	if hc.calls != 1 {
		t.Fatalf("redis calls = %d, want 1", hc.calls)
	}
}

// TestTxtRepositoryRedisHitRequiresExistingFile 回归 L-04(a)：Redis 里还留着集合，
// 但分类 .txt 已被删除/改名时，不得再返回该 URL（Set 没有 TTL，否则永久出图）。
// 命中被丢弃后应落到文件回退 → ErrNoImage，由 Handler 渲染 404 提示页。
func TestTxtRepositoryRedisHitRequiresExistingFile(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pc"), 0755); err != nil {
		t.Fatal(err)
	}

	hc := &hitCache{url: "https://redis.example/1.jpg"}
	repo := NewTxtRepository(root, hc)

	_, err := repo.Random(context.Background(), "anime", model.DevicePC)
	var noImg *model.ErrNoImage
	if !errors.As(err, &noImg) {
		t.Fatalf("文件已删除时应返回 ErrNoImage，实际 err = %v", err)
	}
	if hc.calls != 1 {
		t.Fatalf("redis calls = %d, want 1", hc.calls)
	}
}

// TestTxtRepositoryRedisHitRejectedWhenPathIsDir 同上：分类路径存在但不是普通文件
// （同名目录）时同样不能返回 Redis 成员。
func TestTxtRepositoryRedisHitRejectedWhenPathIsDir(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pc", "anime.txt"), 0755); err != nil {
		t.Fatal(err)
	}

	hc := &hitCache{url: "https://redis.example/1.jpg"}
	repo := NewTxtRepository(root, hc)

	img, err := repo.Random(context.Background(), "anime", model.DevicePC)
	if img != nil {
		t.Fatalf("分类不是普通文件时不应返回图片，实际 URL = %q", img.URL)
	}
	if err == nil {
		t.Fatal("分类不是普通文件时应返回错误")
	}
}
