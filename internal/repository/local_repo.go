package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

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
//   - LOCAL_INDEX_REFRESH_MINUTES > 0 时，后台定时重新扫描并刷新索引
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
}

// NewLocalRepository 创建本地文件仓库并立即加载或生成索引。
//
// rootPath  — resources/local 目录
// indexFile — local.json 索引文件路径（不存在则扫描生成）
func NewLocalRepository(rootPath, indexFile string) *LocalRepository {
	r := &LocalRepository{
		rootPath:  rootPath,
		indexFile: indexFile,
		images:    make(map[string][]string),
	}

	// 优先加载已有索引；不存在/损坏则扫描目录生成（仅启动这一次）
	if err := r.loadIndex(); err != nil {
		logger.L.Info("local index missing, building on first start", "error", err)
		if err := r.refreshIndex(); err != nil {
			logger.L.Warn("local index build failed, will fall back to direct scan", "error", err)
		}
	}

	// 可选：定时自动刷新索引
	if min := config.C.LocalIndexRefreshMinutes; min > 0 {
		go r.refreshLoop(time.Duration(min) * time.Minute)
		logger.L.Info("local index auto refresh enabled", "interval_minutes", min)
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

	// 第 2 层：索引未命中 → 直接扫描目录
	if len(files) == 0 {
		dir := filepath.Join(r.rootPath, deviceDir, category)
		var err error
		files, err = r.scanImages(dir)
		if err != nil {
			return nil, fmt.Errorf("local scan %s: %w", dir, err)
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no images found in: %s", filepath.Join(r.rootPath, deviceDir, category))
	}

	idx := rand.Intn(len(files))
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
// 供主服务的自动索引（refreshIndex）与 build-index 命令共用，
// 保证手动重建与自动生成的结果完全一致。
// 目录不存在或不可读时返回空索引（不报错），保持最小可用。
func ScanLocalImages(rootPath string) map[string][]string {
	images := make(map[string][]string)

	_ = filepath.WalkDir(rootPath, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if !model.ImageExts[ext] {
			return nil
		}

		// 相对路径 pc/default/xxx.jpg → key "pc/default"
		rel, relErr := filepath.Rel(rootPath, path)
		if relErr != nil {
			return nil
		}
		parts := strings.SplitN(filepath.ToSlash(rel), "/", 3)
		if len(parts) >= 3 {
			key := parts[0] + "/" + parts[1]
			images[key] = append(images[key], path)
		}
		return nil
	})

	return images
}

// refreshIndex 扫描整个 local 目录，更新内存索引并写回 local.json。
// 目录不存在时生成空索引（不报错），保证首次启动也能正常创建文件。
func (r *LocalRepository) refreshIndex() error {
	images := ScanLocalImages(r.rootPath)

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
// 原子写入：先写临时文件再重命名，避免写入中途崩溃导致索引损坏。
func (r *LocalRepository) writeIndex(images map[string][]string) error {
	if err := os.MkdirAll(filepath.Dir(r.indexFile), 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(images, "", "  ")
	if err != nil {
		return err
	}

	tmp := r.indexFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, r.indexFile)
}

// refreshLoop 后台定时刷新索引（由 NewLocalRepository 在配置启用时启动）。
func (r *LocalRepository) refreshLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		if err := r.refreshIndex(); err != nil {
			logger.L.Warn("scheduled local index refresh failed", "error", err)
		}
	}
}

// scanImages 列出目录下的所有图片文件（仅当前层，不递归子目录）。
// 返回绝对路径列表，不含子目录。
func (r *LocalRepository) scanImages(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if model.ImageExts[ext] {
			files = append(files, filepath.Join(dir, entry.Name()))
		}
	}
	return files, nil
}
