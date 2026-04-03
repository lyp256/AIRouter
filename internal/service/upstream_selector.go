package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lyp256/airouter/internal/cache"
	"github.com/lyp256/airouter/internal/crypto"
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
	Upstream     *model.Upstream
	Provider     *model.Provider
	ProviderKey  *model.ProviderKey
	DecryptedKey string
}

// UpstreamSelector 上游模型选择器
type UpstreamSelector struct {
	db        *gorm.DB
	encryptor *crypto.Encryptor
	cache     cache.Cache
	cacheTTL  time.Duration
	counters  sync.Map
}

// NewUpstreamSelector 创建上游模型选择器
func NewUpstreamSelector(db *gorm.DB, encryptor *crypto.Encryptor, c cache.Cache) *UpstreamSelector {
	ttl := 10 * time.Minute
	return &UpstreamSelector{
		db:        db,
		encryptor: encryptor,
		cache:     c,
		cacheTTL:  ttl,
	}
}

// SelectUpstream 选择一个上游模型
func (s *UpstreamSelector) SelectUpstream(modelID string) (*UpstreamSelection, error) {
	// 获取缓存的上游列表
	upstreams := s.getUpstreams(modelID)
	if len(upstreams) == 0 {
		return nil, ErrNoAvailableUpstream
	}
	// 按优先级和权重选择
	upstream := s.selectByWeight(upstreams)
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

	// 获取关联的 ProviderKey（带缓存）
	var apiKey model.ProviderKey
	if err := s.cache.Once(ctx, fmt.Sprintf("provider_key:%s", upstream.ProviderKeyID), &apiKey, s.cacheTTL, func() (interface{}, error) {
		var k model.ProviderKey
		if err := s.db.First(&k, "id = ?", upstream.ProviderKeyID).Error; err != nil {
			return nil, err
		}
		return k, nil
	}); err != nil {
		return nil, err
	}

	// 解密密钥
	decryptedKey, err := s.encryptor.Decrypt(apiKey.Key)
	if err != nil {
		return nil, err
	}

	return &UpstreamSelection{
		Upstream:     upstream,
		Provider:     &provider,
		ProviderKey:  &apiKey,
		DecryptedKey: decryptedKey,
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
func (s *UpstreamSelector) selectByWeight(upstreams []*model.Upstream) *model.Upstream {
	// 过滤出活跃状态的上游模型（从缓存检查健康状态）
	activeUpstreams := make([]*model.Upstream, 0, len(upstreams))
	for _, u := range upstreams {
		if !u.Enabled {
			continue
		}
		// 从缓存检查健康状态（缓存未命中 = 健康）
		if s.isUpstreamHealthy(u.ID) {
			activeUpstreams = append(activeUpstreams, u)
		}
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

// isUpstreamHealthy 从缓存检查上游是否健康
func (s *UpstreamSelector) isUpstreamHealthy(upstreamID string) bool {
	health := GetUpstreamHealthFromCache(s.cache, upstreamID)
	if health == nil {
		return true // 缓存未命中，默认健康
	}
	return health.Status == "active"
}

// MarkUpstreamError 标记上游模型错误（写入缓存）
func (s *UpstreamSelector) MarkUpstreamError(upstreamID string) error {
	// 读取已有健康状态以保留连续计数
	health := GetUpstreamHealthFromCache(s.cache, upstreamID)
	if health == nil {
		health = &UpstreamHealthStatus{
			UpstreamID: upstreamID,
			Status:     "active",
		}
	}

	health.UpstreamID = upstreamID
	health.ConsecFail++
	health.ConsecSuccess = 0
	health.Status = "error"
	health.LastErrorTime = time.Now()

	return SetUpstreamHealthToCache(s.cache, upstreamID, health)
}

// MarkUpstreamSuccess 标记上游模型成功（写入缓存）
func (s *UpstreamSelector) MarkUpstreamSuccess(upstreamID string) error {
	health := GetUpstreamHealthFromCache(s.cache, upstreamID)
	if health == nil {
		// 无健康记录说明一直健康，无需操作
		return nil
	}

	// 有记录（当前为 error 状态），成功请求直接恢复
	health.ConsecSuccess++
	health.ConsecFail = 0
	health.Status = "active"
	health.LastCheckTime = time.Now()

	return SetUpstreamHealthToCache(s.cache, upstreamID, health)
}

// UpdateQuotaUsed 更新已使用配额
func (s *UpstreamSelector) UpdateQuotaUsed(apiKeyID string, delta int64) error {
	return s.db.Model(&model.ProviderKey{}).
		Where("id = ?", apiKeyID).
		UpdateColumn("quota_used", gorm.Expr("quota_used + ?", delta)).Error
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

	decryptedKey, err := s.encryptor.Decrypt(apiKey.Key)
	if err != nil {
		return nil, fmt.Errorf("解密密钥失败: %w", err)
	}

	return &UpstreamSelection{
		Upstream:     &upstream,
		Provider:     &provider,
		ProviderKey:  &apiKey,
		DecryptedKey: decryptedKey,
	}, nil
}

// InvalidateCache 使缓存失效
func (s *UpstreamSelector) InvalidateCache(modelID string) {
	_ = s.cache.Delete(context.Background(), fmt.Sprintf("upstreams:model:%s", modelID))
}

// InvalidateAllCache 使所有缓存失效
func (s *UpstreamSelector) InvalidateAllCache() {
	// 由于 cache 接口不支持通配符删除，逐一删除 modelID 对应的缓存
	// 通过查询所有模型 ID 来清理
	var modelIDs []string
	s.db.Model(&model.Model{}).Pluck("id", &modelIDs)
	for _, id := range modelIDs {
		_ = s.cache.Delete(context.Background(), fmt.Sprintf("upstreams:model:%s", id))
	}
}

// GetUpstreamsByModel 获取模型的所有上游模型
func (s *UpstreamSelector) GetUpstreamsByModel(modelID string) ([]*model.Upstream, error) {
	var upstreams []*model.Upstream
	if err := s.db.Where("model_id = ?", modelID).Find(&upstreams).Error; err != nil {
		return nil, err
	}
	return upstreams, nil
}
