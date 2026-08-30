// build-index — 手动重建本地图片 JSON 索引。
//
// 扫描 resources/local/ 目录，将所有图片文件路径写入 storage/index/local.json。
// 与主服务自动索引共用同一扫描函数（repository.ScanLocalImages），结果保持一致。
//
// ⚠️ 通常无需手动运行：主服务启动时会自动生成/加载索引（见 LocalRepository），
// 并可配置 LOCAL_INDEX_REFRESH 计划表自动刷新（duration/@ 描述符/cron）。此命令用于：
//   - 无需重启服务、立即重建索引
//   - 验证索引文件格式
//
// 用法：go run cmd/build-index/main.go
package main

import (
	"encoding/json"
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
	images := repository.ScanLocalImages(localDir)

	// 原子写入 JSON 索引：先写临时文件再重命名（与主服务自动索引行为一致）
	os.MkdirAll(filepath.Dir(indexFile), 0755)
	tmp := indexFile + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ create index file: %v\n", err)
		os.Exit(1)
	}

	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(images); err != nil {
		f.Close()
		os.Remove(tmp)
		fmt.Fprintf(os.Stderr, "❌ encode json: %v\n", err)
		os.Exit(1)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		fmt.Fprintf(os.Stderr, "❌ close index file: %v\n", err)
		os.Exit(1)
	}
	if err := os.Rename(tmp, indexFile); err != nil {
		os.Remove(tmp)
		fmt.Fprintf(os.Stderr, "❌ rename index file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ Index built: %s\n", indexFile)
	for cat, files := range images {
		fmt.Printf("   %s: %d images\n", cat, len(files))
	}
}
