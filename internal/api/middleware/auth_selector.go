package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lyp256/airouter/internal/crypto"
	"gorm.io/gorm"
)

// AuthSelectorConfig 认证选择器配置
type AuthSelectorConfig struct {
	DB        *gorm.DB
	Encryptor *crypto.Encryptor
	JWTConfig JWTConfig
}

// AuthSelector 认证选择器中间件
// 根据请求路径自动选择认证方式：
// - /v1/* → API Key 认证（支持 JWT+KeyID 混合认证）
// - /api/admin/auth/login、/api/admin/auth/logout → 无需认证
// - /api/admin/* 其他 → JWT 认证
// - /health → 无需认证
func AuthSelector(cfg AuthSelectorConfig) gin.HandlerFunc {
	// 预创建认证中间件
	apiAuth := APIKeyAuth(APIKeyAuthConfig(cfg))
	jwtAuth := JWTAuth(cfg.JWTConfig)

	return func(c *gin.Context) {
		path := c.Request.URL.Path

		// 健康检查无需认证
		if path == "/health" {
			c.Next()
			return
		}

		// /v1/* 使用 API Key 认证（支持 JWT+KeyID）
		if strings.HasPrefix(path, "/v1/") {
			apiAuth(c)
			return
		}

		// /api/admin/auth/login 和 /api/admin/auth/logout 无需认证
		if path == "/api/admin/auth/login" || path == "/api/admin/auth/logout" {
			c.Next()
			return
		}

		// /api/admin/* 其他路由使用 JWT 认证
		if strings.HasPrefix(path, "/api/admin/") {
			jwtAuth(c)
			return
		}

		// 未知路径返回 404
		c.JSON(http.StatusNotFound, gin.H{"error": "路由不存在"})
		c.Abort()
	}
}
