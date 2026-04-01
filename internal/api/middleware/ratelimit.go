package middleware

import (
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// RateLimiter 限流器
type RateLimiter struct {
	mu       sync.Mutex
	requests map[string]*clientInfo
	rpm      int           // 每分钟请求数
	ticker   *time.Ticker  // 定时清理器
	stopCh   chan struct{} // 停止信号
}

type clientInfo struct {
	count     int
	resetTime time.Time
}

// NewRateLimiter 创建限流器
func NewRateLimiter(rpm int) *RateLimiter {
	rl := &RateLimiter{
		requests: make(map[string]*clientInfo),
		rpm:      rpm,
		stopCh:   make(chan struct{}),
	}
	// 启动定时清理协程，每分钟清理一次过期记录
	rl.ticker = time.NewTicker(time.Minute)
	go rl.cleanupLoop()
	return rl
}

// cleanupLoop 定时清理过期记录
func (rl *RateLimiter) cleanupLoop() {
	for {
		select {
		case <-rl.ticker.C:
			rl.Cleanup()
		case <-rl.stopCh:
			return
		}
	}
}

// Stop 停止限流器（用于优雅关闭）
func (rl *RateLimiter) Stop() {
	close(rl.stopCh)
	rl.ticker.Stop()
}

// Allow 检查是否允许请求
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	info, exists := rl.requests[key]

	if !exists || now.After(info.resetTime) {
		rl.requests[key] = &clientInfo{
			count:     1,
			resetTime: now.Add(time.Minute),
		}
		return true
	}

	if info.count >= rl.rpm {
		return false
	}

	info.count++
	return true
}

// Cleanup 清理过期记录
func (rl *RateLimiter) Cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for key, info := range rl.requests {
		if now.After(info.resetTime) {
			delete(rl.requests, key)
		}
	}
}

// RateLimit 限流中间件
func RateLimit(limiter *RateLimiter, keyFunc func(c *gin.Context) string) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := keyFunc(c)
		if key == "" {
			key = c.ClientIP()
		}

		if !limiter.Allow(key) {
			c.JSON(429, gin.H{
				"error": gin.H{
					"message": "请求过于频繁，请稍后再试",
					"type":    "rate_limit_error",
				},
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// RateLimitByUserKey 按用户密钥限流
func RateLimitByUserKey(limiter *RateLimiter) gin.HandlerFunc {
	return RateLimit(limiter, func(c *gin.Context) string {
		return GetUserKeyID(c)
	})
}
