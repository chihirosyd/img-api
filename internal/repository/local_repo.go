package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"img-api/internal/config"
	"img-api/internal/logger"
	"img-api/internal/model"
)

// LocalRepository 基于本地磁盘文件的图片仓库。
//
// 目录约定：
//
//	resources/local/{pc|pe}/{category}/
//
// 索引机制（storage/index/local.json）：
//   - 首次创建仓库时，若索引文件不存在则自动扫描目录生成（仅这一次）
//   - LOCAL_INDEX_REFRESH 配置自动刷新计划（duration/@ 描述符/5 字段 cron），
//     0 或留空 = 不自动刷新
//   - 随机选取优先使用内存索引；索引中没有该分类时回退到直接扫描目录
//     （兼容刚放入、尚未被索引收录的新分类）
//
// 示例：
//
//	resources/local/pc/default/
//	  sunset.jpg
//	  mountain.png
type LocalRepository struct {
	rootPath  string // 本地图片根目录（如 resources/local）
	indexFile string // 索引文件路径（如 storage/index/local.json）

	mu     sync.RWMutex        // 保护 images
	images map[string][]string // 内存索引：key "pc/default" → 绝对路径列表

	cron *cron.Cron // 索引自动刷新调度器（nil = 未启用；Stop 时关闭）

	// 负缓存：曾扫描判定"目录不存在"的分类（key "pc/cat" → 判定时间）。
	// 30s 内不再重复递归扫描；命中时仅做一次轻量 os.Stat 复核，
	// 目录一出现（新增分类）立即恢复扫描，保持"新增分类即时可用"的承诺。
	negMu sync.Mutex
	negAt map[string]time.Time

	// negSwept 上次全量清理时间（清理限频），negSweeps 全量清理次数（仅测试观测），
	// 均由 negMu 保护。
	negSwept  time.Time
	negSweeps int

	// refreshMu 串行化索引重建：cron 定时刷新与 SIGHUP 手动重建可能并发，
	// 交错执行会出现"内存=A、磁盘=B"的不一致（重启后加载到另一份索引）。
	refreshMu sync.Mutex

	// rootMissingWarnAt 上次"根目录不可见"告警时间：刷新可能每 30s 一次，
	// 同样的告警需要限频（refreshMu 保护）。
	rootMissingWarnAt time.Time
}

// negCacheTTL 是负缓存的有效时长。
const negCacheTTL = 30 * time.Second

// negSweepThreshold 控制负缓存全量清理的触发时机：常规写入只做 map 赋值，
// 仅当条目数超过它或距上次清理满一个 TTL 时才全扫一次（key 来自用户传入的
// category，每次写入都全扫会随条目数退化成 O(n)）。
const negSweepThreshold = 1024

// NewLocalRepository 创建本地文件仓库并立即加载或生成索引。
//
// rootPath  — resources/local 目录
// indexFile — local.json 索引文件路径（不存在则扫描生成）
func NewLocalRepository(rootPath, indexFile string) *LocalRepository {
	r := &LocalRepository{
		rootPath:  rootPath,
		indexFile: indexFile,
		images:    make(map[string][]string),
		negAt:     make(map[string]time.Time),
	}

	// 优先加载已有索引；不存在/损坏则扫描目录生成（仅启动这一次）
	if err := r.loadIndex(); err != nil {
		logger.L.Info("local index missing, building on first start", "error", err)
		if err := r.refreshIndex(); err != nil {
			logger.L.Warn("local index build failed, will fall back to direct scan", "error", err)
		}
	}

	// 可选：按计划表自动刷新索引（config.C 判空便于单元测试直接构造仓库）
	if config.C != nil {
		if schedule, ok := parseRefreshSchedule(config.C.LocalIndexRefresh); ok {
			if err := r.startRefreshScheduler(schedule); err != nil {
				logger.L.Warn("local index refresh schedule invalid, auto refresh disabled", "error", err)
			}
		}
	}

	return r
}

// Name 实现 ImageRepository 接口。
func (r *LocalRepository) Name() string { return "local" }

// Random 从指定分类和设备目录中随机选择一个图片文件。
//
// 读取策略：
//  1. 优先从内存索引（由 local.json 加载）中读取
//  2. 索引中没有该分类 → 直接扫描目录兜底（新增分类即时可用）
func (r *LocalRepository) Random(ctx context.Context, category string, deviceType model.DeviceType) (*model.Image, error) {
	// Service 层已解析默认分类（含 LOCAL_DEFAULT_CATEGORY 配置），这里仅兜底空值
	if category == "" {
		category = "default"
	}

	deviceDir := "pc"
	if deviceType == model.DevicePE {
		deviceDir = "pe"
	}

	key := deviceDir + "/" + category

	// 第 1 层：内存索引
	r.mu.RLock()
	files := r.images[key]
	r.mu.RUnlock()

	// 第 2 层：索引未命中 → 直接扫描目录（带"目录不存在"负缓存）
	if len(files) == 0 {
		dir := filepath.Join(r.rootPath, deviceDir, category)

		// 负缓存命中：曾判定目录不存在。仅做一次轻量 Stat 复核——
		// 目录一出现（新增分类）立即清缓存走正常扫描，保持"新增分类即时可用"
		if r.negativeRecently(key) {
			if _, statErr := os.Stat(dir); statErr == nil {
				r.clearNegative(key)
			} else {
				return nil, &model.ErrNoImage{Source: "local", Category: category}
			}
		}

		var err error
		files, err = r.scanImages(dir)
		if err != nil {
			// 目录不存在 → 记负缓存并返回分类不存在（哨兵错误供 Handler 渲染 404 提示页）；
			// 其他 IO 错误原样返回（Handler 按 500 处理）
			if os.IsNotExist(err) {
				r.rememberNegative(key)
				return nil, &model.ErrNoImage{Source: "local", Category: category}
			}
			return nil, fmt.Errorf("local scan %s: %w", dir, err)
		}
	}
	if len(files) == 0 {
		return nil, &model.ErrNoImage{Source: "local", Category: category}
	}

	idx := rand.IntN(len(files))
	path := files[idx]

	logger.L.Debug("local random pick",
		"category", category,
		"device", deviceType,
		"total", len(files),
		"pick_file", path,
	)

	return &model.Image{
		URL:      path,
		Category: category,
	}, nil
}

// ── 负缓存（"目录不存在"判定，30 秒）──

// negativeRecently 判断 key 是否处于"目录不存在"负缓存期内。
func (r *LocalRepository) negativeRecently(key string) bool {
	r.negMu.Lock()
	defer r.negMu.Unlock()
	at, ok := r.negAt[key]
	if !ok {
		return false
	}
	if time.Since(at) >= negCacheTTL {
		delete(r.negAt, key)
		return false
	}
	return true
}

// rememberNegative 记录"目录不存在"判定。
//
// 清理不是每次写入都做：key 由用户传入的 category 决定，常规写入只做一次 map
// 赋值；仅当距上次清理满一个 TTL，或条目数超过 negSweepThreshold 时才全扫一次，
// 必要时再清退最旧的一批，保证锁内扫描次数与写入次数脱钩。
func (r *LocalRepository) rememberNegative(key string) {
	r.negMu.Lock()
	defer r.negMu.Unlock()
	now := time.Now()
	r.negAt[key] = now

	if now.Sub(r.negSwept) < negCacheTTL && len(r.negAt) <= negSweepThreshold {
		return
	}
	r.sweepNegLocked(now)
	if len(r.negAt) > negSweepThreshold {
		r.trimNegLocked(negSweepThreshold / 2)
	}
}

// sweepNegLocked 清除已过期条目并记录清理时间（调用方须持有 negMu）。
func (r *LocalRepository) sweepNegLocked(now time.Time) {
	for k, at := range r.negAt {
		if now.Sub(at) >= negCacheTTL {
			delete(r.negAt, k)
		}
	}
	r.negSwept = now
	r.negSweeps++
}

// trimNegLocked 把条目数压到 target，淘汰最旧的一批（调用方须持有 negMu）。
// 丢失负缓存只会让该分类多扫一次目录，不改变对外可见的 TTL 语义。
func (r *LocalRepository) trimNegLocked(target int) {
	keys := make([]string, 0, len(r.negAt))
	for k := range r.negAt {
		keys = append(keys, k)
	}
	if len(keys) <= target {
		return
	}
	sort.Slice(keys, func(i, j int) bool { return r.negAt[keys[i]].Before(r.negAt[keys[j]]) })
	for _, k := range keys[:len(keys)-target] {
		delete(r.negAt, k)
	}
}

// clearNegative 清除 key 的负缓存（目录已出现，如新增分类）。
func (r *LocalRepository) clearNegative(key string) {
	r.negMu.Lock()
	defer r.negMu.Unlock()
	delete(r.negAt, key)
}

// Health 检查本地图库是否真的可用。
//
// 判定口径：
//   - 根目录不存在 → 视为"尚未配置本地图库"（非故障）；是否参与 /health 的最终
//     判定由 service.SourceEmpty 决定（没有内容的图源本就是信息性状态）。
//   - 根目录存在 → pc/pe 两个设备目录必须存在、是目录、且可读；缺失或不可读
//     返回 error（此时确实取不出图，必须让 /health 看出来）。
//
// 注意：不能只 os.Stat 根目录——resources/local 由仓库骨架 / entrypoint 保证存在，
// 那样检查会恒返回"健康"，即使 pc/pe 子目录缺失、一张图都取不出来也报健康。
func (r *LocalRepository) Health(ctx context.Context) error {
	if _, err := os.Stat(r.rootPath); err != nil {
		if os.IsNotExist(err) {
			// 根目录不存在 = 还没有任何本地图片，与 Random 的语义一致（非故障）
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

// ──────────────────────────────────────────────
// 索引管理
// ──────────────────────────────────────────────

// loadIndex 从 local.json 读取索引到内存。文件不存在或格式错误返回 error。
func (r *LocalRepository) loadIndex() error {
	data, err := os.ReadFile(r.indexFile)
	if err != nil {
		return err
	}

	var images map[string][]string
	if err := json.Unmarshal(data, &images); err != nil {
		return fmt.Errorf("parse index: %w", err)
	}

	r.mu.Lock()
	r.images = images
	r.mu.Unlock()

	logger.L.Info("local index loaded", "file", r.indexFile, "categories", len(images))
	return nil
}

// ScanLocalImages 扫描本地图片目录，返回分类索引（key "pc/default" → 绝对路径列表）。
//
// 与 build-index 命令共用同一扫描函数，结果保持一致。
// 错误语义：
//   - 根目录不存在 → 视为"暂无图片"（首次启动正常场景），返回空索引 + nil 错误
//   - 其他扫描错误（权限、IO 故障等）→ 记录首个错误并返回，调用方应保留旧索引，
//     避免用不完整/空索引覆盖磁盘上有效的 local.json
func ScanLocalImages(rootPath string) (map[string][]string, error) {
	images, _, err := scanLocalImages(rootPath)
	return images, err
}

// scanLocalImages 是 ScanLocalImages 的实现，额外返回"根目录当前不可见"标记：
// 调用方据此区分"根目录缺失"（网络盘瞬时掉线、路径配错）与"根目录存在但没有
// 任何图片"——前者的空扫描结果不可信，不能用来覆盖有效索引。
func scanLocalImages(rootPath string) (map[string][]string, bool, error) {
	images := make(map[string][]string)

	// 根目录单独判定：WalkDir 会把根上的错误混进子目录错误里回调，
	// 分不清是"根目录不在"还是"扫描途中某个子目录消失"
	if _, err := os.Stat(rootPath); err != nil {
		if os.IsNotExist(err) {
			return images, true, nil
		}
		return images, false, err
	}

	var firstErr error

	_ = filepath.WalkDir(rootPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// 根目录不存在属正常场景（首次启动），不记为错误；
			// 其余错误记录但继续扫描（部分结果不用于覆盖索引）
			if firstErr == nil && !os.IsNotExist(err) {
				firstErr = err
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if !model.ImageExts[ext] {
			return nil
		}

		// 相对路径 pc/default/xxx.jpg → key "pc/default"
		rel, relErr := filepath.Rel(rootPath, path)
		if relErr != nil {
			if firstErr == nil {
				firstErr = relErr
			}
			return nil
		}
		parts := strings.SplitN(filepath.ToSlash(rel), "/", 3)
		if len(parts) >= 3 {
			key := parts[0] + "/" + parts[1]
			images[key] = append(images[key], path)
		}
		return nil
	})

	return images, false, firstErr
}

// RebuildIndex 立即重建索引（扫描磁盘 + 原子写回 local.json + 更新内存索引）。
//
// 供服务运行期按需触发（如收到 SIGHUP）：取代"exec build-index 再重启服务"
// 的两步运维流程。扫描失败时保留旧索引并返回错误（不会用空索引覆盖磁盘上的有效索引）。
func (r *LocalRepository) RebuildIndex() error {
	logger.L.Info("local index rebuild requested")
	return r.refreshIndex()
}

// refreshIndex 扫描整个 local 目录，更新内存索引并写回 local.json。
// 扫描出错、或根目录当前不可见且已有非空索引时保留旧索引（内存+磁盘）并返回
// 错误；当前索引本身为空时即使根目录不存在也生成空索引（不报错），
// 使首次启动也能正常创建文件。
func (r *LocalRepository) refreshIndex() error {
	// 串行化：cron 刷新与 SIGHUP 重建并发时，避免"内存=A、磁盘=B"的错配
	r.refreshMu.Lock()
	defer r.refreshMu.Unlock()

	images, rootMissing, scanErr := scanLocalImages(r.rootPath)
	if scanErr != nil {
		// 扫描失败（目录瞬时不可读等）：保留旧索引，不覆盖磁盘上有效的 local.json
		logger.L.Warn("local index scan failed, keeping existing index", "error", scanErr)
		return fmt.Errorf("scan local images: %w", scanErr)
	}

	// 根目录不存在（网络盘瞬时掉线、路径配错）与"根目录存在但没有图片"是两种语义：
	// 前者扫出的空结果不可信，内存里已有非空索引时保留旧索引（不覆盖内存、不落盘），
	// 否则一次掉线就会把有效索引清空；当前索引本身为空时仍照常落盘，
	// 避免首次启动（根目录尚未创建）永远建不出索引。
	if rootMissing && len(images) == 0 && r.hasIndex() {
		r.warnRootMissing()
		return fmt.Errorf("scan local images: %w", errLocalRootMissing)
	}

	// 更新内存索引
	r.mu.Lock()
	r.images = images
	r.mu.Unlock()

	// 写回索引文件（失败仅告警，不影响内存索引生效）
	if err := WriteLocalIndex(r.indexFile, images); err != nil {
		logger.L.Warn("write local index failed", "error", err)
		return err
	}

	logger.L.Info("local index refreshed", "categories", len(images))
	return nil
}

// errLocalRootMissing 表示本地图片根目录当前不可见（不存在 / 网络盘掉线）：
// 此时扫描结果为空但不可信，refreshIndex 保留旧索引而不是用它覆盖。
var errLocalRootMissing = errors.New("local images root missing")

// rootMissingWarnInterval 是"根目录不可见"告警的最小间隔：
// 索引刷新可能每 30s 一次，同样的告警限频输出。
const rootMissingWarnInterval = 5 * time.Minute

// hasIndex 判断内存中是否已有非空索引。
func (r *LocalRepository) hasIndex() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.images) > 0
}

// warnRootMissing 提示根目录当前不可见（调用方须持有 refreshMu）。
func (r *LocalRepository) warnRootMissing() {
	now := time.Now()
	if !r.rootMissingWarnAt.IsZero() && now.Sub(r.rootMissingWarnAt) < rootMissingWarnInterval {
		return
	}
	r.rootMissingWarnAt = now
	logger.L.Warn("local images root not visible, keeping existing index", "root", r.rootPath)
}

// WriteLocalIndex 将分类索引原子写入指定文件（JSON，键按字典序），
// 供主服务（refreshIndex）与 build-index 命令共用，保证两处行为一致。
//
// 原子写入：同目录随机临时文件 + Sync 落盘 + 重命名——避免固定 .tmp 并发冲突
// 与掉电导致索引损坏。
func WriteLocalIndex(indexFile string, images map[string][]string) error {
	dir := filepath.Dir(indexFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(images, "", "  ")
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".local.json.tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// 先注册清理，再按需取消：任何提前返回都不残留临时文件
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// CreateTemp 建的是 0600：Rename 会带走这个权限，导致另一用户运行的
	// build-index 或备份脚本读不到索引。索引不含敏感信息，显式放宽到 0644。
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmpName, indexFile); err != nil {
		return err
	}
	removeTmp = false
	return nil
}

// startRefreshScheduler 启动 cron 调度器，按计划表刷新索引。
// 默认本地时区：@daily 等描述符按服务器本地时间计算。
func (r *LocalRepository) startRefreshScheduler(schedule string) error {
	c := cron.New()
	if _, err := c.AddFunc(schedule, func() {
		if err := r.refreshIndex(); err != nil {
			logger.L.Warn("scheduled local index refresh failed", "error", err)
		}
	}); err != nil {
		return err
	}
	c.Start()
	r.cron = c
	logger.L.Info("local index auto refresh enabled", "schedule", schedule)
	return nil
}

// Stop 停止索引自动刷新调度器（服务优雅关闭时调用，防止 goroutine 泄漏）。
// 可重复调用；未启用调度器时为空操作。
func (r *LocalRepository) Stop() {
	if r.cron != nil {
		r.cron.Stop()
	}
}

// parseRefreshSchedule 将配置值归一化为 cron 表达式。
//
// 支持格式（空或 0 = 不自动刷新）：
//   - Go duration（如 30s / 30m / 24h / 168h；单位仅 ns/µs/ms/s/m/h，
//     天写 24h、周写 168h，月/年长度不固定需用 @ 描述符或 cron）
//   - @ 描述符（@hourly / @daily / @weekly / @monthly / @yearly / @every 30m）
//   - 标准 5 字段 cron（如 0 3 * * * = 每天 03:00）
//
// 第二个返回值表示是否启用（false = 关闭自动刷新）。
func parseRefreshSchedule(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "0" {
		return "", false
	}
	// Go duration → @every（30s / 30m / 24h / 168h 等；保留用户原始写法，比 Duration.String() 更易读）
	if d, err := time.ParseDuration(raw); err == nil {
		if d <= 0 {
			return "", false // 0s / 负值 → 视为关闭
		}
		return "@every " + raw, true
	}
	// 其余原样交给 cron 解析（@daily / @weekly / 5 字段 cron 等）
	return raw, true
}

// scanImages 递归列出目录下的所有图片文件（含子目录，与 ScanLocalImages 的
// 索引规则一致：分类目录内的子文件夹图片也归入该分类）。
// 返回绝对路径列表；目录不存在时返回 os.ErrNotExist 类错误。
func (r *LocalRepository) scanImages(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if model.ImageExts[ext] {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}
