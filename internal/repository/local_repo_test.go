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
