package repository

import (
	"context"
	"os"
	"path/filepath"
	"testing"

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

	images := ScanLocalImages(root)
	files := images["pc/default"]
	if len(files) != 1 || files[0] != filepath.Join(dir, "a.jpg") {
		t.Fatalf("ScanLocalImages = %v, want only a.jpg", images)
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

// TestParseRefreshSchedule 验证刷新计划表的归一化：整数分钟、Go duration、
// @ 描述符、5 字段 cron 原样透传；空/0 关闭。
func TestParseRefreshSchedule(t *testing.T) {
	cases := []struct {
		raw      string
		want     string
		wantOn   bool
	}{
		{"", "", false},
		{"0", "", false},
		{"0s", "", false},
		{"-30m", "", false},
		{"30s", "@every 30s", true},
		{"30m", "@every 30m", true}, // 分钟
		{"24h", "@every 24h", true}, // 天（duration 无 d 单位，写 24h）
		{"168h", "@every 168h", true}, // 一周
		{"@hourly", "@hourly", true},
		{"@daily", "@daily", true},
		{"@weekly", "@weekly", true},
		{"@monthly", "@monthly", true},
		{"@yearly", "@yearly", true},
		{"0 3 * * *", "0 3 * * *", true},      // 每天 03:00
		{"30 4 * * 1", "30 4 * * 1", true},    // 每周一 04:30
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
