package service

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLocalCategoryExistsRecursesIntoSubdirs 回归 R5-13：
// 图片全放在分类的子目录里时，分类判定必须与索引扫描一致（递归），
// 否则会出现 "Random 能出图，但分类清单不列、多分类被过滤掉" 的自相矛盾。
func TestLocalCategoryExistsRecursesIntoSubdirs(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "pc", "anime", "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "a.jpg"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !localCategoryExistsForDevice(root, "pc", "anime") {
		t.Fatal("子目录内的图片应让分类判定为存在")
	}
	if got := localCategoriesForDevice(root, "pc"); len(got) != 1 || got[0] != "anime" {
		t.Fatalf("localCategoriesForDevice = %v, want [anime]", got)
	}
	if localCategoryExistsForDevice(root, "pc", "empty") {
		t.Fatal("不存在的分类应为 false")
	}
}
