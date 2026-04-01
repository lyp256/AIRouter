package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/lyp256/airouter/internal/crypto"
	"github.com/lyp256/airouter/internal/model"
	"gorm.io/gorm"
)

// APIKeyAuthConfig API Key 认证中间件配置
type APIKeyAuthConfig struct {
	DB        *gorm.DB
	Encryptor *crypto.Encryptor
	JWTConfig JWTConfig // 用于 JWT+KeyID 认证
}

// apiKeyCache API Key 缓存条目
type apiKeyCache struct {
	keyID      string
	userID     string
	keyHash    string // 原始 key 的 hash
	decrypted  string // 解密后的 key
	quotaUsed  int64
	quotaLimit int64
	expiredAt  *time.Time
	status     string
}

// APIKeyAuthenticator API Key 认证器（带缓存）
type APIKeyAuthenticator struct {
	db        *gorm.DB
	encryptor *crypto.Encryptor
	mu        sync.RWMutex
	cache     map[string]*apiKeyCache // keyID -> cache entry
	keyIndex  map[string]string       // keyHash -> keyID (用于快速查找)
	lastLoad  time.Time
}

// NewAPIKeyAuthenticator 创建 API Key 认证器
func NewAPIKeyAuthenticator(db *gorm.DB, encryptor *crypto.Encryptor) *APIKeyAuthenticator {
	return &APIKeyAuthenticator{
		db:        db,
		encryptor: encryptor,
		cache:     make(map[string]*apiKeyCache),
		keyIndex:  make(map[string]string),
	}
}

// RefreshCache 刷新缓存
func (a *APIKeyAuthenticator) RefreshCache() error {
	var userKeys []model.UserKey
	if err := a.db.Where("status = ?", "active").Find(&userKeys).Error; err != nil {
		return err
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// 重建缓存
	newCache := make(map[string]*apiKeyCache)
	newKeyIndex := make(map[string]string)

	for i := range userKeys {
		decryptedKey, err := a.encryptor.Decrypt(userKeys[i].Key)
		if err != nil {
			continue
		}

		keyHash := sha256Hash(decryptedKey)
		newCache[userKeys[i].ID] = &apiKeyCache{
			keyID:      userKeys[i].ID,
			userID:     userKeys[i].UserID,
			keyHash:    keyHash,
			decrypted:  decryptedKey,
			quotaUsed:  userKeys[i].QuotaUsed,
			quotaLimit: userKeys[i].QuotaLimit,
			expiredAt:  userKeys[i].ExpiredAt,
			status:     userKeys[i].Status,
		}
		newKeyIndex[keyHash] = userKeys[i].ID
	}

	a.cache = newCache
	a.keyIndex = newKeyIndex
	a.lastLoad = time.Now()
	return nil
}

// Authenticate 通过 API Key 认证
func (a *APIKeyAuthenticator) Authenticate(apiKey string) (*model.UserKey, error) {
	// 如果缓存为空或过期（5分钟），刷新缓存
	a.mu.RLock()
	cacheEmpty := len(a.cache) == 0
	cacheStale := time.Since(a.lastLoad) > 5*time.Minute
	a.mu.RUnlock()

	if cacheEmpty || cacheStale {
		if err := a.RefreshCache(); err != nil {
			return nil, err
		}
	}

	// 计算 API Key 的 hash
	keyHash := sha256Hash(apiKey)

	a.mu.RLock()
	defer a.mu.RUnlock()

	// 通过 hash 快速查找
	keyID, exists := a.keyIndex[keyHash]
	if !exists {
		return nil, nil // 未找到
	}

	entry, exists := a.cache[keyID]
	if !exists {
		return nil, nil
	}

	// 验证原始 key 是否匹配（双重验证）
	if entry.decrypted != apiKey {
		return nil, nil
	}

	// 从数据库获取最新的配额信息
	var userKey model.UserKey
	if err := a.db.First(&userKey, "id = ?", keyID).Error; err != nil {
		return nil, err
	}

	return &userKey, nil
}

// sha256Hash 计算 SHA256 hash
func sha256Hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// APIKeyAuth API Key 认证中间件
// 支持两种认证方式：
// 1. API Key 认证: Authorization: Bearer <api_key>
// 2. JWT + KeyID 认证: Authorization: Bearer <jwt_token>, X-Key-ID: <key_id>
func APIKeyAuth(cfg APIKeyAuthConfig) gin.HandlerFunc {
	// 创建认证器
	authenticator := NewAPIKeyAuthenticator(cfg.DB, cfg.Encryptor)

	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "未提供认证信息"})
			c.Abort()
			return
		}

		// 解析 Bearer Token
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "认证格式错误"})
			c.Abort()
			return
		}

		token := parts[1]
		keyID := c.GetHeader("X-Key-ID")

		// 如果有 X-Key-ID 头，尝试 JWT + KeyID 认证
		if keyID != "" {
			handleJWTKeyIDAuth(c, cfg, token, keyID)
			return
		}

		// 使用带缓存的认证器进行 API Key 认证
		userKey, err := authenticator.Authenticate(token)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "认证服务错误"})
			c.Abort()
			return
		}

		if userKey == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "无效的 API Key"})
			c.Abort()
			return
		}

		// 验证密钥状态、过期、配额
		if !validateUserKey(c, userKey) {
			return
		}

		// 设置上下文
		c.Set("user_id", userKey.UserID)
		c.Set("user_key_id", userKey.ID)
		c.Set("user_key", userKey)

		c.Next()
	}
}

// handleJWTKeyIDAuth 处理 JWT + KeyID 认证
func handleJWTKeyIDAuth(c *gin.Context, cfg APIKeyAuthConfig, tokenString, keyID string) {
	// 1. 解析并验证 JWT Token
	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(cfg.JWTConfig.Secret), nil
	})

	if err != nil || !token.Valid {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "无效的 Token"})
		c.Abort()
		return
	}

	claims, ok := token.Claims.(*JWTClaims)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Token 解析失败"})
		c.Abort()
		return
	}

	// 2. 根据 KeyID 和 UserID 查询用户密钥
	var userKey model.UserKey
	result := cfg.DB.Where("id = ? AND user_id = ? AND status = ?", keyID, claims.UserID, "active").First(&userKey)
	if result.Error != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "密钥不存在或无权访问"})
		c.Abort()
		return
	}

	// 3. 验证密钥状态、过期、配额
	if !validateUserKey(c, &userKey) {
		return
	}

	// 4. 设置上下文
	c.Set("user_id", userKey.UserID)
	c.Set("user_key_id", userKey.ID)
	c.Set("user_key", &userKey)
	c.Set("username", claims.Username)
	c.Set("role", claims.Role)

	c.Next()
}

// validateUserKey 验证用户密钥状态、过期时间和配额
// 返回 false 表示验证失败，已设置错误响应
func validateUserKey(c *gin.Context, userKey *model.UserKey) bool {
	// 检查过期时间
	if userKey.ExpiredAt != nil && userKey.ExpiredAt.Before(time.Now()) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "API Key 已过期"})
		c.Abort()
		return false
	}

	// 检查配额
	if userKey.QuotaLimit > 0 && userKey.QuotaUsed >= userKey.QuotaLimit {
		c.JSON(http.StatusForbidden, gin.H{"error": "配额已用尽"})
		c.Abort()
		return false
	}

	return true
}

// GetUserKeyID 从上下文获取用户密钥 ID
func GetUserKeyID(c *gin.Context) string {
	id, exists := c.Get("user_key_id")
	if !exists {
		return ""
	}
	return id.(string)
}

// GetUserKey 从上下文获取用户密钥
func GetUserKey(c *gin.Context) *model.UserKey {
	key, exists := c.Get("user_key")
	if !exists {
		return nil
	}
	return key.(*model.UserKey)
}
