// sync-redis — 将 TXT 图库中的图片 URL 批量同步到 Redis Set。
//
// 每个 TXT 文件对应一个 Redis Set（key: img:{pc|pe}:{category}），
// 之后 TxtRepository 可通过 SRandMember 实现 O(1) 随机选取。
//
// 用法：
//
//	go run ./cmd/sync-redis/
//	go run ./cmd/sync-redis/ -dir resources/txt
//
// 退出码：0 全部成功 / 1 完全没有同步（配置、连接或图库不可用）/ 2 部分成功。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"img-api/internal/app"
	"img-api/internal/cache"
	"img-api/internal/config"
	"img-api/internal/logger"
	"img-api/internal/repository"
)

// 退出码契约（C-04）：调用方与 CI 据此判断同步是否真的完成。
const (
	// exitOK：所有设备目录都扫描成功，且每个分类都写入成功
	exitOK = 0
	// exitFatal：完全没有同步（配置/连接失败、图库目录不可用、一个 .txt 都没找到）
	exitFatal = 1
	// exitPartial：部分成功（至少一个分类已写入 Redis，但同时存在失败项）
	exitPartial = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run 是 main 的可测试实现：返回退出码而不是直接 os.Exit，
// 让"Redis 未配置""部分失败"等路径能在测试里断言（C-04）。
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("sync-redis", flag.ContinueOnError)
	fs.SetOutput(stderr)
	txtDir := fs.String("dir", "resources/txt", "TXT 图库根目录")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK // -h 是主动查看用法，不算失败
		}
		return exitFatal
	}

	// 加载配置（需要 Redis 连接信息）
	rootPath := app.RootPath()
	if err := config.Load(rootPath); err != nil {
		fmt.Fprintf(stderr, "❌ load config: %v\n", err)
		return exitFatal
	}
	_ = logger.Init(config.C.LogLevel, filepath.Join(rootPath, "storage", "logs"), config.C.LogMaxAge, config.C.LogMaxSize)
	// 关掉日志文件句柄：进程退出前的优雅收尾（Windows 上未关闭的句柄会阻止删除日志文件）
	defer logger.Close()

	// 检查 Redis 是否已配置：未配置 = 什么都没同步，必须非零退出
	if !config.C.IsRedisEnabled() {
		fmt.Fprintln(stderr, "❌ Redis 未配置（REDIS_ADDR 为空），没有任何分类被同步。")
		fmt.Fprintln(stderr, "   主服务将直接从 TXT 文件读取图片 URL。")
		fmt.Fprintln(stderr, "   如需启用 Redis 加速，请在 .env 中设置 REDIS_ADDR。")
		return exitFatal
	}

	// 连接 Redis
	redisCache, err := cache.NewRedisCache(config.C.RedisAddr, config.C.RedisPassword, config.C.RedisDB)
	if err != nil {
		fmt.Fprintf(stderr, "❌ Redis 连接失败: %v\n", err)
		return exitFatal
	}
	defer redisCache.Close()

	ctx := context.Background()
	// -dir 支持绝对路径（避免 filepath.Join 将绝对路径拼接到 rootPath 后）
	fullDir := *txtDir
	if !filepath.IsAbs(fullDir) {
		fullDir = filepath.Join(rootPath, fullDir)
	}

	st := runStats{}
	// 遍历 pc/ 和 pe/ 子目录
	for _, device := range []string{"pc", "pe"} {
		r := syncDevice(ctx, redisCache, filepath.Join(fullDir, device), device)
		st.urls += r.urls
		st.failures += r.failures
	}

	fmt.Fprintf(stdout, "\n%s\n", st.summaryLine())
	return st.exitCode()
}

// runStats 汇总整次同步的结果，并把"成败"翻译成汇总行与退出码。
// 抽成纯数据 + 纯函数，让 C-04 的退出码契约能在没有真实 Redis 的环境里单测。
type runStats struct {
	urls     int // 成功写入 Redis 的 URL 总数
	failures int // 失败项数：目录不可用、无 .txt、分类写入/替换失败、孤儿扫描失败
}

// exitCode 按 C-04 契约映射：无失败 → 0；全部失败 → 1；部分成功 → 2。
func (s runStats) exitCode() int {
	switch {
	case s.failures == 0:
		return exitOK
	case s.urls > 0:
		return exitPartial
	default:
		return exitFatal
	}
}

// summaryLine 如实反映成败：只在完全没有失败时才打印"🎉 同步完成"。
func (s runStats) summaryLine() string {
	switch s.exitCode() {
	case exitOK:
		return fmt.Sprintf("🎉 同步完成！共 %d 张图片 URL 导入 Redis", s.urls)
	case exitPartial:
		return fmt.Sprintf("⚠️  同步部分成功：%d 张图片 URL 已导入 Redis，另有 %d 项失败（退出码 %d）",
			s.urls, s.failures, exitPartial)
	default:
		return fmt.Sprintf("❌ 同步失败：没有任何分类写入 Redis（%d 项失败，退出码 %d）", s.failures, exitFatal)
	}
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

// deviceSync 是一次设备目录同步的结果。
type deviceSync struct {
	urls     int // 成功写入 Redis 的 URL 总数
	failures int // 失败项数（目录不可用 / 无 .txt / 分类写入失败 / 孤儿扫描失败）
}

// syncDevice 把一个设备目录（pc/pe）下的所有 .txt 分类同步到 Redis。
//
// 安全要点（回归 R5-04）：
//   - 读取失败 ≠ 分类已删除：失败分类进 protectedCategories，孤儿清理必须跳过它
//   - 文件存在但没有有效 URL 才算"空分类"，其旧集合交给孤儿清理删除
//   - 写入用「临时 key + RENAME」原子替换，SAdd 失败时旧集合不受影响
//   - 一个 .txt 都没找到时跳过孤儿清理（多半是 -dir 指错，不能据此删光），
//     但必须计入失败项，让进程以非零退出码结束（L-04(b)/C-04）
func syncDevice(ctx context.Context, c setCache, deviceDir, device string) deviceSync {
	entries, err := os.ReadDir(deviceDir)
	if err != nil {
		// 目录本身读不到/指错 ≠ 图库为空：既不能据此删集合（数据不可恢复），
		// 也不能静默成功——计入失败项让退出码非零（C-04）
		fmt.Printf("❌ 设备目录不可用 %s: %v\n", deviceDir, err)
		return deviceSync{failures: 1}
	}

	total := 0
	failures := 0
	// validCategories：本次成功同步的分类（其旧集合可安全删除）
	// protectedCategories：读取/写入失败、必须保留现有集合的分类
	validCategories := make(map[string]bool)
	protectedCategories := make(map[string]bool)
	txtSeen := 0 // 找到的 .txt 文件数（含内容为空的），用于判断 -dir 是否指错
	// 分类名一律用原始文件名（只去掉 .txt 后缀，不做小写归一化，L-01(c)）：
	// 主服务 TxtRepository 用请求里的原样分类名拼 key，两边必须一致，否则
	// Anime.txt 会被写成 img:pc:anime，而取图请求 img:pc:Anime 永远打不中。
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(name), ".txt") {
			continue
		}
		txtSeen++

		category := strings.TrimSuffix(name, filepath.Ext(name))
		txtPath := filepath.Join(deviceDir, name)

		// 读取 TXT 文件中的 URL（解析规则与主服务 TxtRepository 一致）
		urls, err := repository.ReadTxtLines(txtPath)
		if err != nil {
			// 读取失败 ≠ 分类已删除：保留 Redis 里现有的集合。
			// 否则一次瞬时 IO 错误就会让下方孤儿清理把线上分类整个删掉。
			protectedCategories[category] = true
			failures++
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
			failures++
			continue
		}
		if err := c.Rename(ctx, tmpKey, redisKey); err != nil {
			fmt.Printf("❌ 替换失败: %s → %v（旧集合保留）\n", redisKey, err)
			_ = c.Delete(ctx, tmpKey)
			protectedCategories[category] = true
			failures++
			continue
		}

		count, _ := c.SCard(ctx, redisKey)
		fmt.Printf("✅ %-30s → %4d URLs\n", redisKey, count)
		total += len(urls)
	}

	// 一个 .txt 都没找到：目录本身是读得到的，但里面一个分类文件都没有，多半是
	// -dir 指错或分类文件被整体挪走。此时清理孤儿 key 会把该设备下所有集合删光
	// （不可逆），因此保留现有集合、跳过清理；但不能静默成功——明确告警并计入
	// 失败项，让退出码非零（L-04(b)/C-04）。
	if txtSeen == 0 {
		fmt.Printf("❌ %s 下没有任何 .txt（-dir 是否正确？），已保留现有集合并跳过孤儿清理\n", deviceDir)
		return deviceSync{failures: failures + 1}
	}

	// 清理孤儿 Set：分类 txt 文件已删除但 Redis 中仍残留的 key，
	// 不清理会导致主服务仍从旧 Set 返回已删除分类的图片。
	// 读取/写入失败的分类在 protectedCategories 里，必须显式跳过。
	keys, scanErr := c.ScanKeys(ctx, "img:"+device+":*")
	if scanErr != nil {
		// 孤儿没清掉 = 本次同步不完整：如实报告失败
		fmt.Printf("❌ 扫描 %s 孤儿 key 失败: %v\n", device, scanErr)
		return deviceSync{urls: total, failures: failures + 1}
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
	return deviceSync{urls: total, failures: failures}
}
