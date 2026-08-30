package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"img-api/internal/config"
	"img-api/internal/logger"
	"img-api/internal/model"
	"img-api/internal/repository"
)

func init() {
	// GetRepo 等路径会写日志，测试中指向丢弃设备
	logger.L = slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestSplitCategories(t *testing.T) {
	cases := []struct {
		raw  string
		want []string
	}{
		{"", []string{"default"}},
		{"anime", []string{"anime"}},
		{"anime, scenery , anime", []string{"anime", "scenery"}}, // 去重 + 去空格
		{" , , ", []string{"default"}},
		{"a,b,c", []string{"a", "b", "c"}},
	}

	for _, c := range cases {
		got := splitCategories(c.raw)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("splitCategories(%q) = %v, want %v", c.raw, got, c.want)
		}
	}
}

func TestContainsCategory(t *testing.T) {
	list := []string{"anime", "scenery"}
	if !containsCategory(list, "scenery") {
		t.Error("containsCategory should find existing category")
	}
	if containsCategory(list, "beauty") {
		t.Error("containsCategory should not find missing category")
	}
	if containsCategory(nil, "anime") {
		t.Error("containsCategory on empty list should return false")
	}
}

func TestMergeCategoryLists(t *testing.T) {
	got := mergeCategoryLists([]string{"a", "b"}, []string{"b", "c"})
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mergeCategoryLists = %v, want %v", got, want)
	}
	if got := mergeCategoryLists(nil, nil); len(got) != 0 {
		t.Errorf("mergeCategoryLists(nil, nil) = %v, want empty", got)
	}
}

// TestResolveDefaultCategory 验证 txt/local 默认分类解析：
// 空或 "default" 映射为配置值，显式分类不干预；external 不处理；未配置回退内置 default。
func TestResolveDefaultCategory(t *testing.T) {
	old := config.C
	config.C = &config.AppConfig{TxtDefaultCategory: "wallpaper", LocalDefaultCategory: "photos"}
	t.Cleanup(func() { config.C = old })

	s := &RandomService{}
	cases := []struct {
		source model.SourceType
		raw    string
		want   string
	}{
		{model.SourceTxt, "", "wallpaper"},
		{model.SourceTxt, "default", "wallpaper"},
		{model.SourceTxt, "anime", "anime"},
		{model.SourceTxt, "default,anime", "default,anime"},
		{model.SourceLocal, "", "photos"},
		{model.SourceLocal, "default", "photos"},
		{model.SourceExternal, "", ""},
		{model.SourceExternal, "default", "default"},
	}
	for _, c := range cases {
		if got := s.ResolveDefaultCategory(c.source, c.raw); got != c.want {
			t.Errorf("ResolveDefaultCategory(%v, %q) = %q, want %q", c.source, c.raw, got, c.want)
		}
	}

	// 未配置（config.C 为 nil 或值为空）→ 回退内置 default
	config.C = nil
	if got := s.ResolveDefaultCategory(model.SourceTxt, ""); got != "default" {
		t.Errorf("ResolveDefaultCategory(txt, \"\") with nil config = %q, want default", got)
	}
}

// TestRandomTxtConfiguredDefaultCategory 端到端验证：配置自定义默认分类后，
// 不传 category 的请求命中该分类；显式指定分类仍优先。
func TestRandomTxtConfiguredDefaultCategory(t *testing.T) {
	old := config.C
	config.C = &config.AppConfig{TxtDefaultCategory: "anime"}
	t.Cleanup(func() { config.C = old })

	root := t.TempDir()
	pc := filepath.Join(root, "resources", "txt", "pc")
	if err := os.MkdirAll(pc, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pc, "anime.txt"), []byte("https://a.com/1.jpg\n"), 0644); err != nil {
		t.Fatal(err)
	}

	s := &RandomService{
		rootPath: root,
		repos:    make(map[model.SourceType]repository.ImageRepository),
	}

	img, err := s.Random(context.Background(), model.SourceTxt, "", "", model.DevicePC)
	if err != nil {
		t.Fatal(err)
	}
	if img.URL != "https://a.com/1.jpg" || img.Category != "anime" {
		t.Fatalf("got url=%q category=%q, want anime image", img.URL, img.Category)
	}

	// 显式指定分类仍优先于默认分类
	if err := os.WriteFile(filepath.Join(pc, "scenery.txt"), []byte("https://b.com/2.jpg\n"), 0644); err != nil {
		t.Fatal(err)
	}
	img, err = s.Random(context.Background(), model.SourceTxt, "", "scenery", model.DevicePC)
	if err != nil {
		t.Fatal(err)
	}
	if img.URL != "https://b.com/2.jpg" {
		t.Fatalf("explicit category: url=%q, want b.com/2.jpg", img.URL)
	}
}

// TestRandomTxtEndToEnd 用临时目录验证 txt 图源的完整随机链路。
// 直接用结构体构造 RandomService（repos 需初始化，见 GetRepo 写 map）。
func TestRandomTxtEndToEnd(t *testing.T) {
	root := t.TempDir()
	pc := filepath.Join(root, "resources", "txt", "pc")
	if err := os.MkdirAll(pc, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pc, "anime.txt"), []byte("https://a.com/1.jpg\n"), 0644); err != nil {
		t.Fatal(err)
	}

	s := &RandomService{
		rootPath: root,
		repos:    make(map[model.SourceType]repository.ImageRepository),
	}

	img, err := s.Random(context.Background(), model.SourceTxt, "", "anime", model.DevicePC)
	if err != nil {
		t.Fatal(err)
	}
	if img.URL != "https://a.com/1.jpg" {
		t.Fatalf("URL = %q, want a.com/1.jpg", img.URL)
	}

	// 外部渠道未配置 → 哨兵错误（引导页场景）
	_, err = s.Random(context.Background(), model.SourceExternal, "", "default", model.DevicePC)
	if !errors.Is(err, model.ErrExternalNotConfigured) {
		t.Fatalf("external err = %v, want ErrExternalNotConfigured", err)
	}

	// 多分类全部不存在 → ErrCategoryNotSupported（404 提示页场景）。
	// 注意：单分类不存在时错误产生于仓库层，由 handler 经 CategoryExists 判定，
	// 服务层只在"多分类全部不可用"或"外部渠道白名单不匹配"时返回该哨兵。
	_, err = s.Random(context.Background(), model.SourceTxt, "", "beauty,nature", model.DevicePC)
	var cns *model.ErrCategoryNotSupported
	if !errors.As(err, &cns) {
		t.Fatalf("category err = %v, want ErrCategoryNotSupported", err)
	}
}
