package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lyp256/airouter/internal/cache"
	"github.com/lyp256/airouter/internal/config"
	"github.com/lyp256/airouter/internal/model"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

const (
	// upstreamHealthTTL 健康状态缓存 TTL
	upstreamHealthTTL = 1 * time.Hour
	// upstreamHealthKeyPrefix 上游健康状态缓存 key 前缀
	upstreamHealthKeyPrefix = "upstream:health:"
	// leaderKeyPrefix 选主 key 前缀
	leaderKeyPrefix = "leader:health-check:"
)

// UpstreamHealthStatus 上游健康状态（存储在缓存中）
type UpstreamHealthStatus struct {
	UpstreamID    string    `json:"upstream_id"`
	Status        string    `json:"status"` // "active" / "error"
	LastCheckTime time.Time `json:"last_check_time"`
	LastErrorTime time.Time `json:"last_error_time,omitempty"`
	ConsecSuccess int       `json:"consec_success"`
	ConsecFail    int       `json:"consec_fail"`
	ResponseMs    int64     `json:"response_ms"`
}

// leaderInfo 选主信息
type leaderInfo struct {
	InstanceID string    `json:"instance_id"`
	AcquiredAt time.Time `json:"acquired_at"`
	RenewedAt  time.Time `json:"renewed_at"`
}

// UpstreamHealthCheckService 上游健康检查服务
type UpstreamHealthCheckService struct {
	db         *gorm.DB
	cache      cache.Cache
	cfg        *config.HealthCheckConfig
	logger     *zap.Logger
	instanceID string
	httpClient *http.Client

	running  atomic.Bool
	stopCh   chan struct{}
	stopOnce sync.Once
}

// NewUpstreamHealthCheckService 创建上游健康检查服务
func NewUpstreamHealthCheckService(
	db *gorm.DB,
	c cache.Cache,
	cfg *config.HealthCheckConfig,
	logger *zap.Logger,
) *UpstreamHealthCheckService {
	instanceID := fmt.Sprintf("%d-%d", time.Now().UnixNano(), time.Now().Nanosecond())
	return &UpstreamHealthCheckService{
		db:         db,
		cache:      c,
		cfg:        cfg,
		logger:     logger,
		instanceID: instanceID,
		httpClient: &http.Client{Timeout: cfg.Timeout},
		stopCh:     make(chan struct{}),
	}
}

// Start 启动健康检查服务
func (s *UpstreamHealthCheckService) Start(ctx context.Context) {
	if !s.running.CompareAndSwap(false, true) {
		return
	}

	s.logger.Info("上游健康检查服务启动",
		zap.String("instance_id", s.instanceID),
		zap.Bool("distributed", s.cache.IsDistributed()))

	var wg sync.WaitGroup
	wg.Add(2)

	// 启动全量检查
	go func() {
		defer wg.Done()
		s.runFullCheck(ctx)
	}()

	// 启动快速恢复检查
	go func() {
		defer wg.Done()
		s.runRecoveryCheck(ctx)
	}()

	// 等待停止
	go func() {
		select {
		case <-ctx.Done():
		case <-s.stopCh:
		}
		wg.Wait()
		s.running.Store(false)
	}()
}

// Stop 停止健康检查服务
func (s *UpstreamHealthCheckService) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
}

// runFullCheck 全量检查循环
func (s *UpstreamHealthCheckService) runFullCheck(ctx context.Context) {
	// 立即执行一次
	s.runFullCheckOnce(ctx)

	ticker := time.NewTicker(s.cfg.FullCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.runFullCheckOnce(ctx)
		}
	}
}

// runRecoveryCheck 快速恢复检查循环
func (s *UpstreamHealthCheckService) runRecoveryCheck(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.RecoveryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.runRecoveryCheckOnce(ctx)
		}
	}
}

// runFullCheckOnce 执行一次全量检查
func (s *UpstreamHealthCheckService) runFullCheckOnce(ctx context.Context) {
	// 尝试获取选主
	leaderKey := leaderKeyPrefix + "full"
	if !s.tryAcquireLeader(ctx, leaderKey) {
		return
	}

	// 启动续约
	renewCtx, cancelRenew := context.WithCancel(ctx)
	defer cancelRenew()
	go s.renewLeader(renewCtx, leaderKey)

	// 查询所有启用的上游
	var upstreams []model.Upstream
	if err := s.db.Where("enabled = ?", true).Find(&upstreams).Error; err != nil {
		s.logger.Error("全量检查查询上游失败", zap.Error(err))
		return
	}

	if len(upstreams) == 0 {
		return
	}

	// 按 (providerID, providerKeyID) 去重，减少重复探测
	type probeTarget struct {
		ProviderID    string
		ProviderKeyID string
	}
	probeResults := make(map[probeTarget]*probeResult)

	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := range upstreams {
		u := &upstreams[i]
		target := probeTarget{
			ProviderID:    u.ProviderID,
			ProviderKeyID: u.ProviderKeyID,
		}

		if _, exists := probeResults[target]; exists {
			continue // 已有相同供应商+密钥的探测结果
		}

		wg.Add(1)
		go func(t probeTarget) {
			defer wg.Done()
			result := s.probeUpstream(ctx, t.ProviderID, t.ProviderKeyID)
			mu.Lock()
			probeResults[t] = result
			mu.Unlock()
		}(target)
	}

	wg.Wait()

	// 根据探测结果更新每个上游的健康状态
	for i := range upstreams {
		u := &upstreams[i]
		target := probeTarget{
			ProviderID:    u.ProviderID,
			ProviderKeyID: u.ProviderKeyID,
		}
		result, ok := probeResults[target]
		if !ok {
			continue
		}

		s.updateUpstreamHealth(ctx, u.ID, result)
	}
}

// runRecoveryCheckOnce 执行一次快速恢复检查
func (s *UpstreamHealthCheckService) runRecoveryCheckOnce(ctx context.Context) {
	// 尝试获取选主
	leaderKey := leaderKeyPrefix + "recovery"
	if !s.tryAcquireLeader(ctx, leaderKey) {
		return
	}

	// 启动续约
	renewCtx, cancelRenew := context.WithCancel(ctx)
	defer cancelRenew()
	go s.renewLeader(renewCtx, leaderKey)

	// 查询所有启用的上游
	var upstreams []model.Upstream
	if err := s.db.Where("enabled = ?", true).Find(&upstreams).Error; err != nil {
		s.logger.Error("恢复检查查询上游失败", zap.Error(err))
		return
	}

	// 只检查不健康的上游
	var unhealthy []model.Upstream
	for i := range upstreams {
		health := GetUpstreamHealthFromCache(s.cache, upstreams[i].ID)
		if health != nil && health.Status == "error" {
			unhealthy = append(unhealthy, upstreams[i])
		}
	}

	if len(unhealthy) == 0 {
		return
	}

	s.logger.Info("恢复检查发现不健康上游", zap.Int("count", len(unhealthy)))

	// 按 (providerID, providerKeyID) 去重
	type probeTarget struct {
		ProviderID    string
		ProviderKeyID string
	}
	probeResults := make(map[probeTarget]*probeResult)

	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := range unhealthy {
		u := &unhealthy[i]
		target := probeTarget{
			ProviderID:    u.ProviderID,
			ProviderKeyID: u.ProviderKeyID,
		}

		if _, exists := probeResults[target]; exists {
			continue
		}

		wg.Add(1)
		go func(t probeTarget) {
			defer wg.Done()
			result := s.probeUpstream(ctx, t.ProviderID, t.ProviderKeyID)
			mu.Lock()
			probeResults[t] = result
			mu.Unlock()
		}(target)
	}

	wg.Wait()

	for i := range unhealthy {
		u := &unhealthy[i]
		target := probeTarget{
			ProviderID:    u.ProviderID,
			ProviderKeyID: u.ProviderKeyID,
		}
		result, ok := probeResults[target]
		if !ok {
			continue
		}
		s.updateUpstreamHealth(ctx, u.ID, result)
	}
}

// probeResult 探测结果
type probeResult struct {
	success      bool
	responseMs   int64
	errorMessage string
}

// probeUpstream 探测上游（通过供应商 /v1/models 端点）
func (s *UpstreamHealthCheckService) probeUpstream(ctx context.Context, providerID, providerKeyID string) *probeResult {
	// 获取供应商
	var provider model.Provider
	if err := s.db.First(&provider, "id = ?", providerID).Error; err != nil {
		return &probeResult{success: false, errorMessage: fmt.Sprintf("供应商不存在: %s", providerID)}
	}

	// 获取密钥
	var apiKey model.ProviderKey
	if err := s.db.First(&apiKey, "id = ?", providerKeyID).Error; err != nil {
		return &probeResult{success: false, errorMessage: fmt.Sprintf("密钥不存在: %s", providerKeyID)}
	}

	// 发送探测请求
	start := time.Now()
	err := s.doHealthCheck(ctx, provider.BaseURL, apiKey.Key)
	responseMs := time.Since(start).Milliseconds()

	if err != nil {
		return &probeResult{
			success:      false,
			responseMs:   responseMs,
			errorMessage: err.Error(),
		}
	}

	return &probeResult{
		success:    true,
		responseMs: responseMs,
	}
}

// doHealthCheck 执行健康检查 HTTP 请求
func (s *UpstreamHealthCheckService) doHealthCheck(ctx context.Context, baseURL, apiKey string) error {
	url := baseURL + "/v1/models"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return fmt.Errorf("服务器错误: %d", resp.StatusCode)
	}

	// 429 限流不算不健康
	if resp.StatusCode == 429 {
		return nil
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("请求错误: %d", resp.StatusCode)
	}

	// 验证响应为有效 JSON
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("响应解析失败: %w", err)
	}

	return nil
}

// updateUpstreamHealth 根据探测结果更新上游健康状态
func (s *UpstreamHealthCheckService) updateUpstreamHealth(ctx context.Context, upstreamID string, result *probeResult) {
	healthKey := upstreamHealthKeyPrefix + upstreamID

	// 读取当前健康状态
	var health UpstreamHealthStatus
	err := s.cache.Get(ctx, healthKey, &health)
	if err != nil {
		// 缓存未命中，创建新记录
		health = UpstreamHealthStatus{
			UpstreamID: upstreamID,
			Status:     "active",
		}
	}

	// 更新连续计数
	if result.success {
		health.ConsecSuccess++
		health.ConsecFail = 0
	} else {
		health.ConsecFail++
		health.ConsecSuccess = 0
	}

	health.ResponseMs = result.responseMs
	health.LastCheckTime = time.Now()

	// 根据阈值判断状态
	if result.success {
		if health.ConsecSuccess >= s.cfg.HealthyThreshold {
			health.Status = "active"
		}
	} else {
		if health.ConsecFail >= s.cfg.UnhealthyThreshold {
			health.Status = "error"
			health.LastErrorTime = time.Now()
		}
	}

	// 写回缓存
	if err := s.cache.Set(ctx, healthKey, health, upstreamHealthTTL); err != nil {
		s.logger.Error("更新上游健康状态失败",
			zap.String("upstream_id", upstreamID),
			zap.Error(err))
	}
}

// tryAcquireLeader 尝试获取选主
func (s *UpstreamHealthCheckService) tryAcquireLeader(ctx context.Context, leaderKey string) bool {
	info := leaderInfo{
		InstanceID: s.instanceID,
		AcquiredAt: time.Now(),
		RenewedAt:  time.Now(),
	}

	ok, err := s.cache.SetNX(ctx, leaderKey, info, s.cfg.LeaderLease)
	if err != nil {
		s.logger.Error("选主失败", zap.String("key", leaderKey), zap.Error(err))
		return false
	}

	return ok
}

// renewLeader 续约选主
func (s *UpstreamHealthCheckService) renewLeader(ctx context.Context, leaderKey string) {
	ticker := time.NewTicker(s.cfg.LeaderRenewInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			info := leaderInfo{
				InstanceID: s.instanceID,
				AcquiredAt: time.Now(),
				RenewedAt:  time.Now(),
			}
			// 直接 Set 覆盖续约（只有 leader 持有该 key 时才能续约）
			if err := s.cache.Set(ctx, leaderKey, info, s.cfg.LeaderLease); err != nil {
				s.logger.Warn("选主续约失败", zap.String("key", leaderKey), zap.Error(err))
			}
		}
	}
}

// GetUpstreamHealthFromCache 从缓存获取上游健康状态
func GetUpstreamHealthFromCache(c cache.Cache, upstreamID string) *UpstreamHealthStatus {
	var health UpstreamHealthStatus
	err := c.Get(context.Background(), upstreamHealthKeyPrefix+upstreamID, &health)
	if err != nil {
		return nil
	}
	return &health
}

// SetUpstreamHealthToCache 设置上游健康状态到缓存
func SetUpstreamHealthToCache(c cache.Cache, upstreamID string, health *UpstreamHealthStatus) error {
	return c.Set(context.Background(), upstreamHealthKeyPrefix+upstreamID, health, upstreamHealthTTL)
}
