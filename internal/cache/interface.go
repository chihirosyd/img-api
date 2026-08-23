// Package cache 定义缓存抽象接口，提供 Redis 和内存两种实现。
//
// 设计原则：
//   - 接口驱动 — Service 依赖 Cache 接口，不关心底层是 Redis 还是内存
//   - 自动降级 — Redis 连接失败时自动切换为 MemoryCache
//   - 零侵入  — 缓存不可用时业务静默绕过（fail-open 策略）
//   - 可替换  — 实现 Cache 接口即可接入新缓存后端（如 Memcached）
package cache

import (
	"context"
	"time"
)

// Cache 是缓存操作的抽象接口。
//
// 所有缓存实现都必须满足此契约。调用方通过 errors.Is(err, cache.Nil)
// 区分"键不存在"和"真正的 I/O 错误"。
type Cache interface {
	// Get 从缓存获取 key 对应的值。
	// 返回值约定：([]byte, nil)=命中, (nil, cache.Nil)=未命中, (nil, error)=I/O错误
	Get(ctx context.Context, key string) ([]byte, error)

	// Set 写入 key-value，ttl 为过期时间。ttl=0 表示永不过期。
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error

	// Delete 删除指定 key，key 不存在不报错。
	Delete(ctx context.Context, key string) error

	// SAdd 向 Set 集合批量添加成员。
	SAdd(ctx context.Context, key string, members ...string) error

	// SRandMember 从 Set 集合中随机获取一个成员。
	// Redis 实现 O(1)，内存实现 O(n)。
	// key 不存在或集合为空时返回 ("", cache.Nil)。
	SRandMember(ctx context.Context, key string) (string, error)

	// SCard 返回 Set 集合的成员数量。
	SCard(ctx context.Context, key string) (int64, error)

	// ScanKeys 返回匹配 pattern（Redis glob，如 "img:pc:*"）的全部 key。
	// 供 sync-redis 清理已删除分类的孤儿 Set 使用。
	ScanKeys(ctx context.Context, pattern string) ([]string, error)

	// Name 返回缓存实例标识（如 "redis" / "memory"），用于日志和监控。
	Name() string
}

// Nil 是缓存未命中的哨兵错误。
// 使用 errors.Is(err, Nil) 判断，而非 == 比较。
var Nil = &CacheNilError{}

// CacheNilError 表示缓存键不存在的特定错误类型。
// 实现了 errors.Is 接口，支持 errors.Is(err, &CacheNilError{}) 匹配。
type CacheNilError struct{}

func (e *CacheNilError) Error() string { return "cache: key not found" }

// Is 实现 errors.Is 的接口，使得 errors.Is(someErr, cache.Nil) 能正确判断。
func (e *CacheNilError) Is(target error) bool {
	_, ok := target.(*CacheNilError)
	return ok
}
