package repository

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"img-api/internal/model"
)

// TestTxtRepositoryCanceledNotTreatedAsRedisFailure 回归 R5-12：
// 客户端取消/超时不是 Redis 故障，不得触发 30 秒冷却（否则一次刷新/断网
// 就会让全站绕过 Redis，并打出一条假告警）。
func TestTxtRepositoryCanceledNotTreatedAsRedisFailure(t *testing.T) {
	root := t.TempDir()
	pc := filepath.Join(root, "pc")
	if err := os.MkdirAll(pc, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pc, "anime.txt"), []byte("https://a.com/1.jpg\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fc := &failCache{err: context.Canceled}
	repo := NewTxtRepository(root, fc)
	ctx := context.Background()

	// 两次请求都必须尝试 Redis：若第一次被误判为故障，第二次会进冷却而跳过
	for i := 0; i < 2; i++ {
		if _, err := repo.Random(ctx, "anime", model.DevicePC); err != nil {
			t.Fatalf("request %d: %v", i+1, err)
		}
	}
	if fc.calls != 2 {
		t.Fatalf("redis calls = %d, want 2（取消不得触发冷却）", fc.calls)
	}
}

// TestLocalHealthRejectsNonDirDevicePath 回归 R5-20：
// pc/pe 是普通文件（挂载异常）时健康检查必须报 unhealthy，而不是只 Stat 就放过。
func TestLocalHealthRejectsNonDirDevicePath(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pe"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pc"), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 直接构造结构体：Health 只依赖 rootPath。
	// 不走 NewLocalRepository（它会扫描并写入 local.json，Windows 上偶尔会让
	// t.TempDir 的清理报 "directory is not empty"，属于不必要的副作用）。
	repo := &LocalRepository{rootPath: root}
	if err := repo.Health(context.Background()); err == nil {
		t.Fatal("pc 是普通文件时 Health 应返回错误")
	}
}
