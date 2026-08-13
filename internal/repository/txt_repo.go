package repository

import (
	"bufio"
	"context"
	"fmt"
	"math/rand"
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
// 两级读取策略（匹配 PHP 版本的 Redis + 文件降级）：
//  1. 优先 Redis Set → SRandMember O(1) 随机
//  2. Redis 未命中 → 从 TXT 文件读取（带 mtime 校验的内存缓存，
//     避免每个请求全量重读文件；管理员修改文件后即时生效）
type TxtRepository struct {
	rootPath string      // TXT 文件根目录（如 resources/txt）
	cache    cache.Cache // 可选缓存（nil 表示跳过 Redis）

	// 文件内容内存缓存：path → 内容快照（mtime+size 校验，变化时自动重读）
	fileMu    sync.RWMutex
	fileCache map[string]txtFileCache
}

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
	if category == "" {
		category = "default"
	}

	deviceDir := "pc"
	if deviceType == model.DevicePE {
		deviceDir = "pe"
	}

	// ── 第 1 层：Redis Set 快速通道 ──
	if r.cache != nil {
		redisKey := fmt.Sprintf("img:%s:%s", deviceDir, category)
		if url, err := r.cache.SRandMember(ctx, redisKey); err == nil && url != "" {
			logger.L.Debug("txt redis hit", "key", redisKey, "url", url)
			return &model.Image{URL: url, Category: category}, nil
		}
	}

	// ── 第 2 层：TXT 文件回退 ──
	txtPath := filepath.Join(r.rootPath, deviceDir, category+".txt")

	lines, err := r.readLines(txtPath)
	if err != nil {
		return nil, fmt.Errorf("txt read %s: %w", txtPath, err)
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("txt file is empty: %s", txtPath)
	}

	// 随机选择一行
	idx := rand.Intn(len(lines))
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

// Health 检查 TXT 根目录是否存在（简单 os.Stat 验证）。
func (r *TxtRepository) Health(ctx context.Context) error {
	_, err := os.Stat(r.rootPath)
	return err
}

// ReadTxtLines 读取 TXT 文件并过滤空行与 # 注释行，返回有效行列表。
//
// 供 TxtRepository（带 mtime 缓存）与 sync-redis 命令共用，
// 保证两处对 TXT 文件的解析规则完全一致。
func ReadTxtLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue // 跳过空行和注释
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

// readLines 读取文件内容（带 mtime+size 校验的内存缓存）。
// 返回非空的 URL 列表或 I/O 错误。
// 先查缓存；未命中/文件已变化时才真正读盘并更新快照。
func (r *TxtRepository) readLines(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	// 命中缓存（mtime 与 size 均未变）
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
