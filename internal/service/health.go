package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/lyp256/airouter/internal/model"
	"gorm.io/gorm"
)

// HealthStatus 健康状态
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusUnknown   HealthStatus = "unknown"
)

// ProviderHealth 供应商健康状态
type ProviderHealth struct {
	ProviderID    string       `json:"provider_id"`
	ProviderName  string       `json:"provider_name"`
	Status        HealthStatus `json:"status"`
	LastCheck     time.Time    `json:"last_check"`
	LastError     string       `json:"last_error,omitempty"`
	ResponseTime  int64        `json:"response_time"`  // 毫秒
	SuccessRate   float64      `json:"success_rate"`   // 成功率
	ConsecutiveOk int          `json:"consecutive_ok"` // 连续成功次数
	ConsecutiveEr int          `json:"consecutive_er"` // 连续失败次数
}

// HealthCheckConfig 健康检查配置
type HealthCheckConfig struct {
	Enabled            bool          `mapstructure:"enabled"`
	Interval           time.Duration `mapstructure:"interval"`            // 检查间隔
	Timeout            time.Duration `mapstructure:"timeout"`             // 请求超时
	HealthyThreshold   int           `mapstructure:"healthy_threshold"`   // 健康阈值（连续成功次数）
	UnhealthyThreshold int           `mapstructure:"unhealthy_threshold"` // 不健康阈值（连续失败次数）
}

// HealthCheckService 健康检查服务
type HealthCheckService struct {
	db             *gorm.DB
	cfg            *HealthCheckConfig
	httpClient     *http.Client
	health         map[string]*ProviderHealth
	mu             sync.RWMutex
	stopCh         chan struct{}
	running        bool
	onStatusChange func(providerID string, oldStatus, newStatus HealthStatus)
}

// NewHealthCheckService 创建健康检查服务
func NewHealthCheckService(db *gorm.DB, cfg *HealthCheckConfig) *HealthCheckService {
	if cfg == nil {
		cfg = &HealthCheckConfig{
			Enabled:            true,
			Interval:           30 * time.Second,
			Timeout:            10 * time.Second,
			HealthyThreshold:   2,
			UnhealthyThreshold: 3,
		}
	}
	return &HealthCheckService{
		db:     db,
		cfg:    cfg,
		health: make(map[string]*ProviderHealth),
		stopCh: make(chan struct{}),
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

// SetStatusChangeCallback 设置状态变更回调
func (s *HealthCheckService) SetStatusChangeCallback(cb func(providerID string, oldStatus, newStatus HealthStatus)) {
	s.onStatusChange = cb
}

// Start 启动健康检查
func (s *HealthCheckService) Start(ctx context.Context) error {
	if !s.cfg.Enabled {
		return nil
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = true
	s.mu.Unlock()

	go s.run(ctx)
	return nil
}

// Stop 停止健康检查
func (s *HealthCheckService) Stop() {
	s.mu.Lock()
	s.running = false
	s.mu.Unlock()
	close(s.stopCh)
}

// run 运行健康检查循环
func (s *HealthCheckService) run(ctx context.Context) {
	// 立即执行一次检查
	s.checkAll(ctx)

	ticker := time.NewTicker(s.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.checkAll(ctx)
		}
	}
}

// checkAll 检查所有供应商
func (s *HealthCheckService) checkAll(ctx context.Context) {
	var providers []*model.Provider
	if err := s.db.Where("enabled = ?", true).Find(&providers).Error; err != nil {
		return
	}

	var wg sync.WaitGroup
	for _, p := range providers {
		wg.Add(1)
		go func(provider *model.Provider) {
			defer wg.Done()
			s.checkProvider(ctx, provider)
		}(p)
	}
	wg.Wait()
}

// checkProvider 检查单个供应商
func (s *HealthCheckService) checkProvider(ctx context.Context, provider *model.Provider) {
	// 获取一个活跃的密钥
	var key model.ProviderKey
	if err := s.db.Where("provider_id = ? AND status = ?", provider.ID, "active").First(&key).Error; err != nil {
		s.updateHealth(provider.ID, provider.Name, HealthStatusUnknown, "无可用密钥", 0)
		return
	}

	// 执行健康检查请求
	start := time.Now()
	err := s.doHealthCheck(ctx, provider.BaseURL, key.Key)
	responseTime := time.Since(start).Milliseconds()

	// 获取当前健康状态
	s.mu.RLock()
	currentHealth, exists := s.health[provider.ID]
	s.mu.RUnlock()

	if !exists {
		currentHealth = &ProviderHealth{
			ProviderID:   provider.ID,
			ProviderName: provider.Name,
			Status:       HealthStatusUnknown,
		}
	}

	var newStatus HealthStatus
	var lastError string

	if err != nil {
		currentHealth.ConsecutiveEr++
		currentHealth.ConsecutiveOk = 0
		lastError = err.Error()

		if currentHealth.ConsecutiveEr >= s.cfg.UnhealthyThreshold {
			newStatus = HealthStatusUnhealthy
		} else {
			newStatus = currentHealth.Status
		}
	} else {
		currentHealth.ConsecutiveOk++
		currentHealth.ConsecutiveEr = 0

		if currentHealth.ConsecutiveOk >= s.cfg.HealthyThreshold {
			newStatus = HealthStatusHealthy
		} else {
			newStatus = currentHealth.Status
		}
	}

	s.updateHealth(provider.ID, provider.Name, newStatus, lastError, responseTime)
}

// doHealthCheck 执行健康检查请求
func (s *HealthCheckService) doHealthCheck(ctx context.Context, baseURL, apiKey string) error {
	// 使用 /v1/models 端点进行健康检查
	url := baseURL + "/v1/models"

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
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

	if resp.StatusCode == 429 {
		// 速率限制不算不健康
		return nil
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("请求错误: %d", resp.StatusCode)
	}

	// 尝试解析响应，确保返回有效 JSON
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("响应解析失败: %w", err)
	}

	return nil
}

// updateHealth 更新健康状态
func (s *HealthCheckService) updateHealth(providerID, providerName string, status HealthStatus, lastError string, responseTime int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	oldHealth, exists := s.health[providerID]
	oldStatus := HealthStatusUnknown
	if exists {
		oldStatus = oldHealth.Status
	}

	s.health[providerID] = &ProviderHealth{
		ProviderID:    providerID,
		ProviderName:  providerName,
		Status:        status,
		LastCheck:     time.Now(),
		LastError:     lastError,
		ResponseTime:  responseTime,
		ConsecutiveOk: oldHealth.ConsecutiveOk,
		ConsecutiveEr: oldHealth.ConsecutiveEr,
	}

	// 触发状态变更回调
	if s.onStatusChange != nil && oldStatus != status {
		go s.onStatusChange(providerID, oldStatus, status)
	}
}

// GetHealth 获取单个供应商健康状态
func (s *HealthCheckService) GetHealth(providerID string) *ProviderHealth {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.health[providerID]
}

// GetAllHealth 获取所有供应商健康状态
func (s *HealthCheckService) GetAllHealth() map[string]*ProviderHealth {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]*ProviderHealth)
	for k, v := range s.health {
		result[k] = v
	}
	return result
}

// IsHealthy 检查供应商是否健康
func (s *HealthCheckService) IsHealthy(providerID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	health, exists := s.health[providerID]
	if !exists {
		return false
	}
	return health.Status == HealthStatusHealthy
}

// RecordRequest 记录请求结果（用于更新成功率）
func (s *HealthCheckService) RecordRequest(providerID string, success bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	health, exists := s.health[providerID]
	if !exists {
		return
	}

	// 简单的成功率计算（最近100次请求）
	// 实际应用中可以使用更复杂的滑动窗口算法
	if success {
		health.ConsecutiveOk++
		health.ConsecutiveEr = 0
		if health.ConsecutiveOk >= s.cfg.HealthyThreshold {
			health.Status = HealthStatusHealthy
		}
	} else {
		health.ConsecutiveEr++
		health.ConsecutiveOk = 0
		if health.ConsecutiveEr >= s.cfg.UnhealthyThreshold {
			health.Status = HealthStatusUnhealthy
		}
	}
}
