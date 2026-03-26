package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/lyp256/airouter/internal/model"
	"gorm.io/gorm"
)

var (
	// ErrQuotaExceeded 配额超限
	ErrQuotaExceeded = errors.New("配额已用尽")
	// ErrQuotaWarning 配额警告
	ErrQuotaWarning = errors.New("配额接近上限")
)

// QuotaConfig 配额配置
type QuotaConfig struct {
	WarningThreshold float64        // 警告阈值（百分比）
	AlertChannels    []AlertChannel // 告警通道
}

// AlertChannel 告警通道接口
type AlertChannel interface {
	Send(ctx context.Context, alert QuotaAlert) error
	Name() string
}

// QuotaAlert 配额告警
type QuotaAlert struct {
	Type       string    // warning, exceeded
	UserID     string    // 用户ID
	UserKeyID  string    // 用户密钥ID（可选）
	QuotaLimit int64     // 配额上限
	QuotaUsed  int64     // 已使用配额
	UsageRate  float64   // 使用率
	Message    string    // 告警消息
	Timestamp  time.Time // 时间戳
}

// LogAlertChannel 日志告警通道（默认实现）
type LogAlertChannel struct {
	logger func(format string, args ...interface{})
}

// NewLogAlertChannel 创建日志告警通道
func NewLogAlertChannel(logger func(format string, args ...interface{})) *LogAlertChannel {
	return &LogAlertChannel{logger: logger}
}

// Send 发送告警
func (c *LogAlertChannel) Send(ctx context.Context, alert QuotaAlert) error {
	if c.logger != nil {
		c.logger("[配额告警] %s - 用户: %s, 使用率: %.2f%%, 已用: %d/%d",
			alert.Type, alert.UserID, alert.UsageRate*100, alert.QuotaUsed, alert.QuotaLimit)
	}
	return nil
}

// Name 返回通道名称
func (c *LogAlertChannel) Name() string {
	return "log"
}

// QuotaService 配额服务
type QuotaService struct {
	db       *gorm.DB
	cfg      *QuotaConfig
	alerters []AlertChannel
	mu       sync.RWMutex
	warned   map[string]bool // 已发送警告的用户/密钥
}

// NewQuotaService 创建配额服务
func NewQuotaService(db *gorm.DB, cfg *QuotaConfig) *QuotaService {
	if cfg == nil {
		cfg = &QuotaConfig{
			WarningThreshold: 0.8, // 默认 80% 警告
		}
	}
	return &QuotaService{
		db:     db,
		cfg:    cfg,
		warned: make(map[string]bool),
	}
}

// AddAlerter 添加告警通道
func (s *QuotaService) AddAlerter(alerter AlertChannel) {
	s.alerters = append(s.alerters, alerter)
}

// CheckUserKeyQuota 检查用户密钥配额
func (s *QuotaService) CheckUserKeyQuota(ctx context.Context, keyID string) error {
	var key model.UserKey
	if err := s.db.First(&key, "id = ?", keyID).Error; err != nil {
		return err
	}

	// 检查配额限制
	if key.QuotaLimit > 0 && key.QuotaUsed >= key.QuotaLimit {
		return ErrQuotaExceeded
	}

	// 检查过期时间
	if key.ExpiredAt != nil && key.ExpiredAt.Before(time.Now()) {
		return errors.New("密钥已过期")
	}

	// 检查是否需要发送警告
	if key.QuotaLimit > 0 {
		usageRate := float64(key.QuotaUsed) / float64(key.QuotaLimit)
		if usageRate >= s.cfg.WarningThreshold {
			s.sendAlertIfNeeded(ctx, key.UserID, keyID, key.QuotaLimit, key.QuotaUsed, usageRate)
		}
	}

	return nil
}

// CheckProviderKeyQuota 检查供应商密钥配额
func (s *QuotaService) CheckProviderKeyQuota(ctx context.Context, keyID string) error {
	var key model.ProviderKey
	if err := s.db.First(&key, "id = ?", keyID).Error; err != nil {
		return err
	}

	// 检查配额限制
	if key.QuotaLimit > 0 && key.QuotaUsed >= key.QuotaLimit {
		return ErrQuotaExceeded
	}

	return nil
}

// UpdateUserKeyUsage 更新用户密钥使用量
func (s *QuotaService) UpdateUserKeyUsage(ctx context.Context, keyID string, delta int64) error {
	return s.db.Model(&model.UserKey{}).
		Where("id = ?", keyID).
		UpdateColumn("quota_used", gorm.Expr("quota_used + ?", delta)).Error
}

// UpdateProviderKeyUsage 更新供应商密钥使用量
func (s *QuotaService) UpdateProviderKeyUsage(ctx context.Context, keyID string, delta int64) error {
	return s.db.Model(&model.ProviderKey{}).
		Where("id = ?", keyID).
		UpdateColumn("quota_used", gorm.Expr("quota_used + ?", delta)).Error
}

// GetUserKeyUsage 获取用户密钥使用情况
func (s *QuotaService) GetUserKeyUsage(ctx context.Context, keyID string) (*QuotaUsage, error) {
	var key model.UserKey
	if err := s.db.First(&key, "id = ?", keyID).Error; err != nil {
		return nil, err
	}

	usage := &QuotaUsage{
		KeyID:      keyID,
		UserID:     key.UserID,
		QuotaLimit: key.QuotaLimit,
		QuotaUsed:  key.QuotaUsed,
	}

	if key.QuotaLimit > 0 {
		usage.UsageRate = float64(key.QuotaUsed) / float64(key.QuotaLimit)
		usage.Remaining = key.QuotaLimit - key.QuotaUsed
	}

	return usage, nil
}

// GetProviderKeyUsage 获取供应商密钥使用情况
func (s *QuotaService) GetProviderKeyUsage(ctx context.Context, keyID string) (*QuotaUsage, error) {
	var key model.ProviderKey
	if err := s.db.First(&key, "id = ?", keyID).Error; err != nil {
		return nil, err
	}

	usage := &QuotaUsage{
		KeyID:      keyID,
		QuotaLimit: key.QuotaLimit,
		QuotaUsed:  key.QuotaUsed,
	}

	if key.QuotaLimit > 0 {
		usage.UsageRate = float64(key.QuotaUsed) / float64(key.QuotaLimit)
		usage.Remaining = key.QuotaLimit - key.QuotaUsed
	}

	return usage, nil
}

// ResetUserKeyQuota 重置用户密钥配额
func (s *QuotaService) ResetUserKeyQuota(ctx context.Context, keyID string) error {
	return s.db.Model(&model.UserKey{}).
		Where("id = ?", keyID).
		Update("quota_used", 0).Error
}

// ResetProviderKeyQuota 重置供应商密钥配额
func (s *QuotaService) ResetProviderKeyQuota(ctx context.Context, keyID string) error {
	return s.db.Model(&model.ProviderKey{}).
		Where("id = ?", keyID).
		Update("quota_used", 0).Error
}

// sendAlertIfNeeded 如果需要则发送告警
func (s *QuotaService) sendAlertIfNeeded(ctx context.Context, userID, keyID string, limit, used int64, usageRate float64) {
	alertKey := fmt.Sprintf("%s:%s", userID, keyID)

	s.mu.RLock()
	warned := s.warned[alertKey]
	s.mu.RUnlock()

	if warned {
		return
	}

	// 发送告警
	alert := QuotaAlert{
		Type:       "warning",
		UserID:     userID,
		UserKeyID:  keyID,
		QuotaLimit: limit,
		QuotaUsed:  used,
		UsageRate:  usageRate,
		Message:    fmt.Sprintf("配额使用率已达 %.0f%%，请注意控制用量", usageRate*100),
		Timestamp:  time.Now(),
	}

	for _, alerter := range s.alerters {
		go func(a AlertChannel) {
			_ = a.Send(ctx, alert)
		}(alerter)
	}

	// 标记已警告
	s.mu.Lock()
	s.warned[alertKey] = true
	s.mu.Unlock()
}

// ClearWarningFlag 清除警告标记（配额重置后调用）
func (s *QuotaService) ClearWarningFlag(userID, keyID string) {
	alertKey := fmt.Sprintf("%s:%s", userID, keyID)
	s.mu.Lock()
	delete(s.warned, alertKey)
	s.mu.Unlock()
}

// QuotaUsage 配额使用情况
type QuotaUsage struct {
	KeyID      string  `json:"key_id"`
	UserID     string  `json:"user_id,omitempty"`
	QuotaLimit int64   `json:"quota_limit"`
	QuotaUsed  int64   `json:"quota_used"`
	Remaining  int64   `json:"remaining"`
	UsageRate  float64 `json:"usage_rate"`
}
