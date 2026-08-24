package cache

import (
	"context"
	"math/rand"
	"path"
	"sync"
	"sync/atomic"
	"time"

	"img-api/internal/logger"
)

// memEntry 是内存缓存中单个条目的结构。
// expire 为零值（IsZero()==true）表示永不过期。
type memEntry struct {
	data   []byte    // 缓存的原始字节（JSON 序列化后的 Image）
	expire time.Time // 过期时间（零值=永不过期）
}

// MemoryCache 基于 sync.Map 的进程内缓存。
//
// 作为 Redis 连接失败时的自动降级方案，提供与 Redis 相同的 Cache 接口。
// Set 类型操作（SAdd/SRandMember/SCard）使用独立的 RWMutex + map 实现，
// 与 sync.Map 条目互不干扰，保证并发安全。
//
// 容量限制：最多 maxItems 个缓存条目，超出时随机淘汰旧条目。
//
// 限制：
//   - 进程重启后所有数据丢失
//   - 多实例部署时缓存不共享
//   - 生产环境建议配 Redis
type MemoryCache struct {
	items    sync.Map                       // key(string) → *memEntry
	sets     map[string]map[string]struct{} // Set 集合存储
	setMu    sync.RWMutex                   // 保护 sets
	maxItems int                            // 最大缓存条目数（0=不限制）
	count    atomic.Int64                   // 条目数近似值（淘汰判断用，允许微小漂移）
}

const defaultMaxItems = 10000 // 默认最多缓存 10000 个条目

// NewMemoryCache 创建内存缓存并启动后台过期清理协程。
//
// 每 5 分钟扫描一次全量条目，删除已过期的。
// 调用方无需手动停止——清理协程随进程退出自动终止。
func NewMemoryCache() *MemoryCache {
	mc := &MemoryCache{
		sets:     make(map[string]map[string]struct{}),
		maxItems: defaultMaxItems,
	}

	// 后台定期清理过期条目（每 5 分钟）
	go mc.cleanupLoop(5 * time.Minute)

	logger.L.Info("memory cache initialized (fallback mode)", "max_items", mc.maxItems)
	return mc
}

// Name 返回缓存名称
func (m *MemoryCache) Name() string { return "memory" }

// Get 从内存获取值
func (m *MemoryCache) Get(ctx context.Context, key string) ([]byte, error) {
	val, ok := m.items.Load(key)
	if !ok {
		return nil, Nil
	}

	entry := val.(*memEntry)

	// 检查是否过期
	if !entry.expire.IsZero() && time.Now().After(entry.expire) {
		m.items.Delete(key)
		m.count.Add(-1)
		return nil, Nil
	}

	// 返回拷贝：调用方持有内部切片引用时可能修改污染缓存
	data := make([]byte, len(entry.data))
	copy(data, entry.data)
	return data, nil
}

// Set 写入内存（拷贝 value，防止调用方后续修改污染缓存），
// 超过容量上限时随机淘汰旧条目。
func (m *MemoryCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	// 防御性拷贝：调用方持有的切片可能被复用/修改
	data := make([]byte, len(value))
	copy(data, value)

	entry := &memEntry{data: data}
	if ttl > 0 {
		entry.expire = time.Now().Add(ttl)
	}

	if _, loaded := m.items.Load(key); !loaded {
		// 容量控制：新增 key 且已达上限 → 随机淘汰一个（O(1) 计数判断）
		if m.maxItems > 0 {
			m.evictIfNeeded()
		}
		m.items.Store(key, entry)
		m.count.Add(1)
		return nil
	}

	m.items.Store(key, entry)
	return nil
}

// evictIfNeeded 如果条目数超限，随机淘汰一个（近似，非严格均匀）。
func (m *MemoryCache) evictIfNeeded() {
	if m.count.Load() < int64(m.maxItems) {
		return
	}

	// 随机选一个 key 淘汰
	target := rand.Intn(m.maxItems) // 索引范围 [0, maxItems)，与遍历序号对齐
	i := 0
	m.items.Range(func(key, _ any) bool {
		if i == target {
			m.items.Delete(key)
			m.count.Add(-1)
			return false
		}
		i++
		return true
	})
}

// Delete 删除内存键
func (m *MemoryCache) Delete(ctx context.Context, key string) error {
	if _, ok := m.items.LoadAndDelete(key); ok {
		m.count.Add(-1)
	}
	return nil
}

// cleanupLoop 定期清理过期条目
func (m *MemoryCache) cleanupLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		m.items.Range(func(key, value any) bool {
			entry := value.(*memEntry)
			if !entry.expire.IsZero() && now.After(entry.expire) {
				m.items.Delete(key)
				m.count.Add(-1)
			}
			return true
		})
	}
}

// SAdd 向内存 Set 批量添加成员（并发安全，支持去重）。
func (m *MemoryCache) SAdd(ctx context.Context, key string, members ...string) error {
	if len(members) == 0 {
		return nil
	}

	m.setMu.Lock()
	defer m.setMu.Unlock()

	s, ok := m.sets[key]
	if !ok {
		s = make(map[string]struct{})
		m.sets[key] = s
	}
	for _, member := range members {
		s[member] = struct{}{}
	}
	return nil
}

// SRandMember 从内存 Set 中随机获取一个成员（并发安全）。
func (m *MemoryCache) SRandMember(ctx context.Context, key string) (string, error) {
	m.setMu.RLock()
	s, ok := m.sets[key]
	if !ok || len(s) == 0 {
		m.setMu.RUnlock()
		return "", Nil
	}

	// 收集所有成员后随机选（O(n)，但 Set 通常很小）
	members := make([]string, 0, len(s))
	for member := range s {
		members = append(members, member)
	}
	m.setMu.RUnlock()

	return members[rand.Intn(len(members))], nil
}

// SCard 返回内存 Set 的成员数量（并发安全）。
func (m *MemoryCache) SCard(ctx context.Context, key string) (int64, error) {
	m.setMu.RLock()
	defer m.setMu.RUnlock()

	s, ok := m.sets[key]
	if !ok {
		return 0, nil
	}
	return int64(len(s)), nil
}

// ScanKeys 返回内存 Set 中匹配 pattern（Redis glob）的全部 key。
func (m *MemoryCache) ScanKeys(ctx context.Context, pattern string) ([]string, error) {
	m.setMu.RLock()
	defer m.setMu.RUnlock()

	var keys []string
	for k := range m.sets {
		if ok, err := path.Match(pattern, k); err == nil && ok {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

// Size 返回当前缓存条目数（用于监控）
func (m *MemoryCache) Size() int {
	return int(m.count.Load())
}
