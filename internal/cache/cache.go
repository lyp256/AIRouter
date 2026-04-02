// Package cache 提供统一缓存接口，支持 Redis 和内存缓存
package cache

import (
	"context"
	"errors"
	"time"

	"github.com/lyp256/airouter/internal/config"
)

// ErrCacheMiss 缓存未命中
var ErrCacheMiss = errors.New("cache: key not found")

// Cache 统一缓存接口
type Cache interface {
	// Get 获取缓存值，未命中返回 ErrCacheMiss
	Get(ctx context.Context, key string, value interface{}) error
	// Set 设置缓存值
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error
	// Delete 删除缓存
	Delete(ctx context.Context, key string) error
	// Once 单飞：缓存未命中时执行 do 函数并缓存结果
	Once(ctx context.Context, key string, value interface{}, ttl time.Duration, do func() (interface{}, error)) error
}

// New 根据配置创建缓存实例
func New(cfg *config.CacheConfig) (Cache, error) {
	if !cfg.Enabled {
		return newNopCache(), nil
	}
	switch cfg.Type {
	case "redis":
		return newRedisCache(cfg)
	default:
		return newMemoryCache(cfg)
	}
}

// nopCache 空缓存实现（缓存禁用时使用）
type nopCache struct{}

func newNopCache() Cache {
	return &nopCache{}
}

func (n *nopCache) Get(_ context.Context, _ string, _ interface{}) error {
	return ErrCacheMiss
}

func (n *nopCache) Set(_ context.Context, _ string, _ interface{}, _ time.Duration) error {
	return nil
}

func (n *nopCache) Delete(_ context.Context, _ string) error {
	return nil
}

func (n *nopCache) Once(_ context.Context, _ string, value interface{}, _ time.Duration, do func() (interface{}, error)) error {
	v, err := do()
	if err != nil {
		return err
	}
	// 将结果赋值给 value（value 必须是指针）
	// nopCache 场景下直接通过反射赋值
	return assignValue(value, v)
}
