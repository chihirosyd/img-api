// sync-redis — 将 TXT 图库中的图片 URL 批量同步到 Redis Set。
//
// 每个 TXT 文件对应一个 Redis Set（key: img:{pc|pe}:{category}），
// 之后 TxtRepository 可通过 SRandMember 实现 O(1) 随机选取。
//
// 用法：
//
//	go run ./cmd/sync-redis/
//	go run ./cmd/sync-redis/ -dir resources/txt
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"img-api/internal/app"
	"img-api/internal/cache"
	"img-api/internal/config"
	"img-api/internal/logger"
	"img-api/internal/repository"
)

func main() {
	txtDir := flag.String("dir", "resources/txt", "TXT 图库根目录")
	flag.Parse()

	// 加载配置（需要 Redis 连接信息）
	rootPath := app.RootPath()
	if err := config.Load(rootPath); err != nil {
		fmt.Fprintf(os.Stderr, "❌ load config: %v\n", err)
		os.Exit(1)
	}
	_ = logger.Init("info", filepath.Join(rootPath, "storage", "logs"), 30, 0)

	// 检查 Redis 是否已配置
	if !config.C.IsRedisEnabled() {
		fmt.Println("⚠️  Redis 未配置（REDIS_ADDR 为空），无需同步。")
		fmt.Println("   主服务将直接从 TXT 文件读取图片 URL。")
		fmt.Println("   如需启用 Redis 加速，请在 .env 中设置 REDIS_ADDR。")
		os.Exit(0)
	}

	// 连接 Redis
	redisCache, err := cache.NewRedisCache(config.C.RedisAddr, config.C.RedisPassword, config.C.RedisDB)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Redis 连接失败: %v\n", err)
		os.Exit(1)
	}
	defer redisCache.Close()

	ctx := context.Background()
	fullDir := filepath.Join(rootPath, *txtDir)
	total := 0

	// 遍历 pc/ 和 pe/ 子目录
	for _, device := range []string{"pc", "pe"} {
		deviceDir := filepath.Join(fullDir, device)
		entries, err := os.ReadDir(deviceDir)
		if err != nil {
			fmt.Printf("⚠️  跳过 %s: %v\n", device, err)
			continue
		}

		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".txt") {
				continue
			}

			category := strings.TrimSuffix(entry.Name(), ".txt")
			txtPath := filepath.Join(deviceDir, entry.Name())

			// 读取 TXT 文件中的 URL（解析规则与主服务 TxtRepository 一致）
			urls, err := repository.ReadTxtLines(txtPath)
			if err != nil || len(urls) == 0 {
				fmt.Printf("⚠️  跳过 %s: %v\n", txtPath, err)
				continue
			}

			// Redis Set key: img:pc:anime, img:pe:scenery
			redisKey := fmt.Sprintf("img:%s:%s", device, category)

			// 先清旧数据再批量写入
			// 注意：生产环境应用 SCARD 检查增量更新
			_ = redisCache.Delete(ctx, redisKey)
			if err := redisCache.SAdd(ctx, redisKey, urls...); err != nil {
				fmt.Printf("❌ 写入 Redis 失败: %s → %v\n", redisKey, err)
				continue
			}

			count, _ := redisCache.SCard(ctx, redisKey)
			fmt.Printf("✅ %-30s → %4d URLs\n", redisKey, count)
			total += len(urls)
		}
	}

	fmt.Printf("\n🎉 同步完成！共 %d 张图片 URL 导入 Redis\n", total)
}
