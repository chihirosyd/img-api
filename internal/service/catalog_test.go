package service

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
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

// TestTxtCategoriesKeepOriginalCase 回归 L-01：分类名按文件名的原始大小写探测与列举。
// 读取侧（TxtRepository）用请求的原始名拼 "<分类>.txt" 打开文件，若这里先小写化，
// 大小写敏感文件系统上 Anime.txt 会"能出图但分类清单里列不出来"。
// 该场景在 Windows/macOS（大小写不敏感）上同样可构造，断言的是列举名的大小写。
func TestTxtCategoriesKeepOriginalCase(t *testing.T) {
	root := t.TempDir()
	pc := filepath.Join(root, "pc")
	if err := os.MkdirAll(pc, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pc, "Anime.txt"), []byte("https://example.com/a.jpg\n"), 0644); err != nil {
		t.Fatal(err)
	}

	got := txtCategoriesForDevice(root, "pc")
	want := []string{"Anime"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("txtCategoriesForDevice = %v, want %v（分类名应保留原始大小写）", got, want)
	}
}

// TestTxtCategoriesCaseDedupe 大小写并存时视为同一分类，只列一个且用原始名。
// 依赖大小写敏感文件系统：Windows/macOS 上 Anime.txt 与 anime.txt 是同一个文件，
// 无法在真实目录里并列；跨平台的去重断言见 TestTxtCategoriesCaseDedupeByFiles。
func TestTxtCategoriesCaseDedupe(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		t.Skip("大小写不敏感文件系统无法构造 Anime.txt/anime.txt 并存；由 Linux CI 覆盖去重")
	}
	root := t.TempDir()
	pc := filepath.Join(root, "pc")
	if err := os.MkdirAll(pc, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Anime.txt", "anime.txt"} {
		if err := os.WriteFile(filepath.Join(pc, name), []byte("https://example.com/a.jpg\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// ReadDir 按文件名排序，Anime.txt 在前 → 去重后保留先读到的原始名
	got := txtCategoriesForDevice(root, "pc")
	want := []string{"Anime"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("txtCategoriesForDevice = %v, want %v（并存只列一个且用原始名）", got, want)
	}
}

// TestTxtCategoriesCaseDedupeByFiles 在文件清单层面断言去重（不依赖文件系统大小写）：
// 两个大小写不同的名字指向同一分类时只列一次。Windows/macOS 上两个名字都指向
// 同一个文件、探测都会命中，因此这里同样能覆盖去重分支。
func TestTxtCategoriesCaseDedupeByFiles(t *testing.T) {
	root := t.TempDir()
	pc := filepath.Join(root, "pc")
	if err := os.MkdirAll(pc, 0755); err != nil {
		t.Fatal(err)
	}
	// 大小写不敏感文件系统上第二次写入落到同一个文件，不影响本用例的断言
	for _, name := range []string{"Anime.txt", "anime.txt"} {
		if err := os.WriteFile(filepath.Join(pc, name), []byte("https://example.com/a.jpg\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	got := txtCategoriesFromFiles(root, "pc", []string{"Anime.txt", "anime.txt"})
	if !reflect.DeepEqual(got, []string{"Anime"}) {
		t.Errorf("txtCategoriesFromFiles = %v, want [Anime]（大小写并存只列一个且用原始名）", got)
	}
}

// TestTxtCategoriesUppercaseExtensionUnreadable 大写扩展名（.TXT）不在读取侧口径内：
// 读取侧只拼 ".txt"，此类文件既取不到图也不应出现在清单里（大小写敏感文件系统）。
func TestTxtCategoriesUppercaseExtensionUnreadable(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		t.Skip("大小写不敏感文件系统上 ANIME.TXT 可被 ANIME.txt 打开，与读取侧一致地列出")
	}
	root := t.TempDir()
	pc := filepath.Join(root, "pc")
	if err := os.MkdirAll(pc, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pc, "ANIME.TXT"), []byte("https://example.com/a.jpg\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if got := txtCategoriesForDevice(root, "pc"); len(got) != 0 {
		t.Errorf("txtCategoriesForDevice = %v, want empty（读取侧打不开 ANIME.txt）", got)
	}
}

// TestTxtCategoriesInvalidSchemeIgnored 回归 L1：非 http/https 的行不算有效 URL，
// 分类探测与读取（ReadTxtLines）口径一致，避免"提示页说存在、请求却取不到"。
func TestTxtCategoriesInvalidSchemeIgnored(t *testing.T) {
	root := t.TempDir()
	pc := filepath.Join(root, "pc")
	if err := os.MkdirAll(pc, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pc, "bad.txt"), []byte("ftp://example.com/a.jpg\njavascript:alert(1)\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if got := txtCategoriesForDevice(root, "pc"); len(got) != 0 {
		t.Errorf("txtCategoriesForDevice = %v, want empty (invalid schemes ignored)", got)
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

// TestCategorySnapshotQueries 验证基于快照的 CategoryExistsFor /
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

	// 分类完全不存在时对当前设备返回 false
	if s.CategoryExistsFor(model.SourceTxt, "beauty", model.DevicePC) {
		t.Error("CategoryExistsFor(txt, beauty, pc) = true, want false")
	}
	// 多分类：任一分类对当前设备存在即 true
	if !s.CategoryExistsFor(model.SourceTxt, "beauty,anime", model.DevicePC) {
		t.Error("CategoryExistsFor(txt, beauty,anime, pc) = false, want true")
	}
	// 多分类：全部不在当前设备目录时为 false（scenery 只在 pe）
	if s.CategoryExistsFor(model.SourceTxt, "beauty,scenery", model.DevicePC) {
		t.Error("CategoryExistsFor(txt, beauty,scenery, pc) = true, want false")
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
