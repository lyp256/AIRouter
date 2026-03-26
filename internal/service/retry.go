package service

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/lyp256/airouter/internal/config"
)

// RetryableError 可重试的错误
type RetryableError struct {
	Err        error
	StatusCode int
}

func (e *RetryableError) Error() string {
	return e.Err.Error()
}

// IsRetryable 检查是否可重试
func (e *RetryableError) IsRetryable() bool {
	return true
}

// RetryService 重试服务
type RetryService struct {
	cfg        *config.RetryConfig
	httpClient *http.Client
	mu         sync.Mutex
}

// NewRetryService 创建重试服务
func NewRetryService(cfg *config.RetryConfig, httpClient *http.Client) *RetryService {
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 60 * time.Second,
		}
	}
	return &RetryService{
		cfg:        cfg,
		httpClient: httpClient,
	}
}

// Do 执行带重试的请求
func (s *RetryService) Do(ctx context.Context, fn func(ctx context.Context) (*http.Response, error)) (*http.Response, error) {
	if !s.cfg.Enabled {
		return fn(ctx)
	}

	var lastErr error
	var resp *http.Response

	for attempt := 1; attempt <= s.cfg.MaxAttempts; attempt++ {
		// 检查上下文是否已取消
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		resp, lastErr = fn(ctx)

		// 如果成功且状态码不在重试列表中，直接返回
		if lastErr == nil && !s.shouldRetry(resp.StatusCode) {
			return resp, nil
		}

		// 如果是最后一次尝试，不再重试
		if attempt == s.cfg.MaxAttempts {
			break
		}

		// 计算等待时间
		waitTime := s.calculateWaitTime(attempt)

		// 等待后重试
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(waitTime):
		}
	}

	return resp, lastErr
}

// shouldRetry 检查是否应该重试
func (s *RetryService) shouldRetry(statusCode int) bool {
	for _, code := range s.cfg.RetryOnCodes {
		if statusCode == code {
			return true
		}
	}
	return false
}

// calculateWaitTime 计算等待时间（指数退避 + 抖动）
func (s *RetryService) calculateWaitTime(attempt int) time.Duration {
	// 指数退避
	wait := float64(s.cfg.InitialWait)
	for i := 1; i < attempt; i++ {
		wait *= s.cfg.Multiplier
	}

	// 添加抖动（±20%）
	jitter := wait * 0.2 * (rand.Float64()*2 - 1)
	wait += jitter

	// 不超过最大等待时间
	if wait > float64(s.cfg.MaxWait) {
		wait = float64(s.cfg.MaxWait)
	}

	return time.Duration(wait)
}

// DoWithBackoff 执行带退避的重试操作
func DoWithBackoff(ctx context.Context, cfg *config.RetryConfig, fn func() error) error {
	if !cfg.Enabled {
		return fn()
	}

	var lastErr error
	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		lastErr = fn()
		if lastErr == nil {
			return nil
		}

		// 如果是最后一次尝试，不再等待
		if attempt == cfg.MaxAttempts {
			break
		}

		// 计算等待时间
		wait := float64(cfg.InitialWait)
		for i := 1; i < attempt; i++ {
			wait *= cfg.Multiplier
		}

		// 添加抖动
		jitter := wait * 0.2 * (rand.Float64()*2 - 1)
		wait += jitter

		if wait > float64(cfg.MaxWait) {
			wait = float64(cfg.MaxWait)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(wait)):
		}
	}

	return fmt.Errorf("重试 %d 次后仍然失败: %w", cfg.MaxAttempts, lastErr)
}
