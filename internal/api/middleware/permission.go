package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RequireAdmin 要求管理员权限的中间件
// 必须在 JWTAuth 或 AuthSelector 中间件之后使用
func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		role := GetRole(c)
		if role == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
			c.Abort()
			return
		}

		if role != "admin" {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足，需要管理员权限"})
			c.Abort()
			return
		}

		c.Next()
	}
}

// IsAdmin 检查当前用户是否为管理员
func IsAdmin(c *gin.Context) bool {
	return GetRole(c) == "admin"
}
