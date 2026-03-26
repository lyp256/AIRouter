package service

import (
	"errors"
	"math/rand"
	"sort"
	"sync"
	"time"

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
	mu        sync.RWMutex
	cache     map[string][]*model.Upstream // modelID -> upstreams
}

// NewUpstreamSelector 创建上游模型选择器
func NewUpstreamSelector(db *gorm.DB, encryptor *crypto.Encryptor) *UpstreamSelector {
	return &UpstreamSelector{
		db:        db,
		encryptor: encryptor,
		cache:     make(map[string][]*model.Upstream),
	}
}

// SelectUpstream 选择一个上游模型
func (s *UpstreamSelector) SelectUpstream(modelID string) (*UpstreamSelection, error) {
	s.mu.RLock()
	upstreams, ok := s.cache[modelID]
	s.mu.RUnlock()

	if !ok || len(upstreams) == 0 {
		// 从数据库加载
		if err := s.loadUpstreams(modelID); err != nil {
			return nil, err
		}
		s.mu.RLock()
		upstreams = s.cache[modelID]
		s.mu.RUnlock()
	}

	if len(upstreams) == 0 {
		return nil, ErrNoAvailableUpstream
	}

	// 按优先级和权重选择
	upstream := s.selectByWeight(upstreams)
	if upstream == nil {
		return nil, ErrNoAvailableUpstream
	}

	// 获取关联的 Provider
	var provider model.Provider
	if err := s.db.First(&provider, "id = ?", upstream.ProviderID).Error; err != nil {
		return nil, err
	}

	// 获取关联的 ProviderKey
	var apiKey model.ProviderKey
	if err := s.db.First(&apiKey, "id = ?", upstream.ProviderKeyID).Error; err != nil {
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

// selectByWeight 根据权重选择上游模型
func (s *UpstreamSelector) selectByWeight(upstreams []*model.Upstream) *model.Upstream {
	// 过滤出活跃状态的上游模型
	activeUpstreams := make([]*model.Upstream, 0, len(upstreams))
	for _, u := range upstreams {
		if u.Status == "active" && u.Enabled {
			activeUpstreams = append(activeUpstreams, u)
		}
	}

	if len(activeUpstreams) == 0 {
		return nil
	}

	// 按优先级排序
	sort.Slice(activeUpstreams, func(i, j int) bool {
		return activeUpstreams[i].Priority > activeUpstreams[j].Priority
	})

	// 找出最高优先级的上游模型
	maxPriority := activeUpstreams[0].Priority
	priorityUpstreams := make([]*model.Upstream, 0)
	for _, u := range activeUpstreams {
		if u.Priority == maxPriority {
			priorityUpstreams = append(priorityUpstreams, u)
		}
	}

	// 按权重随机选择
	totalWeight := 0
	for _, u := range priorityUpstreams {
		totalWeight += u.Weight
	}

	if totalWeight == 0 {
		return priorityUpstreams[0]
	}

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	n := r.Intn(totalWeight)
	for _, u := range priorityUpstreams {
		n -= u.Weight
		if n < 0 {
			return u
		}
	}

	return priorityUpstreams[0]
}

// loadUpstreams 从数据库加载上游模型
func (s *UpstreamSelector) loadUpstreams(modelID string) error {
	var upstreams []*model.Upstream
	if err := s.db.Where("model_id = ?", modelID).Find(&upstreams).Error; err != nil {
		return err
	}

	s.mu.Lock()
	s.cache[modelID] = upstreams
	s.mu.Unlock()

	return nil
}

// MarkUpstreamError 标记上游模型错误
func (s *UpstreamSelector) MarkUpstreamError(upstreamID string) error {
	return s.db.Model(&model.Upstream{}).
		Where("id = ?", upstreamID).
		Updates(map[string]interface{}{
			"status": "error",
		}).Error
}

// MarkUpstreamSuccess 标记上游模型成功
func (s *UpstreamSelector) MarkUpstreamSuccess(upstreamID string) error {
	return s.db.Model(&model.Upstream{}).
		Where("id = ?", upstreamID).
		Updates(map[string]interface{}{
			"status": "active",
		}).Error
}

// MarkAPIKeyError 标记供应商密钥错误
func (s *UpstreamSelector) MarkAPIKeyError(apiKeyID string) error {
	now := time.Now()
	return s.db.Model(&model.ProviderKey{}).
		Where("id = ?", apiKeyID).
		Updates(map[string]interface{}{
			"last_error_at": now,
			"status":        "error",
		}).Error
}

// MarkAPIKeySuccess 标记供应商密钥成功
func (s *UpstreamSelector) MarkAPIKeySuccess(apiKeyID string) error {
	now := time.Now()
	return s.db.Model(&model.ProviderKey{}).
		Where("id = ?", apiKeyID).
		Updates(map[string]interface{}{
			"last_used_at": now,
			"status":       "active",
		}).Error
}

// UpdateQuotaUsed 更新已使用配额
func (s *UpstreamSelector) UpdateQuotaUsed(apiKeyID string, delta int64) error {
	return s.db.Model(&model.ProviderKey{}).
		Where("id = ?", apiKeyID).
		UpdateColumn("quota_used", gorm.Expr("quota_used + ?", delta)).Error
}

// InvalidateCache 使缓存失效
func (s *UpstreamSelector) InvalidateCache(modelID string) {
	s.mu.Lock()
	delete(s.cache, modelID)
	s.mu.Unlock()
}

// InvalidateAllCache 使所有缓存失效
func (s *UpstreamSelector) InvalidateAllCache() {
	s.mu.Lock()
	s.cache = make(map[string][]*model.Upstream)
	s.mu.Unlock()
}

// GetUpstreamsByModel 获取模型的所有上游模型
func (s *UpstreamSelector) GetUpstreamsByModel(modelID string) ([]*model.Upstream, error) {
	var upstreams []*model.Upstream
	if err := s.db.Where("model_id = ?", modelID).Find(&upstreams).Error; err != nil {
		return nil, err
	}
	return upstreams, nil
}
