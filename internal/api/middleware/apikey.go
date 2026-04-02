package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/lyp256/airouter/internal/cache"
	"github.com/lyp256/airouter/internal/crypto"
	"github.com/lyp256/airouter/internal/model"
	"gorm.io/gorm"
)

// APIKeyAuthConfig API Key 认证中间件配置
type APIKeyAuthConfig struct {
	DB        *gorm.DB
	Encryptor *crypto.Encryptor
	JWTConfig JWTConfig
	Cache     cache.Cache
}

// userKeyCacheData 用户密钥缓存数据
type userKeyCacheData struct {
	KeyID     string `json:"key_id"`
	UserID    string `json:"user_id"`
	KeyHash   string `json:"key_hash"`
	Decrypted string `json:"decrypted"`
}

// APIKeyAuthenticator API Key 认证器（带缓存）
type APIKeyAuthenticator struct {
	db        *gorm.DB
	encryptor *crypto.Encryptor
	cache     cache.Cache
}

// NewAPIKeyAuthenticator 创建 API Key 认证器
func NewAPIKeyAuthenticator(db *gorm.DB, encryptor *crypto.Encryptor, c cache.Cache) *APIKeyAuthenticator {
	return &APIKeyAuthenticator{
		db:        db,
		encryptor: encryptor,
		cache:     c,
	}
}

// Authenticate 通过 API Key 认证
func (a *APIKeyAuthenticator) Authenticate(apiKey string) (*model.UserKey, error) {
	ctx := context.Background()
	keyHash := sha256Hash(apiKey)

	// 通过缓存查找 keyHash 对应的 keyID
	var cacheData userKeyCacheData
	err := a.cache.Once(ctx, "user_key:hash:"+keyHash, &cacheData, 5*time.Minute, func() (interface{}, error) {
		// 缓存未命中，从数据库全量加载活跃的 UserKey 并建立索引
		return a.loadKeyByHash(keyHash)
	})
	if err != nil {
		if err == cache.ErrCacheMiss {
			return nil, nil // 未找到
		}
		return nil, err
	}

	// 验证原始 key 是否匹配（双重验证）
	if cacheData.Decrypted != apiKey {
		return nil, nil
	}

	// 获取最新的用户密钥信息（短缓存，包含配额）
	var userKey model.UserKey
	err = a.cache.Once(ctx, "user_key:id:"+cacheData.KeyID, &userKey, 1*time.Minute, func() (interface{}, error) {
		var uk model.UserKey
		if err := a.db.First(&uk, "id = ? AND status = ?", cacheData.KeyID, "active").Error; err != nil {
			return nil, err
		}
		return uk, nil
	})
	if err != nil {
		if err == cache.ErrCacheMiss {
			return nil, nil
		}
		return nil, err
	}

	return &userKey, nil
}

// loadKeyByHash 从数据库加载所有活跃密钥，找到匹配 hash 的密钥
func (a *APIKeyAuthenticator) loadKeyByHash(targetHash string) (*userKeyCacheData, error) {
	var userKeys []model.UserKey
	if err := a.db.Where("status = ?", "active").Find(&userKeys).Error; err != nil {
		return nil, err
	}

	// 同时缓存所有密钥的 hash 映射
	for i := range userKeys {
		decryptedKey, err := a.encryptor.Decrypt(userKeys[i].Key)
		if err != nil {
			continue
		}

		keyHash := sha256Hash(decryptedKey)
		data := &userKeyCacheData{
			KeyID:     userKeys[i].ID,
			UserID:    userKeys[i].UserID,
			KeyHash:   keyHash,
			Decrypted: decryptedKey,
		}

		// 缓存每个密钥的 hash 映射
		_ = a.cache.Set(context.Background(), "user_key:hash:"+keyHash, data, 5*time.Minute)
		// 缓存 ID 到 keyID 的映射（用于快速失效）
		_ = a.cache.Set(context.Background(), "user_key:id2hash:"+userKeys[i].ID, keyHash, 5*time.Minute)

		if keyHash == targetHash {
			return data, nil
		}
	}

	return nil, cache.ErrCacheMiss
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
	authenticator := NewAPIKeyAuthenticator(cfg.DB, cfg.Encryptor, cfg.Cache)

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

// InvalidateUserKeyCache 使用户密钥缓存失效
func InvalidateUserKeyCache(c cache.Cache, keyID string) {
	ctx := context.Background()
	// 通过 ID 找到 hash，然后清理两个缓存
	var hashData struct {
		Hash string `json:"hash"`
	}
	if err := c.Get(ctx, "user_key:id2hash:"+keyID, &hashData); err == nil {
		_ = c.Delete(ctx, "user_key:hash:"+hashData.Hash)
	}
	_ = c.Delete(ctx, "user_key:id2hash:"+keyID)
	_ = c.Delete(ctx, "user_key:id:"+keyID)
}
