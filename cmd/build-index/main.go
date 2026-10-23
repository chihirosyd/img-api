// build-index — 手动重建本地图片 JSON 索引。
//
// 扫描 resources/local/ 目录，将所有图片文件路径写入 storage/index/local.json。
// 扫描与写盘均复用主服务的实现（repository.ScanLocalImages / repository.WriteLocalIndex），
// 结果与主服务自动生成的索引完全一致（原子写入 + 落盘 + 重命名）。
//
// ⚠️ 通常无需手动运行：主服务启动时会自动生成/加载索引（见 LocalRepository），
// 并可配置 LOCAL_INDEX_REFRESH 计划表自动刷新（duration/@ 描述符/cron）。此命令用于：
//   - 无需重启服务、立即重建索引
//   - 验证索引文件格式
//
// 用法：go run cmd/build-index/main.go
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"img-api/internal/app"
	"img-api/internal/repository"
)

func main() {
	root := app.RootPath()
	localDir := filepath.Join(root, "resources", "local")
	indexFile := filepath.Join(root, "storage", "index", "local.json")

	fmt.Println("🔍 Scanning:", localDir)

	// 与主服务自动索引共用的扫描逻辑（key: "pc/default", value: [filepaths]）
	images, err := repository.ScanLocalImages(localDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ scan failed: %v\n", err)
		os.Exit(1)
	}

	// 与主服务自动索引共用的原子写入（随机临时文件 + Sync + Rename）
	if err := repository.WriteLocalIndex(indexFile, images); err != nil {
		fmt.Fprintf(os.Stderr, "❌ write index failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ Index built: %s\n", indexFile)
	for cat, files := range images {
		fmt.Printf("   %s: %d images\n", cat, len(files))
	}
}
