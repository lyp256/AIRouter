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
	rpm      int // 每分钟请求数
}

type clientInfo struct {
	count     int
	resetTime time.Time
}

// NewRateLimiter 创建限流器
func NewRateLimiter(rpm int) *RateLimiter {
	return &RateLimiter{
		requests: make(map[string]*clientInfo),
		rpm:      rpm,
	}
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
