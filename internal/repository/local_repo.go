package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
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
//   resources/local/{pc|pe}/{category}/
//
// 索引机制（storage/index/local.json）：
//   - 首次创建仓库时，若索引文件不存在则自动扫描目录生成（仅这一次）
//   - LOCAL_INDEX_REFRESH 配置自动刷新计划（duration/@ 描述符/5 字段 cron），
//     0 或留空 = 不自动刷新
//   - 随机选取优先使用内存索引；索引中没有该分类时回退到直接扫描目录
//     （兼容刚放入、尚未被索引收录的新分类）
//
// 示例：
//   resources/local/pc/default/
//     sunset.jpg
//     mountain.png
type LocalRepository struct {
	rootPath  string // 本地图片根目录（如 resources/local）
	indexFile string // 索引文件路径（如 storage/index/local.json）

	mu     sync.RWMutex       // 保护 images
	images map[string][]string // 内存索引：key "pc/default" → 绝对路径列表

	cron *cron.Cron // 索引自动刷新调度器（nil = 未启用；Stop 时关闭）

	// 负缓存：曾扫描判定"目录不存在"的分类（key "pc/cat" → 判定时间）。
	// 30s 内不再重复递归扫描；命中时仅做一次轻量 os.Stat 复核，
	// 目录一出现（新增分类）立即恢复扫描，保持"新增分类即时可用"的承诺。
	negMu sync.Mutex
	negAt map[string]time.Time
}

// negCacheTTL 是负缓存的有效时长。
const negCacheTTL = 30 * time.Second

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

// rememberNegative 记录"目录不存在"判定，并顺手清理过期条目（限制 map 增长）。
func (r *LocalRepository) rememberNegative(key string) {
	r.negMu.Lock()
	defer r.negMu.Unlock()
	now := time.Now()
	r.negAt[key] = now
	for k, at := range r.negAt {
		if now.Sub(at) >= negCacheTTL {
			delete(r.negAt, k)
		}
	}
}

// clearNegative 清除 key 的负缓存（目录已出现，如新增分类）。
func (r *LocalRepository) clearNegative(key string) {
	r.negMu.Lock()
	defer r.negMu.Unlock()
	delete(r.negAt, key)
}

// Health 检查本地图片根目录是否存在。
func (r *LocalRepository) Health(ctx context.Context) error {
	_, err := os.Stat(r.rootPath)
	return err
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
	images := make(map[string][]string)
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

	return images, firstErr
}

// refreshIndex 扫描整个 local 目录，更新内存索引并写回 local.json。
// 扫描出错时保留旧索引（内存+磁盘），返回错误；目录不存在时生成空索引
// （不报错），使首次启动也能正常创建文件。
func (r *LocalRepository) refreshIndex() error {
	images, scanErr := ScanLocalImages(r.rootPath)
	if scanErr != nil {
		// 扫描失败（目录瞬时不可读等）：保留旧索引，不覆盖磁盘上有效的 local.json
		logger.L.Warn("local index scan failed, keeping existing index", "error", scanErr)
		return fmt.Errorf("scan local images: %w", scanErr)
	}

	// 更新内存索引
	r.mu.Lock()
	r.images = images
	r.mu.Unlock()

	// 写回索引文件（失败仅告警，不影响内存索引生效）
	if err := r.writeIndex(images); err != nil {
		logger.L.Warn("write local index failed", "error", err)
		return err
	}

	logger.L.Info("local index refreshed", "categories", len(images))
	return nil
}

// writeIndex 将索引序列化写入 local.json（与 build-index 命令输出格式一致，可互换使用）。
// 原子写入：同目录随机临时文件 + Sync 落盘 + 重命名——避免固定 .tmp 并发冲突
// 与掉电导致索引损坏。
func (r *LocalRepository) writeIndex(images map[string][]string) error {
	dir := filepath.Dir(r.indexFile)
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
	defer os.Remove(tmp.Name()) // Rename 成功后此清理为 no-op

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
	return os.Rename(tmp.Name(), r.indexFile)
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
