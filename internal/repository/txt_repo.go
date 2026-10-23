package repository

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"img-api/internal/cache"
	"img-api/internal/logger"
	"img-api/internal/model"
)

// TxtRepository 基于 TXT 文件的图片 URL 仓库。
//
// 目录约定：resources/txt/{pc|pe}/{category}.txt
// TXT 格式：每行一个 URL，# 开头为注释，空行自动跳过。
//
// 两级读取策略（Redis + 文件降级）：
//  1. 优先 Redis Set → SRandMember O(1) 随机
//  2. Redis 未命中 → 从 TXT 文件读取（带 mtime 校验的内存缓存，
//     避免每个请求全量重读文件；管理员修改文件后即时生效）
//
// Redis 故障冷却：SRandMember 失败后进入 redisCooldown 冷却期，
// 期间直走文件通道，避免 Redis 宕机时每个请求都等待超时。
type TxtRepository struct {
	rootPath string      // TXT 文件根目录（如 resources/txt）
	cache    cache.Cache // 可选缓存（nil 表示跳过 Redis）

	// 文件内容内存缓存：path → 内容快照（mtime+size 校验，变化时自动重读）
	fileMu    sync.RWMutex
	fileCache map[string]txtFileCache

	// Redis 故障冷却状态（见 shouldTryRedis / noteRedisFailure）
	redisMu       sync.Mutex
	redisFailAt   time.Time // 最近一次 Redis 失败时间（冷却期内直走文件）
	redisLogAt    time.Time // 最近一次告警日志时间（限频：每分钟最多一条）
}

// redisCooldown 是 Redis 失败后的冷却时长：期间不再尝试 Redis，
// 直接读文件。冷却结束后下一次请求会重新探测（失败则再次冷却）。
const redisCooldown = 30 * time.Second

// txtFileCache 是单个 TXT 文件的缓存快照。
type txtFileCache struct {
	mtime time.Time
	size  int64
	lines []string
}

// NewTxtRepository 创建 TXT 仓库，可传入缓存实例用于 Redis Set 加速。
func NewTxtRepository(rootPath string, c cache.Cache) *TxtRepository {
	return &TxtRepository{
		rootPath:  rootPath,
		cache:     c,
		fileCache: make(map[string]txtFileCache),
	}
}

// Name 实现 ImageRepository 接口。
func (r *TxtRepository) Name() string { return "txt" }

// Random 从指定分类和设备类型的 TXT 文件中随机选一行。
//
// 读取策略：
//  1. 优先从 Redis img:{pc|pe}:{category} Set 中用 SRandMember
//  2. Redis 未命中/不可用 → 回退到直接读取 TXT 文件
func (r *TxtRepository) Random(ctx context.Context, category string, deviceType model.DeviceType) (*model.Image, error) {
	// Service 层已解析默认分类（含 TXT_DEFAULT_CATEGORY 配置），这里仅兜底空值
	if category == "" {
		category = "default"
	}

	deviceDir := "pc"
	if deviceType == model.DevicePE {
		deviceDir = "pe"
	}

	// ── 第 1 层：Redis Set 快速通道（故障冷却期内跳过）──
	if r.cache != nil && r.shouldTryRedis() {
		redisKey := fmt.Sprintf("img:%s:%s", deviceDir, category)
		url, err := r.cache.SRandMember(ctx, redisKey)
		if err == nil && url != "" && IsValidImageURL(url) {
			logger.L.Debug("txt redis hit", "key", redisKey, "url", url)
			return &model.Image{URL: url, Category: category}, nil
		}
		if err != nil && !errors.Is(err, cache.Nil) {
			// 客户端取消/超时不是 Redis 故障：否则一次刷新/断网就会让全站
			// 绕过 Redis 30 秒，并打出一条"Redis unavailable"的假告警。
			if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				// Redis 真实故障（Nil=键不存在，属正常）：进入冷却并限频告警
				r.noteRedisFailure(err)
			}
		}
	}

	// ── 第 2 层：TXT 文件回退 ──
	txtPath := filepath.Join(r.rootPath, deviceDir, category+".txt")

	lines, err := r.readLines(txtPath)
	if err != nil {
		// 文件不存在 → 分类不存在（哨兵错误供 Handler 渲染 404 提示页）；
		// 其他 IO 错误原样返回（Handler 按 500 处理，避免掩盖真实故障）
		if os.IsNotExist(err) {
			return nil, &model.ErrNoImage{Source: "txt", Category: category}
		}
		return nil, fmt.Errorf("txt read %s: %w", txtPath, err)
	}
	if len(lines) == 0 {
		return nil, &model.ErrNoImage{Source: "txt", Category: category}
	}

	// 随机选择一行
	idx := rand.IntN(len(lines))
	url := strings.TrimSpace(lines[idx])

	logger.L.Debug("txt random pick",
		"category", category,
		"device", deviceType,
		"total", len(lines),
		"pick_index", idx,
	)

	return &model.Image{
		URL:      url,
		Category: category,
	}, nil
}

// Health 检查 TXT 图库是否真的可用。
//
// 判定口径与 LocalRepository.Health 一致：
//   - 根目录不存在 → 视为"尚未配置 TXT 图库"（非故障），是否计入 /health 由
//     service.SourceEmpty 决定；
//   - 根目录存在 → pc/pe 两个设备目录必须存在、是目录、且可读。
//
// 不能只 os.Stat 根目录：该目录由部署骨架保证存在，恒返回"健康"会让 /health
// 在 pc/pe 子目录缺失（取不到任何图）时报 ok。
func (r *TxtRepository) Health(ctx context.Context) error {
	if _, err := os.Stat(r.rootPath); err != nil {
		if os.IsNotExist(err) {
			// 根目录不存在 = 还没有任何 TXT 图库，与 Random 的语义一致（非故障）
			return nil
		}
		return err
	}
	for _, device := range []string{"pc", "pe"} {
		if err := checkDeviceDir(filepath.Join(r.rootPath, device), device); err != nil {
			return err
		}
	}
	return nil
}

// checkDeviceDir 校验设备目录（pc/pe）确实是可读目录。
//
// 只 os.Stat 不够：路径是普通文件、目录没有读权限（挂载掉线、权限被改）时
// Stat 仍然成功，而 Random 会失败——健康检查便成了"报健康但取不到图"。
func checkDeviceDir(dir, device string) error {
	fi, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("设备目录缺失: %s", device)
	}
	if !fi.IsDir() {
		return fmt.Errorf("设备目录不是目录: %s", device)
	}
	if _, err := os.ReadDir(dir); err != nil {
		return fmt.Errorf("设备目录不可读: %s: %w", device, err)
	}
	return nil
}

// ReadTxtLines 读取 TXT 文件并过滤空行、# 注释行与非法 URL 行，
// 返回有效行列表。
//
// 供 TxtRepository（带 mtime 缓存）与 sync-redis 命令共用，
// 使两处对 TXT 文件的解析规则保持一致。
func ReadTxtLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	// 默认 64KB 行上限对 URL 行太小：调到 1MB（超过则报"行超长"错误，
	// 而不是静默截断导致 URL 损坏）
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue // 跳过空行和注释
		}
		// 仅接受 http/https URL：非法协议（file:、javascript: 等）与含控制字符
		// （如 CR/LF 头注入）的行直接跳过，保证 mode=redirect 的 Location 安全
		if !IsValidImageURL(line) {
			continue
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

// IsValidImageURL 校验 URL 可解析且 scheme 为 http/https。
// 供 txt 图库、外部 API 池、分类探测（service/catalog.go）共用口径：
// 只有合法 http(s) 地址才视为有效图片 URL。
func IsValidImageURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https")
}

// readLines 读取文件内容（带 mtime+size 校验的内存缓存）。
// 返回非空的 URL 列表或 I/O 错误。
// 先查缓存；未命中/文件已变化时才真正读盘并更新快照。
func (r *TxtRepository) readLines(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	// 命中缓存（mtime 与 size 均未变）。
	// 注意：返回的切片与缓存共享底层数组，调用方只读、不得修改。
	r.fileMu.RLock()
	if c, ok := r.fileCache[path]; ok && c.mtime.Equal(info.ModTime()) && c.size == info.Size() {
		lines := c.lines
		r.fileMu.RUnlock()
		return lines, nil
	}
	r.fileMu.RUnlock()

	lines, err := ReadTxtLines(path)
	if err != nil {
		return nil, err
	}

	// 更新缓存快照
	r.fileMu.Lock()
	r.fileCache[path] = txtFileCache{
		mtime: info.ModTime(),
		size:  info.Size(),
		lines: lines,
	}
	r.fileMu.Unlock()

	return lines, nil
}

// shouldTryRedis 判断当前是否应尝试 Redis 快速通道。
// 故障冷却期内返回 false（直走文件），冷却结束后恢复探测。
func (r *TxtRepository) shouldTryRedis() bool {
	r.redisMu.Lock()
	defer r.redisMu.Unlock()
	return time.Since(r.redisFailAt) >= redisCooldown
}

// noteRedisFailure 记录 Redis 失败时间（进入冷却），并按分钟限频输出告警日志，
// 让运维能感知运行期 Redis 故障（而非静默降级）。
func (r *TxtRepository) noteRedisFailure(err error) {
	r.redisMu.Lock()
	defer r.redisMu.Unlock()
	now := time.Now()
	r.redisFailAt = now
	if now.Sub(r.redisLogAt) >= time.Minute {
		r.redisLogAt = now
		logger.L.Warn("redis unavailable, cooling down; serving from txt files",
			"cooldown", redisCooldown, "error", err)
	}
}
