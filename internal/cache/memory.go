package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/lyp256/airouter/internal/config"
	"github.com/qianbin/directcache"
	"golang.org/x/sync/singleflight"
)

// cacheEntry 带过期时间的缓存条目
type cacheEntry struct {
	Data     []byte    `json:"d"`
	ExpireAt time.Time `json:"e"`
}

// memoryCache 基于 directcache 的内存缓存实现（带 TTL 支持）
type memoryCache struct {
	client *directcache.Cache
	ttl    time.Duration
	mu     sync.Mutex
	sf     singleflight.Group
}

func newMemoryCache(cfg *config.CacheConfig) (*memoryCache, error) {
	size := cfg.Size
	if size <= 0 {
		size = 64
	}

	ttl := cfg.TTL
	if ttl == 0 {
		ttl = 10 * time.Minute
	}

	client := directcache.New(size * 1024 * 1024)

	return &memoryCache{
		client: client,
		ttl:    ttl,
	}, nil
}

func (m *memoryCache) SetNX(_ context.Context, key string, value interface{}, ttl time.Duration) (bool, error) {
	if ttl == 0 {
		ttl = m.ttl
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// 检查 key 是否已存在且未过期
	data, ok := m.client.Get([]byte(key))
	if ok {
		var entry cacheEntry
		if err := json.Unmarshal(data, &entry); err == nil {
			if time.Now().Before(entry.ExpireAt) {
				return false, nil // key 已存在，设置失败
			}
		}
		m.client.Del([]byte(key))
	}

	// key 不存在或已过期，设置值
	valData, err := json.Marshal(value)
	if err != nil {
		return false, fmt.Errorf("缓存序列化失败: %w", err)
	}

	entry := cacheEntry{
		Data:     valData,
		ExpireAt: time.Now().Add(ttl),
	}
	entryData, err := json.Marshal(entry)
	if err != nil {
		return false, fmt.Errorf("缓存序列化失败: %w", err)
	}

	m.client.Set([]byte(key), entryData)
	return true, nil
}

func (m *memoryCache) IsDistributed() bool {
	return false
}

func (m *memoryCache) Get(_ context.Context, key string, value interface{}) error {
	data, ok := m.client.Get([]byte(key))
	if !ok {
		return ErrCacheMiss
	}

	var entry cacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return ErrCacheMiss
	}

	// 检查过期
	if time.Now().After(entry.ExpireAt) {
		m.client.Del([]byte(key))
		return ErrCacheMiss
	}

	return json.Unmarshal(entry.Data, value)
}

func (m *memoryCache) Set(_ context.Context, key string, value interface{}, ttl time.Duration) error {
	if ttl == 0 {
		ttl = m.ttl
	}

	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("缓存序列化失败: %w", err)
	}

	entry := cacheEntry{
		Data:     data,
		ExpireAt: time.Now().Add(ttl),
	}

	entryData, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("缓存序列化失败: %w", err)
	}

	m.client.Set([]byte(key), entryData)
	return nil
}

func (m *memoryCache) Delete(_ context.Context, key string) error {
	m.client.Del([]byte(key))
	return nil
}

func (m *memoryCache) Once(_ context.Context, key string, value interface{}, ttl time.Duration, do func() (interface{}, error)) error {
	if ttl == 0 {
		ttl = m.ttl
	}

	// 先尝试从缓存获取
	data, ok := m.client.Get([]byte(key))
	if ok {
		var entry cacheEntry
		if err := json.Unmarshal(data, &entry); err == nil {
			// 检查过期
			if time.Now().Before(entry.ExpireAt) {
				if err := json.Unmarshal(entry.Data, value); err == nil {
					return nil
				}
			}
		}
		// 缓存数据损坏或已过期，删除
		m.client.Del([]byte(key))
	}

	// 使用 singleflight 防止缓存击穿（同一个 key 只允许一个请求加载）
	res, err, _ := m.sf.Do(key, func() (interface{}, error) {
		// 双重检查：获取 sf 锁后再查一次缓存（可能其他请求刚填补了缓存）
		data, ok = m.client.Get([]byte(key))
		if ok {
			var entry cacheEntry
			if err := json.Unmarshal(data, &entry); err == nil {
				if time.Now().Before(entry.ExpireAt) {
					return entry.Data, nil
				}
			}
			m.client.Del([]byte(key))
		}

		// 缓存未命中，执行加载函数
		result, err := do()
		if err != nil {
			return nil, err
		}

		// 序列化结果
		resultData, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("缓存序列化失败: %w", err)
		}

		// 存入缓存
		entry := cacheEntry{
			Data:     resultData,
			ExpireAt: time.Now().Add(ttl),
		}
		entryData, err := json.Marshal(entry)
		if err != nil {
			return nil, fmt.Errorf("缓存序列化失败: %w", err)
		}
		m.client.Set([]byte(key), entryData)

		return resultData, nil
	})

	if err != nil {
		return err
	}

	// 将结果反序列化到 value
	return json.Unmarshal(res.([]byte), value)
}

// assignValue 将 src 的值赋给 dst（dst 必须是指针）
func assignValue(dst, src interface{}) error {
	if src == nil {
		return errors.New("缓存值为 nil")
	}

	dstVal := reflect.ValueOf(dst)
	if dstVal.Kind() != reflect.Ptr {
		return errors.New("目标值必须是指针")
	}

	srcVal := reflect.ValueOf(src)

	// 如果 src 是指针，获取其指向的值
	if srcVal.Kind() == reflect.Ptr {
		srcVal = srcVal.Elem()
	}

	// 如果 dst 指向指针，需要处理
	if dstVal.Elem().Kind() == reflect.Ptr {
		newVal := reflect.New(srcVal.Type())
		newVal.Elem().Set(srcVal)
		dstVal.Elem().Set(newVal)
		return nil
	}

	dstVal.Elem().Set(srcVal)
	return nil
}
