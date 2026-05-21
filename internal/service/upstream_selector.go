package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lyp256/airouter/internal/cache"
	"github.com/lyp256/airouter/internal/model"
	"gorm.io/gorm"
)

var (
	// ErrNoAvailableUpstream 没有可用的上游模型
	ErrNoAvailableUpstream = errors.New("没有可用的上游模型")
	// ErrUpstreamQuotaExceeded 配额超限
	ErrUpstreamQuotaExceeded = errors.New("上游模型配额已用尽")
)

// UpstreamSelection 上游模型选择结果
type UpstreamSelection struct {
	Upstream    *model.Upstream
	Provider    *model.Provider
	ProviderKey *model.ProviderKey
	RawKey      string
}

// UpstreamSelector 上游模型选择器
type UpstreamSelector struct {
	db       *gorm.DB
	cache    cache.Cache
	cacheTTL time.Duration
	counters sync.Map
}

// NewUpstreamSelector 创建上游模型选择器
func NewUpstreamSelector(db *gorm.DB, c cache.Cache) *UpstreamSelector {
	ttl := 10 * time.Minute
	return &UpstreamSelector{
		db:       db,
		cache:    c,
		cacheTTL: ttl,
	}
}

// SelectUpstream 选择一个上游模型
func (s *UpstreamSelector) SelectUpstream(modelID string, excludeIDs ...string) (*UpstreamSelection, error) {
	// 获取缓存的上游列表
	upstreams := s.getUpstreams(modelID)
	if len(upstreams) == 0 {
		return nil, ErrNoAvailableUpstream
	}
	// 按优先级和权重选择
	upstream := s.selectByWeight(upstreams, excludeIDs...)
	if upstream == nil {
		return nil, ErrNoAvailableUpstream
	}

	ctx := context.Background()

	// 获取关联的 Provider（带缓存）
	var provider model.Provider
	if err := s.cache.Once(ctx, fmt.Sprintf("provider:%s", upstream.ProviderID), &provider, s.cacheTTL, func() (interface{}, error) {
		var p model.Provider
		if err := s.db.First(&p, "id = ?", upstream.ProviderID).Error; err != nil {
			return nil, err
		}
		return p, nil
	}); err != nil {
		return nil, err
	}

	// ProviderKey.Key has json:"-", so caching the model drops the secret when
	// it is unmarshaled back. Load it directly to keep upstream auth intact.
	var apiKey model.ProviderKey
	if err := s.db.First(&apiKey, "id = ?", upstream.ProviderKeyID).Error; err != nil {
		return nil, err
	}

	return &UpstreamSelection{
		Upstream:    upstream,
		Provider:    &provider,
		ProviderKey: &apiKey,
		RawKey:      apiKey.Key,
	}, nil
}

// getUpstreams 获取上游列表（带缓存）
func (s *UpstreamSelector) getUpstreams(modelID string) []*model.Upstream {
	ctx := context.Background()
	var upstreams []*model.Upstream
	if err := s.cache.Once(ctx, fmt.Sprintf("upstreams:model:%s", modelID), &upstreams, s.cacheTTL, func() (interface{}, error) {
		var list []*model.Upstream
		if err := s.db.Where("model_id = ?", modelID).Find(&list).Error; err != nil {
			return nil, err
		}
		return list, nil
	}); err != nil {
		return nil
	}
	return upstreams
}

// selectByWeight 根据权重选择上游模型
func (s *UpstreamSelector) selectByWeight(upstreams []*model.Upstream, excludeIDs ...string) *model.Upstream {
	// 转换为 map 方便查找
	excluded := make(map[string]bool)
	for _, id := range excludeIDs {
		excluded[id] = true
	}

	// 过滤出启用且不在排除列表中的上游模型
	activeUpstreams := make([]*model.Upstream, 0, len(upstreams))
	for _, u := range upstreams {
		if !u.Enabled || excluded[u.ID] {
			continue
		}
		activeUpstreams = append(activeUpstreams, u)
	}

	if len(activeUpstreams) == 0 {
		return nil
	}

	// 按权重计算总和
	totalWeight := 0
	for _, u := range activeUpstreams {
		totalWeight += u.Weight
	}

	if totalWeight == 0 {
		return activeUpstreams[0]
	}

	// 按 model_id 区分计数器
	modelID := activeUpstreams[0].ModelID
	counterVal, _ := s.counters.LoadOrStore(modelID, new(uint64))
	counter := counterVal.(*uint64)

	// 原子递增并计算当前权重偏移量
	count := atomic.AddUint64(counter, 1) - 1
	n := int(count % uint64(totalWeight))

	for _, u := range activeUpstreams {
		if n < u.Weight {
			return u
		}
		n -= u.Weight
	}

	return activeUpstreams[0]
}

// GetUpstreamSelection 根据 upstreamID 获取完整的选择信息（用于测试）
func (s *UpstreamSelector) GetUpstreamSelection(upstreamID string) (*UpstreamSelection, error) {
	var upstream model.Upstream
	if err := s.db.First(&upstream, "id = ?", upstreamID).Error; err != nil {
		return nil, fmt.Errorf("上游模型不存在: %w", err)
	}

	var provider model.Provider
	if err := s.db.First(&provider, "id = ?", upstream.ProviderID).Error; err != nil {
		return nil, fmt.Errorf("供应商不存在: %w", err)
	}

	var apiKey model.ProviderKey
	if err := s.db.First(&apiKey, "id = ?", upstream.ProviderKeyID).Error; err != nil {
		return nil, fmt.Errorf("供应商密钥不存在: %w", err)
	}

	return &UpstreamSelection{
		Upstream:    &upstream,
		Provider:    &provider,
		ProviderKey: &apiKey,
		RawKey:      apiKey.Key,
	}, nil
}

// InvalidateCache 使缓存失效
func (s *UpstreamSelector) InvalidateCache(modelID string) {
	_ = s.cache.Delete(context.Background(), fmt.Sprintf("upstreams:model:%s", modelID))
}

// GetUpstreamsByModel 获取模型的所有上游模型
func (s *UpstreamSelector) GetUpstreamsByModel(modelID string) ([]*model.Upstream, error) {
	var upstreams []*model.Upstream
	if err := s.db.Where("model_id = ?", modelID).Find(&upstreams).Error; err != nil {
		return nil, err
	}
	return upstreams, nil
}
