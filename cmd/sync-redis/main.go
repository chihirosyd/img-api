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
	_ = logger.Init(config.C.LogLevel, filepath.Join(rootPath, "storage", "logs"), config.C.LogMaxAge, config.C.LogMaxSize)

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
	// -dir 支持绝对路径（避免 filepath.Join 将绝对路径拼接到 rootPath 后）
	fullDir := *txtDir
	if !filepath.IsAbs(fullDir) {
		fullDir = filepath.Join(rootPath, fullDir)
	}
	total := 0

	// 遍历 pc/ 和 pe/ 子目录
	for _, device := range []string{"pc", "pe"} {
		total += syncDevice(ctx, redisCache, filepath.Join(fullDir, device), device)
	}

	fmt.Printf("\n🎉 同步完成！共 %d 张图片 URL 导入 Redis\n", total)
}

// setCache 是同步流程所需的最小缓存能力。
// 抽成接口是为了能用假实现做回归测试（CI 里没有真实 Redis）。
type setCache interface {
	Delete(ctx context.Context, key string) error
	SAdd(ctx context.Context, key string, members ...string) error
	SCard(ctx context.Context, key string) (int64, error)
	ScanKeys(ctx context.Context, pattern string) ([]string, error)
	Rename(ctx context.Context, oldKey, newKey string) error
}

// syncDevice 把一个设备目录（pc/pe）下的所有 .txt 分类同步到 Redis，
// 返回成功写入的 URL 总数。
//
// 安全要点（回归 R5-04）：
//   - 读取失败 ≠ 分类已删除：失败分类进 protectedCategories，孤儿清理必须跳过它
//   - 文件存在但没有有效 URL 才算"空分类"，其旧集合交给孤儿清理删除
//   - 写入用「临时 key + RENAME」原子替换，SAdd 失败时旧集合不受影响
//   - 一个 .txt 都没找到时跳过孤儿清理（多半是 -dir 指错，不能据此删光）
func syncDevice(ctx context.Context, c setCache, deviceDir, device string) int {
	entries, err := os.ReadDir(deviceDir)
	if err != nil {
		fmt.Printf("⚠️  跳过 %s: %v\n", device, err)
		return 0
	}

	total := 0
	// validCategories：本次成功同步的分类（其旧集合可安全删除）
	// protectedCategories：读取/写入失败、必须保留现有集合的分类
	validCategories := make(map[string]bool)
	protectedCategories := make(map[string]bool)
	txtSeen := 0 // 找到的 .txt 文件数（含内容为空的），用于判断 -dir 是否指错
	// 与主服务一致：分类文件名按小写处理（英文字母归一化为小写；中文无大小写，原样保留）
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".txt") {
			continue
		}
		txtSeen++

		category := strings.TrimSuffix(strings.ToLower(entry.Name()), ".txt")
		txtPath := filepath.Join(deviceDir, entry.Name())

		// 读取 TXT 文件中的 URL（解析规则与主服务 TxtRepository 一致）
		urls, err := repository.ReadTxtLines(txtPath)
		if err != nil {
			// 读取失败 ≠ 分类已删除：保留 Redis 里现有的集合。
			// 否则一次瞬时 IO 错误就会让下方孤儿清理把线上分类整个删掉。
			protectedCategories[category] = true
			fmt.Printf("⚠️  读取失败，保留现有集合 %s: %v\n", txtPath, err)
			continue
		}
		if len(urls) == 0 {
			// 文件存在但没有任何有效 URL：该分类实际已空，
			// 不加入 validCategories，交给孤儿清理删除旧集合。
			fmt.Printf("⚠️  %s 没有有效 URL，按空分类处理\n", txtPath)
			continue
		}

		// Redis Set key: img:pc:anime, img:pe:scenery
		redisKey := fmt.Sprintf("img:%s:%s", device, category)
		validCategories[category] = true

		// 原子替换：先写临时 key 再 RENAME。
		// 不用「先 Delete 再 SAdd」——SAdd 失败时旧集合已经没了。
		tmpKey := redisKey + ":tmp"
		_ = c.Delete(ctx, tmpKey) // 清理上次中断的残留
		if err := c.SAdd(ctx, tmpKey, urls...); err != nil {
			fmt.Printf("❌ 写入 Redis 失败: %s → %v（旧集合保留）\n", tmpKey, err)
			_ = c.Delete(ctx, tmpKey)
			protectedCategories[category] = true
			continue
		}
		if err := c.Rename(ctx, tmpKey, redisKey); err != nil {
			fmt.Printf("❌ 替换失败: %s → %v（旧集合保留）\n", redisKey, err)
			_ = c.Delete(ctx, tmpKey)
			protectedCategories[category] = true
			continue
		}

		count, _ := c.SCard(ctx, redisKey)
		fmt.Printf("✅ %-30s → %4d URLs\n", redisKey, count)
		total += len(urls)
	}

	// 一个 .txt 都没找到：多半是 -dir 指错了，此时清理会把该设备下所有集合删光
	if txtSeen == 0 {
		fmt.Printf("⚠️  %s 下没有任何 .txt（-dir 是否正确？），跳过孤儿清理\n", deviceDir)
		return total
	}

	// 清理孤儿 Set：分类 txt 文件已删除但 Redis 中仍残留的 key，
	// 不清理会导致主服务仍从旧 Set 返回已删除分类的图片。
	// 读取/写入失败的分类在 protectedCategories 里，必须显式跳过。
	keys, scanErr := c.ScanKeys(ctx, "img:"+device+":*")
	if scanErr != nil {
		fmt.Printf("⚠️  扫描 %s 孤儿 key 失败: %v\n", device, scanErr)
		return total
	}
	for _, k := range keys {
		cat := strings.TrimPrefix(k, "img:"+device+":")
		if validCategories[cat] || protectedCategories[cat] {
			continue
		}
		// 上次中断残留的临时键：直接清掉，不当作"已删除分类"报告
		if strings.HasSuffix(cat, ":tmp") {
			_ = c.Delete(ctx, k)
			continue
		}
		_ = c.Delete(ctx, k)
		fmt.Printf("🧹 清理已删除分类: %s\n", k)
	}
	return total
}
