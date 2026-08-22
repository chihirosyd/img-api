package service

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"img-api/internal/model"
)

// TestTxtCategoriesForDevice 验证 txt 分类清单只包含"含有效 URL"的文件，
// 且目录顺序与 ReadDir（文件名排序）一致。
func TestTxtCategoriesForDevice(t *testing.T) {
	root := t.TempDir()
	pc := filepath.Join(root, "pc")
	if err := os.MkdirAll(pc, 0755); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(pc, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("anime.txt", "https://example.com/a.jpg\n")
	write("empty.txt", "# 仅注释\n")
	write("notes.txt", "\n# 注释\n")
	write("scenery.txt", "https://example.com/s.jpg\n")

	got := txtCategoriesForDevice(root, "pc")
	want := []string{"anime", "scenery"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("txtCategoriesForDevice = %v, want %v", got, want)
	}
	// 设备目录不存在 → nil
	if got := txtCategoriesForDevice(root, "pe"); got != nil {
		t.Errorf("txtCategoriesForDevice(missing device) = %v, want nil", got)
	}
}

// TestLocalCategoriesForDevice 验证 local 分类清单只包含直接含图片文件的目录。
func TestLocalCategoriesForDevice(t *testing.T) {
	root := t.TempDir()
	pc := filepath.Join(root, "pc")
	for _, dir := range []string{"default", "empty"} {
		if err := os.MkdirAll(filepath.Join(pc, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(pc, "default", "a.jpg"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	got := localCategoriesForDevice(root, "pc")
	if !reflect.DeepEqual(got, []string{"default"}) {
		t.Errorf("localCategoriesForDevice = %v, want [default]", got)
	}
}

// TestCategorySnapshotQueries 验证基于快照的 CategoryExists / CategoryExistsFor /
// AvailableCategories 语义（直接用结构体构造 RandomService，不依赖 config 初始化）。
func TestCategorySnapshotQueries(t *testing.T) {
	root := t.TempDir()
	mk := func(rel string) {
		if err := os.MkdirAll(filepath.Join(root, rel), 0755); err != nil {
			t.Fatal(err)
		}
	}
	mk("resources/txt/pc")
	mk("resources/txt/pe")
	mk("resources/local/pc")
	if err := os.WriteFile(
		filepath.Join(root, "resources", "txt", "pc", "anime.txt"),
		[]byte("https://example.com/a.jpg\n"), 0644,
	); err != nil {
		t.Fatal(err)
	}
	// pe 设备目录下没有 anime → 按设备判定应返回 false
	if err := os.WriteFile(
		filepath.Join(root, "resources", "txt", "pe", "scenery.txt"),
		[]byte("https://example.com/s.jpg\n"), 0644,
	); err != nil {
		t.Fatal(err)
	}

	s := &RandomService{rootPath: root}

	// 任一设备存在即 true
	if !s.CategoryExists(model.SourceTxt, "anime") {
		t.Error("CategoryExists(txt, anime) = false, want true")
	}
	if !s.CategoryExists(model.SourceTxt, "scenery") {
		t.Error("CategoryExists(txt, scenery) = false, want true")
	}
	if s.CategoryExists(model.SourceTxt, "beauty") {
		t.Error("CategoryExists(txt, beauty) = true, want false")
	}
	// 多分类：任一存在即 true
	if !s.CategoryExists(model.SourceTxt, "beauty,anime") {
		t.Error("CategoryExists(txt, beauty,anime) = false, want true")
	}

	// 按设备精确判定：anime 只在 pc，pe 请求应返回 false
	if !s.CategoryExistsFor(model.SourceTxt, "anime", model.DevicePC) {
		t.Error("CategoryExistsFor(txt, anime, pc) = false, want true")
	}
	if s.CategoryExistsFor(model.SourceTxt, "anime", model.DevicePE) {
		t.Error("CategoryExistsFor(txt, anime, pe) = true, want false")
	}
	if !s.CategoryExistsFor(model.SourceTxt, "scenery", model.DevicePE) {
		t.Error("CategoryExistsFor(txt, scenery, pe) = false, want true")
	}
	// external 固定返回 true
	if !s.CategoryExistsFor(model.SourceExternal, "whatever", model.DevicePC) {
		t.Error("CategoryExistsFor(external, ...) = false, want true")
	}

	// 可用分类列表：pc 在前去重合并
	got := s.AvailableCategories(model.SourceTxt)
	want := []string{"anime", "scenery"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AvailableCategories(txt) = %v, want %v", got, want)
	}
	if got := s.AvailableCategories(model.SourceExternal); got != nil {
		t.Errorf("AvailableCategories(external) = %v, want nil", got)
	}
}
