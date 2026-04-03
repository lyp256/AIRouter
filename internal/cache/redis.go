package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	gorediscache "github.com/go-redis/cache/v9"
	"github.com/lyp256/airouter/internal/config"
	"github.com/qianbin/directcache"
	"github.com/redis/go-redis/v9"
)

// redisCache 基于 go-redis/cache/v9 的 Redis 缓存实现
type redisCache struct {
	client *gorediscache.Cache
	rdb    *redis.Client
	ttl    time.Duration
}

type l1Cache struct {
	*directcache.Cache
}

func (c *l1Cache) Set(key string, data []byte) {
	c.Cache.Set([]byte(key), data)
}

func (c *l1Cache) Get(key string) ([]byte, bool) {
	return c.Cache.Get([]byte(key))
}

func (c *l1Cache) Del(key string) {
	c.Cache.Del([]byte(key))
}

func newRedisCache(cfg *config.CacheConfig) (*redisCache, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("连接 Redis 失败: %w", err)
	}

	ttl := cfg.TTL
	if ttl == 0 {
		ttl = 10 * time.Minute
	}

	// 启用本地二级缓存 (L1)
	size := cfg.Size
	if size <= 0 {
		size = 16 // 默认 L1 16MB
	}
	localCache := &l1Cache{directcache.New(size * 1024 * 1024)}

	client := gorediscache.New(&gorediscache.Options{
		Redis:      rdb,
		LocalCache: localCache,
	})

	return &redisCache{
		client: client,
		rdb:    rdb,
		ttl:    ttl,
	}, nil
}

func (r *redisCache) Get(ctx context.Context, key string, value interface{}) error {
	err := r.client.Get(ctx, key, value)
	if err != nil {
		if err == gorediscache.ErrCacheMiss {
			return ErrCacheMiss
		}
		return err
	}
	return nil
}

func (r *redisCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	if ttl == 0 {
		ttl = r.ttl
	}
	return r.client.Set(&gorediscache.Item{
		Ctx:   ctx,
		Key:   key,
		Value: value,
		TTL:   ttl,
	})
}

func (r *redisCache) Delete(ctx context.Context, key string) error {
	return r.client.Delete(ctx, key)
}

func (r *redisCache) Once(ctx context.Context, key string, value interface{}, ttl time.Duration, do func() (interface{}, error)) error {
	if ttl == 0 {
		ttl = r.ttl
	}
	return r.client.Once(&gorediscache.Item{
		Ctx:   ctx,
		Key:   key,
		Value: value,
		TTL:   ttl,
		Do: func(*gorediscache.Item) (interface{}, error) {
			return do()
		},
	})
}

func (r *redisCache) SetNX(ctx context.Context, key string, value interface{}, ttl time.Duration) (bool, error) {
	if ttl == 0 {
		ttl = r.ttl
	}
	data, err := json.Marshal(value)
	if err != nil {
		return false, fmt.Errorf("缓存序列化失败: %w", err)
	}
	result, err := r.rdb.SetArgs(ctx, key, data, redis.SetArgs{
		Mode: "NX",
		TTL:  ttl,
	}).Result()
	if err != nil {
		if err == redis.Nil {
			return false, nil // key 已存在，设置失败
		}
		return false, err
	}
	return result == "OK", nil
}

func (r *redisCache) IsDistributed() bool {
	return true
}
