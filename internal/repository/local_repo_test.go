package repository

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"img-api/internal/model"
)

func TestScanLocalImages(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "pc", "default")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.jpg"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("not an image"), 0644); err != nil {
		t.Fatal(err)
	}

	images, err := ScanLocalImages(root)
	if err != nil {
		t.Fatal(err)
	}
	files := images["pc/default"]
	if len(files) != 1 || files[0] != filepath.Join(dir, "a.jpg") {
		t.Fatalf("ScanLocalImages = %v, want only a.jpg", images)
	}
}

// TestScanLocalImagesMissingRoot 根目录不存在视为"暂无图片"（首次启动正常场景），
// 返回空索引 + nil 错误（允许写空索引，而非视为扫描失败）。
func TestScanLocalImagesMissingRoot(t *testing.T) {
	images, err := ScanLocalImages(filepath.Join(t.TempDir(), "not-exist"))
	if err != nil {
		t.Fatalf("missing root should not be an error, got %v", err)
	}
	if len(images) != 0 {
		t.Fatalf("missing root should return empty index, got %v", images)
	}
}

func TestLocalRepositoryIndexAndRandom(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "pc", "default")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.jpg"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	indexFile := filepath.Join(t.TempDir(), "local.json")
	repo := NewLocalRepository(root, indexFile)

	ctx := context.Background()
	img, err := repo.Random(ctx, "default", model.DevicePC)
	if err != nil {
		t.Fatal(err)
	}
	if img.URL != filepath.Join(dir, "a.jpg") {
		t.Fatalf("URL = %q, want a.jpg", img.URL)
	}

	// 索引文件应已原子生成，且可被新实例重新加载
	if _, err := os.Stat(indexFile); err != nil {
		t.Fatalf("index not written: %v", err)
	}
	repo2 := NewLocalRepository(root, indexFile)
	img2, err := repo2.Random(ctx, "default", model.DevicePC)
	if err != nil {
		t.Fatal(err)
	}
	if img2.URL != img.URL {
		t.Fatal("reloaded index mismatch")
	}

	// 缺失分类 → 报错（不是 panic）
	if _, err := repo.Random(ctx, "missing", model.DevicePC); err == nil {
		t.Fatal("expected error for missing category")
	}
}

// TestLocalRepositoryFallbackScanRecursive 回归 L6：索引未命中的兜底扫描
// 与索引扫描统一为递归——新分类目录内的子文件夹图片也应被选中。
func TestLocalRepositoryFallbackScanRecursive(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pc", "default"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pc", "default", "a.jpg"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	indexFile := filepath.Join(t.TempDir(), "local.json")
	repo := NewLocalRepository(root, indexFile)

	// 索引建立后再新增分类（索引未命中 → 走 scanImages 兜底），
	// 图片放在子文件夹里（此前仅当前层的实现会漏掉）
	sub := filepath.Join(root, "pc", "newcat", "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "b.jpg"), []byte("y"), 0644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	img, err := repo.Random(ctx, "newcat", model.DevicePC)
	if err != nil {
		t.Fatal(err)
	}
	if img.URL != filepath.Join(sub, "b.jpg") {
		t.Fatalf("URL = %q, want subdir b.jpg (recursive fallback scan)", img.URL)
	}
}

// TestParseRefreshSchedule 验证刷新计划表的归一化：整数分钟、Go duration、
// @ 描述符、5 字段 cron 原样透传；空/0 关闭。
func TestParseRefreshSchedule(t *testing.T) {
	cases := []struct {
		raw    string
		want   string
		wantOn bool
	}{
		{"", "", false},
		{"0", "", false},
		{"0s", "", false},
		{"-30m", "", false},
		{"30s", "@every 30s", true},
		{"30m", "@every 30m", true},   // 分钟
		{"24h", "@every 24h", true},   // 天（duration 无 d 单位，写 24h）
		{"168h", "@every 168h", true}, // 一周
		{"@hourly", "@hourly", true},
		{"@daily", "@daily", true},
		{"@weekly", "@weekly", true},
		{"@monthly", "@monthly", true},
		{"@yearly", "@yearly", true},
		{"0 3 * * *", "0 3 * * *", true},       // 每天 03:00
		{"30 4 * * 1", "30 4 * * 1", true},     // 每周一 04:30
		{"bad !!! expr", "bad !!! expr", true}, // 非法表达式交给 cron 校验（AddFunc 报错则禁用）
	}
	for _, c := range cases {
		got, on := parseRefreshSchedule(c.raw)
		if got != c.want || on != c.wantOn {
			t.Errorf("parseRefreshSchedule(%q) = (%q, %v), want (%q, %v)",
				c.raw, got, on, c.want, c.wantOn)
		}
	}
}

// ── L-05：根目录瞬时不可见时不得用空索引覆盖有效索引 ──

// TestRefreshIndexKeepsIndexWhenRootMissing 根目录瞬时不可见（网络盘掉线、
// 路径配错）时扫出的空结果不可信：内存与磁盘索引都必须保留原样，并返回错误。
func TestRefreshIndexKeepsIndexWhenRootMissing(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "local")
	dir := filepath.Join(root, "pc", "default")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.jpg"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	indexFile := filepath.Join(base, "index", "local.json")
	repo := NewLocalRepository(root, indexFile)
	before, err := os.ReadFile(indexFile)
	if err != nil {
		t.Fatalf("索引未生成：%v", err)
	}
	if got := len(repo.images["pc/default"]); got != 1 {
		t.Fatalf("内存索引 pc/default 条目数 = %d，期望 1", got)
	}

	// 根目录消失（写入的文件均已关闭，Windows 下可直接删除）
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}

	if err := repo.RebuildIndex(); !errors.Is(err, errLocalRootMissing) {
		t.Fatalf("RebuildIndex = %v，期望 errLocalRootMissing", err)
	}

	// 内存索引不得被清空
	if got := len(repo.images["pc/default"]); got != 1 {
		t.Fatalf("内存索引被清空：pc/default 条目数 = %d", got)
	}
	// 磁盘索引不得被覆盖（连字节都不应变）
	after, err := os.ReadFile(indexFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("磁盘索引被覆盖：\nbefore=%s\nafter=%s", before, after)
	}
}

// TestRefreshIndexWritesEmptyIndexWhenRootExistsButEmpty 根目录存在但没有图片
// 属合法空图库（与根目录不可见区分）：仍应清空并落盘空索引。
func TestRefreshIndexWritesEmptyIndexWhenRootExistsButEmpty(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "local")
	dir := filepath.Join(root, "pc", "default")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	img := filepath.Join(dir, "a.jpg")
	if err := os.WriteFile(img, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	indexFile := filepath.Join(base, "index", "local.json")
	repo := NewLocalRepository(root, indexFile)

	// 删掉唯一图片：目录仍在，只是没有图片了
	if err := os.Remove(img); err != nil {
		t.Fatal(err)
	}

	if err := repo.RebuildIndex(); err != nil {
		t.Fatalf("RebuildIndex = %v，期望 nil（目录存在但为空不算根目录缺失）", err)
	}
	if len(repo.images) != 0 {
		t.Fatalf("内存索引 = %v，期望清空", repo.images)
	}
	data, err := os.ReadFile(indexFile)
	if err != nil {
		t.Fatal(err)
	}
	var onDisk map[string][]string
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("索引文件解析失败：%v", err)
	}
	if len(onDisk) != 0 {
		t.Fatalf("磁盘索引 = %v，期望空索引", onDisk)
	}
}

// TestNewLocalRepositoryIndexWhenRootMissing 首次启动尚未创建根目录时不返回错误，
// 仍应落盘空索引——否则根目录缺失会导致永远建不出索引。
func TestNewLocalRepositoryIndexWhenRootMissing(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "local") // 故意不创建
	indexFile := filepath.Join(base, "index", "local.json")

	NewLocalRepository(root, indexFile)

	if _, err := os.Stat(indexFile); err != nil {
		t.Fatalf("根目录缺失且当前索引为空时应照常生成空索引：%v", err)
	}
}

// ── N-04：负缓存写入不再每次全量扫描 ──

// TestNegativeCacheSweepFrequency 负缓存的清理不得随写入次数线性增长：
// key 来自用户传入的 category，旧实现每次写入都在锁内遍历整个 map。
func TestNegativeCacheSweepFrequency(t *testing.T) {
	t.Run("重复同一 key 只清理一次", func(t *testing.T) {
		repo := &LocalRepository{negAt: make(map[string]time.Time)}
		for i := 0; i < 5000; i++ {
			repo.rememberNegative("pc/default")
		}
		if repo.negSweeps != 1 {
			t.Fatalf("全量清理次数 = %d，期望 1（旧实现等于写入次数 5000）", repo.negSweeps)
		}
	})

	t.Run("大量不同 key 时清理次数远少于写入次数", func(t *testing.T) {
		repo := &LocalRepository{negAt: make(map[string]time.Time)}
		const writes = 5000
		for i := 0; i < writes; i++ {
			repo.rememberNegative("pc/cat-" + strconv.Itoa(i))
		}
		if repo.negSweeps*10 > writes {
			t.Fatalf("全量清理 %d 次 / 写入 %d 次，疑似每次写入都全扫", repo.negSweeps, writes)
		}
		if len(repo.negAt) > negSweepThreshold {
			t.Fatalf("负缓存条目数 = %d，超过阈值 %d（map 无上限增长）", len(repo.negAt), negSweepThreshold)
		}
		t.Logf("写入 %d 个不同 key：全量清理 %d 次，最终条目数 %d", writes, repo.negSweeps, len(repo.negAt))
	})
}

// TestNegativeCacheTTLUnchanged 负缓存 TTL 判定语义保持原样：
// TTL 内命中，超过 TTL 视为未命中并清掉该条目。
func TestNegativeCacheTTLUnchanged(t *testing.T) {
	repo := &LocalRepository{negAt: make(map[string]time.Time)}
	repo.rememberNegative("pc/default")
	if !repo.negativeRecently("pc/default") {
		t.Fatal("TTL 内应命中负缓存")
	}

	// 把判定时间推到 TTL 之前
	repo.negMu.Lock()
	repo.negAt["pc/default"] = time.Now().Add(-negCacheTTL - time.Second)
	repo.negMu.Unlock()

	if repo.negativeRecently("pc/default") {
		t.Fatal("超过 TTL 不应命中负缓存")
	}
	repo.negMu.Lock()
	_, ok := repo.negAt["pc/default"]
	repo.negMu.Unlock()
	if ok {
		t.Fatal("超过 TTL 的条目应被清除")
	}
}
