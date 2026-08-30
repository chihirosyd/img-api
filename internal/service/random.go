// Package service 实现随机图片的核心业务逻辑。
//
// 请求流程：Handler → 仓库随机选取 → 返回。
// external 来源经熔断器保护，txt/local 直接读取。
//
// 设计：每次请求独立随机，不缓存选中的结果，维持"随机图"语义；
// 熔断器保护外部 API；双重检查锁保护线程安全。
package service

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"img-api/internal/cache"
	"img-api/internal/circuit"
	"img-api/internal/config"
	"img-api/internal/logger"
	"img-api/internal/model"
	"img-api/internal/repository"
)

// RandomService 管理多个图片仓库，提供统一的随机图片获取入口。
type RandomService struct {
	mu           sync.RWMutex
	repos        map[model.SourceType]repository.ImageRepository
	externalPool *repository.ExternalPool
	breakr       *circuit.Breaker
	cache        cache.Cache
	stats        *Stats
	rootPath     string

	// 图源空置检查的短时缓存（30s），避免错误请求风暴时频繁扫描文件系统
	emptyCheckAt   time.Time // 上次检查时间
	sourceEmptyMap map[model.SourceType]bool // 各渠道空置结果（检查时一并计算）

	// 分类清单快照缓存（30s）：CategoryExists / CategoryExistsFor /
	// AvailableCategories / pickExistingCategory 共用，避免错误请求风暴时
	// 对每个分类名单独做文件系统探测（与 emptyCheckAt 相同的缓存策略）
	catAt    time.Time
	catCache categorySnapshot
}

// NewRandomService 创建服务实例。
//
// c     — 缓存实例（Redis 或内存降级，非 nil）
// pool  — 外部 API 池（nil=不支持 external 来源）
// stats — 统计实例（由 main.go 创建并共享）
func NewRandomService(rootPath string, c cache.Cache, pool *repository.ExternalPool, stats *Stats) *RandomService {
	return &RandomService{
		repos:        make(map[model.SourceType]repository.ImageRepository),
		externalPool: pool,
		breakr: circuit.NewBreaker(
			config.C.CircuitFailureThreshold,
			config.C.CircuitTimeoutSeconds,
			config.C.CircuitHalfOpenMax,
		),
		cache:          c,
		stats:          stats,
		rootPath:       rootPath,
		sourceEmptyMap: make(map[model.SourceType]bool),
	}
}

// GetRepo 延迟加载并缓存指定来源的仓库实例（线程安全）。
//
// 使用双重检查锁（double-checked locking）模式：
//  1. 读锁快速检查（99% 的情况）
//  2. 未命中则升级为写锁
//  3. 写锁内二次检查（防止并发初始化）
func (s *RandomService) GetRepo(source model.SourceType) repository.ImageRepository {
	s.mu.RLock()
	if repo, ok := s.repos[source]; ok {
		s.mu.RUnlock()
		return repo
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	// 双检：可能在等锁期间已被其他 goroutine 初始化
	if repo, ok := s.repos[source]; ok {
		return repo
	}

	repo := repository.SourceByName(source, s.rootPath, s.externalPool, s.cache)
	s.repos[source] = repo
	logger.L.Info("repository initialized", "source", source, "name", repo.Name())
	return repo
}

// Random 获取一张随机图片（对外唯一入口）。
//
// Category 支持逗号分隔多选，每次随机选一个分类。
// 每次请求都在仓库层独立随机（不缓存选中的结果），
// 维持"随机图"语义：同一分类的连续请求通常返回不同图片（图库较小时可能重复）。
func (s *RandomService) Random(ctx context.Context, source model.SourceType, apiName, category string, deviceType model.DeviceType) (*model.Image, error) {
	// txt/local 支持通过配置自定义默认分类（external 的默认分类在池内按 API 解析）。
	category = s.ResolveDefaultCategory(source, category)

	// 解析多分类：多分类时只从"实际可用"的分类中随机选（外部渠道按 API 池
	// 白名单过滤），避免选到不存在的分类导致误报 500。
	pickedCategory := s.pickExistingCategory(source, category, deviceType)
	if pickedCategory == "" {
		return nil, &model.ErrCategoryNotSupported{Source: string(source), Category: category}
	}

	// 仓库随机选取
	var img *model.Image
	var err error

	switch source {
	case model.SourceExternal:
		img, err = s.randomFromExternal(ctx, apiName, pickedCategory, deviceType)
	default:
		repo := s.GetRepo(source)
		img, err = repo.Random(ctx, pickedCategory, deviceType)
	}

	if err != nil {
		return nil, fmt.Errorf("random %s/%s: %w", source, category, err)
	}

	return img, nil
}

// randomFromExternal 通过熔断器从外部 API 池获取图片。
// 熔断器打开时直接返回 ErrCircuitOpen，不等待超时。
// 外部 API 池未配置（nil 或空）时返回 ErrExternalNotConfigured，
// 由 Handler 层返回"开始使用"引导页。
// 指定的 API 名称不存在时返回 ErrAPINotFound，由 Handler 层返回 404 提示页。
//
// 注意：池未配置、API 名不存在均属配置问题而非上游故障，
// 因此在调用熔断器前提前返回，避免被误记为"失败"而错误触发熔断。
func (s *RandomService) randomFromExternal(ctx context.Context, apiName, category string, deviceType model.DeviceType) (*model.Image, error) {
	if s.externalPool == nil || len(s.externalPool.APIs()) == 0 {
		return nil, model.ErrExternalNotConfigured
	}

	// 指定的 API 名称不存在：属配置错误而非上游故障，
	// 在进入熔断器前提前返回（避免被误记为失败而污染熔断计数）
	if apiName != "" {
		if _, ok := s.externalPool.FindByName(apiName); !ok {
			return nil, &model.ErrAPINotFound{Name: apiName}
		}
	}

	// 分类白名单预检：指定的 API 或整个池都不支持该分类时提前返回
	// ErrCategoryNotSupported（同样不进入熔断器，避免污染熔断计数）。
	if apiName != "" {
		if !s.externalPool.APISupportsCategory(apiName, category) {
			return nil, &model.ErrCategoryNotSupported{Source: "external", Category: category}
		}
	} else if !s.externalPool.SupportsCategory(category) {
		return nil, &model.ErrCategoryNotSupported{Source: "external", Category: category}
	}

	var img *model.Image
	err := s.breakr.Call(func() error {
		var e error
		img, e = s.externalPool.Random(ctx, apiName, category, deviceType)
		return e
	})

	if errors.Is(err, circuit.ErrCircuitOpen) {
		s.stats.RecordCircuitTrip()
		return nil, fmt.Errorf("external source unavailable (circuit open): %w", err)
	}
	if err != nil {
		return nil, fmt.Errorf("external random (breaker: %s): %w", s.breakr.State(), err)
	}

	return img, nil
}

// Health 检查所有已初始化仓库和外部 API 池的健康状态（线程安全）
func (s *RandomService) Health(ctx context.Context) map[string]string {
	result := make(map[string]string)

	s.mu.RLock()
	for source, repo := range s.repos {
		if err := repo.Health(ctx); err != nil {
			result[string(source)] = fmt.Sprintf("unhealthy: %v", err)
		} else {
			result[string(source)] = "healthy"
		}
	}
	s.mu.RUnlock()

	// 外部 API 池
	if s.externalPool == nil {
		result["external_pool"] = "disabled"
	} else if len(s.externalPool.APIs()) == 0 {
		result["external_pool"] = "unconfigured (no APIs in image.yaml)"
	} else {
		result["external_pool"] = fmt.Sprintf("healthy (%d APIs)", len(s.externalPool.APIs()))
	}

	// 熔断器
	result["circuit_breaker"] = s.breakr.State().String()

	// 缓存
	if s.cache != nil {
		result["cache"] = s.cache.Name()
	} else {
		result["cache"] = "disabled"
	}

	return result
}

// BreakerState 返回熔断器当前状态
func (s *RandomService) BreakerState() string {
	return s.breakr.State().String()
}

// Stats 返回统计实例
func (s *RandomService) Stats() *Stats {
	return s.stats
}

// SourceEmpty 判断指定图源是否未配置内容（按渠道分别检测）。
// Handler 层据此向用户展示"开始使用"引导页。
func (s *RandomService) SourceEmpty(source model.SourceType) bool {
	return s.checkSourcesEmpty()[source]
}

// CategoryExists 判断指定图源中是否存在指定分类。
//
// category 支持逗号分隔多选（与 Random 一致），只要其中任一分类
// 存在即返回 true。external 图源固定返回 true（分类校验由 API 池完成）。
// 结果来自 30 秒快照缓存，避免错误请求风暴时频繁扫描文件系统。
func (s *RandomService) CategoryExists(source model.SourceType, category string) bool {
	if source == model.SourceExternal {
		return true // external 的分类由 API 池的 categories 白名单校验，不在此判断
	}
	snap := s.categorySnapshot()
	for _, cat := range splitCategories(category) {
		switch source {
		case model.SourceTxt:
			if containsCategory(snap.txtAll, cat) {
				return true
			}
		case model.SourceLocal:
			if containsCategory(snap.localAll, cat) {
				return true
			}
		}
	}
	return false
}

// CategoryExistsFor 判断指定图源在"指定设备目录"下是否存在该分类。
//
// 与 CategoryExists（pc/pe 任一存在即 true）不同：按请求的设备类型精确检查，
// 用于区分"分类完全不存在"与"分类只在另一设备目录存在"——后者对当前设备
// 同样返回"分类不存在"提示页，而不是让仓库报错落入 500。
// category 支持逗号分隔多选，任一分类对当前设备存在即返回 true。
// external 图源固定返回 true（分类校验由 API 池完成）。
// 结果同样来自 30 秒快照缓存。
func (s *RandomService) CategoryExistsFor(source model.SourceType, category string, deviceType model.DeviceType) bool {
	if source == model.SourceExternal {
		return true
	}

	snap := s.categorySnapshot()
	var list []string
	switch source {
	case model.SourceTxt:
		list = snap.txtPC
		if deviceType == model.DevicePE {
			list = snap.txtPE
		}
	case model.SourceLocal:
		list = snap.localPC
		if deviceType == model.DevicePE {
			list = snap.localPE
		}
	}

	for _, cat := range splitCategories(category) {
		if containsCategory(list, cat) {
			return true
		}
	}
	return false
}

// AvailableCategories 返回指定图源当前存在的分类列表（用于提示页展示）。
// external 图源返回 nil（分类由各 API 动态决定，无全局列表）。
// 结果来自 30 秒快照缓存；调用方只读，不得修改返回切片。
func (s *RandomService) AvailableCategories(source model.SourceType) []string {
	switch source {
	case model.SourceTxt:
		return s.categorySnapshot().txtAll
	case model.SourceLocal:
		return s.categorySnapshot().localAll
	default:
		return nil
	}
}

// AvailableAPIs 返回外部 API 池中已配置的 API 名称列表（用于"API 不存在"提示页）。
// 池未配置时返回 nil。
func (s *RandomService) AvailableAPIs() []string {
	if s.externalPool == nil {
		return nil
	}
	apis := s.externalPool.APIs()
	names := make([]string, 0, len(apis))
	for _, a := range apis {
		names = append(names, a.Name)
	}
	return names
}

// ──────────────────────────────────────────────
// 分类清单快照（30 秒缓存）
// ──────────────────────────────────────────────

// categorySnapshot 是 txt/local 图库分类清单的一次扫描快照。
// txt 列表包含"文件含有效 URL"的分类（与仓库解析规则一致），
// local 列表包含"目录下直接含图片文件"的分类。
//
// 快照 30 秒有效：管理员增删分类后，提示页/多分类筛选最迟 30 秒内感知，
// 与 checkSourcesEmpty 的缓存策略一致；正常取图路径不受影响（仓库直接读文件，
// 新增分类仍即时可用）。
type categorySnapshot struct {
	txtPC    []string
	txtPE    []string
	localPC  []string
	localPE  []string
	txtAll   []string // pc+pe 去重合并（pc 在前，顺序稳定）
	localAll []string
}

// categorySnapshotNow 扫描四个设备目录，构建分类清单快照。
func (s *RandomService) categorySnapshotNow() categorySnapshot {
	txtRoot := filepath.Join(s.rootPath, "resources", "txt")
	localRoot := filepath.Join(s.rootPath, "resources", "local")

	snap := categorySnapshot{
		txtPC:   txtCategoriesForDevice(txtRoot, "pc"),
		txtPE:   txtCategoriesForDevice(txtRoot, "pe"),
		localPC: localCategoriesForDevice(localRoot, "pc"),
		localPE: localCategoriesForDevice(localRoot, "pe"),
	}
	snap.txtAll = mergeCategoryLists(snap.txtPC, snap.txtPE)
	snap.localAll = mergeCategoryLists(snap.localPC, snap.localPE)
	return snap
}

// categorySnapshot 返回 30 秒内的分类清单快照（过期自动重建）。
func (s *RandomService) categorySnapshot() categorySnapshot {
	s.mu.RLock()
	if time.Since(s.catAt) < 30*time.Second {
		snap := s.catCache
		s.mu.RUnlock()
		return snap
	}
	s.mu.RUnlock()

	snap := s.categorySnapshotNow()

	s.mu.Lock()
	s.catAt = time.Now()
	s.catCache = snap
	s.mu.Unlock()
	return snap
}

// containsCategory 判断分类是否在列表中（分类数量少，线性扫描足够）。
func containsCategory(list []string, category string) bool {
	for _, c := range list {
		if c == category {
			return true
		}
	}
	return false
}

// mergeCategoryLists 合并 pc/pe 两个设备列表并去重（pc 在前）。
func mergeCategoryLists(pc, pe []string) []string {
	seen := make(map[string]bool, len(pc)+len(pe))
	out := make([]string, 0, len(pc)+len(pe))
	for _, c := range pc {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	for _, c := range pe {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

// checkSourcesEmpty 一次性检测三种图源的空置状态，结果缓存 30 秒。
//
// 检查规则：
//   - txt      — resources/txt/{pc,pe} 下没有任何含有效 URL 的 .txt
//   - local    — resources/local/{pc,pe} 下没有任何图片文件
//   - external — 外部 API 池为空或 nil
//
// 缓存 30 秒：错误请求风暴下避免频繁扫描文件系统；
// 管理员添加内容后最迟 30 秒内提示页自动消失。
func (s *RandomService) checkSourcesEmpty() map[model.SourceType]bool {
	s.mu.RLock()
	if time.Since(s.emptyCheckAt) < 30*time.Second && len(s.sourceEmptyMap) == 3 {
		cached := s.sourceEmptyMap
		s.mu.RUnlock()
		return cached
	}
	s.mu.RUnlock()

	result := map[model.SourceType]bool{
		model.SourceTxt:      !txtDirHasContent(filepath.Join(s.rootPath, "resources", "txt")),
		model.SourceLocal:    !localDirHasImages(filepath.Join(s.rootPath, "resources", "local")),
		model.SourceExternal: s.externalPool == nil || len(s.externalPool.APIs()) == 0,
	}

	s.mu.Lock()
	s.emptyCheckAt = time.Now()
	s.sourceEmptyMap = result
	s.mu.Unlock()
	return result
}

// ResolveDefaultCategory 将"未指定分类"解析为渠道的默认分类。
//
//   - external：不处理，返回原值（各 API 的 default_category 由 ExternalPool 解析）；
//   - txt/local：raw 为空或 "default" 时，返回 TXT_DEFAULT_CATEGORY /
//     LOCAL_DEFAULT_CATEGORY 的配置值，未配置则回退内置 "default"；
//     显式指定其他分类（含多选）时原样返回。
//
// Handler 渲染"分类不存在"提示页时也使用本方法，保证提示页显示的分类
// 与实际使用的分类一致。
func (s *RandomService) ResolveDefaultCategory(source model.SourceType, raw string) string {
	if source == model.SourceExternal || (raw != "" && raw != "default") {
		return raw
	}

	configured := "default"
	if config.C != nil {
		switch source {
		case model.SourceTxt:
			configured = config.C.TxtDefaultCategory
		case model.SourceLocal:
			configured = config.C.LocalDefaultCategory
		}
	}
	if v := strings.TrimSpace(configured); v != "" {
		return v
	}
	return "default"
}

// pickExistingCategory 从逗号分隔的类别中随机选一个"实际可用"的分类。
//
// 多分类场景下先过滤掉当前设备目录下不存在的分类（外部渠道按 API 池分类
// 白名单过滤）再随机，避免选到不可用的分类导致仓库报错、Handler 误判为 500。
// 单分类场景不做存在性校验（保持零开销），由仓库/Handler 处理结果。
//
// 全部不可用时返回 ""，调用方据此返回"分类不存在"错误。
func (s *RandomService) pickExistingCategory(source model.SourceType, raw string, deviceType model.DeviceType) string {
	categories := splitCategories(raw)
	if len(categories) == 1 {
		return categories[0] // 单分类直接返回，存在性交给后续处理
	}

	// 多分类：过滤出实际可用的分类
	var existing []string
	for _, c := range categories {
		if source == model.SourceExternal {
			// 外部渠道：按 API 池的分类白名单过滤
			if s.externalPool != nil && s.externalPool.SupportsCategory(c) {
				existing = append(existing, c)
			}
			continue
		}
		if s.CategoryExistsFor(source, c, deviceType) {
			existing = append(existing, c)
		}
	}
	if len(existing) == 0 {
		return "" // 全部不可用，通知 Handler 显示"分类不存在"
	}
	return existing[rand.Intn(len(existing))]
}

// splitCategories 将逗号分隔的类别字符串解析为去重后的分类切片。
// "" → ["default"]；"a, b, a" → ["a", "b"]。
func splitCategories(raw string) []string {
	if raw == "" {
		return []string{"default"}
	}
	seen := make(map[string]bool)
	var categories []string
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		categories = append(categories, p)
	}
	if len(categories) == 0 {
		return []string{"default"}
	}
	return categories
}
