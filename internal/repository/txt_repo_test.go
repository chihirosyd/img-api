package repository

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"img-api/internal/logger"
	"img-api/internal/model"
)

func init() {
	// 仓库构造函数会写日志，测试中指向丢弃设备
	logger.L = slog.New(slog.NewTextHandler(io.Discard, nil))
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
