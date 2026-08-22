package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"img-api/internal/logger"
)

// RedisCache 封装 go-redis 客户端，实现 Cache 接口。
//
// 连接参数在启动时通过 .env 注入，连接失败不会 panic，
// 而是返回 error 由 main.go 回退到 MemoryCache。
type RedisCache struct {
	client *redis.Client // go-redis 客户端（内部管理连接池）
}

// NewRedisCache 创建 Redis 缓存实例并验证连接。
//
// 启动时执行一次 PING 检查连通性。
// 连接失败应回退到 NewMemoryCache()，保障服务正常启动。
func NewRedisCache(addr, password string, db int) (*RedisCache, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		DialTimeout:  3 * time.Second,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
	})

	// 启动时做一次连接检查
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis connect %s: %w", addr, err)
	}

	logger.L.Info("redis connected", "addr", addr, "db", db)
	return &RedisCache{client: client}, nil
}

// Name 返回缓存名称
func (r *RedisCache) Name() string { return "redis" }

// Get 从 Redis 获取值
func (r *RedisCache) Get(ctx context.Context, key string) ([]byte, error) {
	val, err := r.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, Nil
	}
	if err != nil {
		return nil, fmt.Errorf("redis get %s: %w", key, err)
	}
	return val, nil
}

// Set 写入 Redis
func (r *RedisCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return r.client.Set(ctx, key, value, ttl).Err()
}

// Delete 删除 Redis 键
func (r *RedisCache) Delete(ctx context.Context, key string) error {
	return r.client.Del(ctx, key).Err()
}

// SAdd 向 Redis Set 批量添加成员。
func (r *RedisCache) SAdd(ctx context.Context, key string, members ...string) error {
	if len(members) == 0 {
		return nil
	}
	anys := make([]any, len(members))
	for i, m := range members {
		anys[i] = m
	}
	return r.client.SAdd(ctx, key, anys...).Err()
}

// SRandMember 从 Redis Set 中 O(1) 随机获取一个成员。
func (r *RedisCache) SRandMember(ctx context.Context, key string) (string, error) {
	val, err := r.client.SRandMember(ctx, key).Result()
	if err == redis.Nil {
		return "", Nil
	}
	return val, err
}

// SCard 返回 Redis Set 的成员总数。
func (r *RedisCache) SCard(ctx context.Context, key string) (int64, error) {
	return r.client.SCard(ctx, key).Result()
}

// Close 关闭 Redis 连接
func (r *RedisCache) Close() error {
	return r.client.Close()
}
