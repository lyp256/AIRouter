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
	// SetNX 仅当 key 不存在时设置值，返回是否设置成功（用于分布式选主）
	SetNX(ctx context.Context, key string, value interface{}, ttl time.Duration) (bool, error)
	// IsDistributed 返回是否为分布式缓存（Redis=true，内存=false）
	IsDistributed() bool
}

// Namespace 带有前缀的缓存命名空间，提供泛型支持
type Namespace[T any] struct {
	cache Cache
	name  string
	ttl   time.Duration
}

// NewNamespace 创建一个新的命名空间
func NewNamespace[T any](cache Cache, name string, ttl time.Duration) *Namespace[T] {
	return &Namespace[T]{
		cache: cache,
		name:  name,
		ttl:   ttl,
	}
}

func (n *Namespace[T]) key(key string) string {
	return n.name + ":" + key
}

// Get 从命名空间获取值
func (n *Namespace[T]) Get(ctx context.Context, key string) (T, error) {
	var val T
	err := n.cache.Get(ctx, n.key(key), &val)
	return val, err
}

// Set 设置值到命名空间
func (n *Namespace[T]) Set(ctx context.Context, key string, val T, ttl time.Duration) error {
	if ttl == 0 {
		ttl = n.ttl
	}
	return n.cache.Set(ctx, n.key(key), val, ttl)
}

// Delete 从命名空间删除
func (n *Namespace[T]) Delete(ctx context.Context, key string) error {
	return n.cache.Delete(ctx, n.key(key))
}

// Once 命名空间版本的 Once
func (n *Namespace[T]) Once(ctx context.Context, key string, ttl time.Duration, do func() (T, error)) (T, error) {
	if ttl == 0 {
		ttl = n.ttl
	}
	var val T
	err := n.cache.Once(ctx, n.key(key), &val, ttl, func() (interface{}, error) {
		return do()
	})
	return val, err
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

func (n *nopCache) SetNX(_ context.Context, _ string, _ interface{}, _ time.Duration) (bool, error) {
	return true, nil
}

func (n *nopCache) IsDistributed() bool {
	return false
}
