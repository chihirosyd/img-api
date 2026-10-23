package repository

// 注：本包的 logger.L 由 txt_repo_test.go 的 TestMain 初始化为丢弃输出，
// 因此 buildRequestHeaders 的告警日志路径可安全触发（不会 nil panic）。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"img-api/internal/model"
)

// TestBuildRequestHeadersUserAgentCase 回归 D2 / P4-2：
// 配置里的 User-Agent 键无论大小写写法都应被识别为"已配置 UA"，
// 并统一写入规范键 User-Agent。此前 canonicalUA 只在精确匹配 "User-Agent"
// 时赋值，小写写法下该分支永远不可达（P2A-7 的修复形同虚设）。
func TestBuildRequestHeadersUserAgentCase(t *testing.T) {
	t.Run("未配置 UA → 按设备注入", func(t *testing.T) {
		got := buildRequestHeaders(map[string]string{"Authorization": "Bearer x"}, model.DevicePC)
		if got["User-Agent"] != userAgentFor(model.DevicePC) {
			t.Errorf("User-Agent = %q, want %q", got["User-Agent"], userAgentFor(model.DevicePC))
		}
		if got["Authorization"] != "Bearer x" {
			t.Errorf("Authorization 应原样保留，got %q", got["Authorization"])
		}
	})

	t.Run("小写 user-agent 视为已配置", func(t *testing.T) {
		got := buildRequestHeaders(map[string]string{"user-agent": "custom-ua"}, model.DevicePE)
		if got["User-Agent"] != "custom-ua" {
			t.Errorf("User-Agent = %q, want \"custom-ua\"（小写键应被识别为已配置）", got["User-Agent"])
		}
		if len(got) != 1 {
			t.Errorf("应只保留规范键，got keys=%v", got)
		}
	})

	t.Run("大写变体同样识别", func(t *testing.T) {
		got := buildRequestHeaders(map[string]string{"USER-AGENT": "upper-ua"}, model.DevicePC)
		if got["User-Agent"] != "upper-ua" {
			t.Errorf("User-Agent = %q, want \"upper-ua\"", got["User-Agent"])
		}
	})

	t.Run("重复变体不应 panic 且产出规范键", func(t *testing.T) {
		got := buildRequestHeaders(map[string]string{
			"User-Agent": "a", "user-agent": "b", "USER-AGENT": "c",
		}, model.DevicePC)
		if _, ok := got["User-Agent"]; !ok {
			t.Errorf("应产出规范键 User-Agent，got %v", got)
		}
	})
}

// TestWriteLocalIndex 回归 D7 / P4-4：
// build-index 与主服务共用同一写入实现（原子写入、无固定 .tmp 残留）。
func TestWriteLocalIndex(t *testing.T) {
	dir := t.TempDir()
	indexFile := filepath.Join(dir, "storage", "index", "local.json")

	images := map[string][]string{
		"pc/default": {"/abs/a.jpg", "/abs/b.png"},
		"pe/default": {"/abs/c.webp"},
	}

	if err := WriteLocalIndex(indexFile, images); err != nil {
		t.Fatalf("WriteLocalIndex: %v", err)
	}

	data, err := os.ReadFile(indexFile)
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	var got map[string][]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("索引不是合法 JSON: %v", err)
	}
	if len(got["pc/default"]) != 2 || got["pe/default"][0] != "/abs/c.webp" {
		t.Fatalf("索引内容不符: %#v", got)
	}

	// 目录下不应残留临时文件（旧实现用固定名 local.json.tmp）
	entries, err := os.ReadDir(filepath.Dir(indexFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "local.json" {
			t.Errorf("残留文件: %s", e.Name())
		}
	}
}

// TestRepositoryHealthChecksDeviceDirs 回归 D6 / P4-6：
// 根目录存在但 pc/pe 设备子目录缺失时必须报不健康——此前仅 os.Stat 根目录，
// 而该目录由部署骨架保证永远存在，导致 /health 恒报健康、探针形同虚设。
func TestRepositoryHealthChecksDeviceDirs(t *testing.T) {
	ctx := t.Context()

	t.Run("local 只建根目录 → 不健康", func(t *testing.T) {
		root := t.TempDir()
		localDir := filepath.Join(root, "resources", "local")
		if err := os.MkdirAll(localDir, 0o755); err != nil {
			t.Fatal(err)
		}
		repo := NewLocalRepository(localDir, filepath.Join(root, "storage", "index", "local.json"))
		if err := repo.Health(ctx); err == nil {
			t.Error("pc/pe 子目录缺失时 Health 应返回 error，实际 nil")
		}
	})

	t.Run("local 建齐 pc/pe → 健康", func(t *testing.T) {
		root := t.TempDir()
		localDir := filepath.Join(root, "resources", "local")
		for _, dev := range []string{"pc", "pe"} {
			if err := os.MkdirAll(filepath.Join(localDir, dev), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		repo := NewLocalRepository(localDir, filepath.Join(root, "storage", "index", "local.json"))
		if err := repo.Health(ctx); err != nil {
			t.Errorf("pc/pe 齐备时 Health 应返回 nil，实际 %v", err)
		}
	})

	t.Run("local 根目录不存在 → 健康（尚无图片，非故障）", func(t *testing.T) {
		root := t.TempDir()
		repo := NewLocalRepository(filepath.Join(root, "resources", "local"),
			filepath.Join(root, "storage", "index", "local.json"))
		if err := repo.Health(ctx); err != nil {
			t.Errorf("根目录不存在应视为非故障，实际 %v", err)
		}
	})

	t.Run("txt 只建根目录 → 不健康", func(t *testing.T) {
		root := t.TempDir()
		txtDir := filepath.Join(root, "resources", "txt")
		if err := os.MkdirAll(txtDir, 0o755); err != nil {
			t.Fatal(err)
		}
		repo := NewTxtRepository(txtDir, nil)
		if err := repo.Health(ctx); err == nil {
			t.Error("pc/pe 子目录缺失时 Health 应返回 error，实际 nil")
		}
	})

	t.Run("txt 建齐 pc/pe → 健康", func(t *testing.T) {
		root := t.TempDir()
		txtDir := filepath.Join(root, "resources", "txt")
		for _, dev := range []string{"pc", "pe"} {
			if err := os.MkdirAll(filepath.Join(txtDir, dev), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		repo := NewTxtRepository(txtDir, nil)
		if err := repo.Health(ctx); err != nil {
			t.Errorf("pc/pe 齐备时 Health 应返回 nil，实际 %v", err)
		}
	})
}
