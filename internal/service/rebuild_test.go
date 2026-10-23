package service

import (
	"os"
	"path/filepath"
	"testing"

	"img-api/internal/cache"
	"img-api/internal/config"
	"img-api/internal/model"
)

// loadTestConfig 初始化包级 config.C：NewRandomService 会读
// config.C.Circuit* 构造熔断器（nil 会 panic）。本包 random_test.go 只初始化了
// logger.L，因此这两个用例自行加载默认配置。
func loadTestConfig(t *testing.T) {
	t.Helper()
	if config.C != nil {
		return
	}
	if err := config.Load(t.TempDir()); err != nil {
		t.Fatalf("config.Load: %v", err)
	}
}

// TestRebuildLocalIndex 回归 P4-11：
// SIGHUP 触发的按需索引重建必须真正扫描磁盘并刷新内存索引，
// 且不影响请求路径（不需要"exec build-index + 重启"）。
func TestRebuildLocalIndex(t *testing.T) {
	loadTestConfig(t)
	root := t.TempDir()
	catDir := filepath.Join(root, "resources", "local", "pc", "default")
	if err := os.MkdirAll(catDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(catDir, "a.jpg"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	stats := NewStats()
	svc := NewRandomService(root, cache.NewMemoryCache(), nil, stats)
	defer svc.Close()

	// 首次重建：生成索引并识别出新图片
	if err := svc.RebuildLocalIndex(); err != nil {
		t.Fatalf("RebuildLocalIndex: %v", err)
	}
	img, err := svc.Random(t.Context(), model.SourceLocal, "", "default", model.DevicePC)
	if err != nil {
		t.Fatalf("重建后取图失败: %v", err)
	}
	if filepath.Base(img.URL) != "a.jpg" {
		t.Fatalf("取到的图片 = %q, want a.jpg", img.URL)
	}

	// 新增第二张图片后再次重建：内存索引应立即包含它
	if err := os.WriteFile(filepath.Join(catDir, "b.jpg"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := svc.RebuildLocalIndex(); err != nil {
		t.Fatalf("第二次 RebuildLocalIndex: %v", err)
	}

	seen := map[string]bool{}
	for i := 0; i < 60; i++ { // 随机抽样，确认两张都可能被选中
		got, err := svc.Random(t.Context(), model.SourceLocal, "", "default", model.DevicePC)
		if err != nil {
			t.Fatalf("取图失败: %v", err)
		}
		seen[filepath.Base(got.URL)] = true
	}
	if !seen["b.jpg"] {
		t.Errorf("重建后新增图片未被索引（seen=%v）", seen)
	}

	// 索引文件必须真的写盘（供重启后 loadIndex 使用）
	if _, err := os.Stat(filepath.Join(root, "storage", "index", "local.json")); err != nil {
		t.Errorf("索引文件未生成: %v", err)
	}
}

// TestRebuildLocalIndexInvalidatesCategorySnapshot 重建后分类快照必须立即失效，
// 否则新增分类在 30 秒内不会出现在提示页/多分类筛选中。
func TestRebuildLocalIndexInvalidatesCategorySnapshot(t *testing.T) {
	loadTestConfig(t)
	root := t.TempDir()
	for _, dev := range []string{"pc", "pe"} {
		if err := os.MkdirAll(filepath.Join(root, "resources", "local", dev, "default"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(
		filepath.Join(root, "resources", "local", "pc", "default", "a.jpg"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	stats := NewStats()
	svc := NewRandomService(root, cache.NewMemoryCache(), nil, stats)
	defer svc.Close()

	// 预热快照（此时只有 default 分类）
	_ = svc.AvailableCategories(model.SourceLocal)

	// 新增一个分类目录后重建
	newCat := filepath.Join(root, "resources", "local", "pc", "anime")
	if err := os.MkdirAll(newCat, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newCat, "n.jpg"), []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := svc.RebuildLocalIndex(); err != nil {
		t.Fatalf("RebuildLocalIndex: %v", err)
	}

	cats := svc.AvailableCategories(model.SourceLocal)
	found := false
	for _, c := range cats {
		if c == "anime" {
			found = true
		}
	}
	if !found {
		t.Errorf("重建后新分类未立即出现在快照中（cats=%v）——快照未失效？", cats)
	}
}
