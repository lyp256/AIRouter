package middleware

import (
	"net/http"
	"strings"
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

// APIKeyAuth API Key 认证中间件
// 支持两种认证方式：
// 1. API Key 认证: Authorization: Bearer <api_key>
// 2. JWT + KeyID 认证: Authorization: Bearer <jwt_token>, X-Key-ID: <key_id>
func APIKeyAuth(cfg APIKeyAuthConfig) gin.HandlerFunc {
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

		// 否则使用传统 API Key 认证
		handleAPIKeyAuth(c, cfg, token)
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

// handleAPIKeyAuth 处理传统 API Key 认证
func handleAPIKeyAuth(c *gin.Context, cfg APIKeyAuthConfig, apiKey string) {
	// 查询所有活跃的用户密钥
	var userKeys []model.UserKey
	result := cfg.DB.Where("status = ?", "active").Find(&userKeys)
	if result.Error != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "无效的 API Key"})
		c.Abort()
		return
	}

	// 遍历密钥进行匹配
	var matchedKey *model.UserKey
	for i := range userKeys {
		decryptedKey, err := cfg.Encryptor.Decrypt(userKeys[i].Key)
		if err != nil {
			continue // 解密失败，跳过
		}
		if decryptedKey == apiKey {
			matchedKey = &userKeys[i]
			break
		}
	}

	if matchedKey == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "无效的 API Key"})
		c.Abort()
		return
	}

	// 验证密钥状态、过期、配额
	if !validateUserKey(c, matchedKey) {
		return
	}

	// 设置上下文
	c.Set("user_id", matchedKey.UserID)
	c.Set("user_key_id", matchedKey.ID)
	c.Set("user_key", matchedKey)

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
