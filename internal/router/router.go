// Package router 提供路由注册功能
package router

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lyp256/airouter/internal/api/handler"
	"github.com/lyp256/airouter/internal/api/middleware"
	"github.com/lyp256/airouter/internal/config"
	"github.com/lyp256/airouter/internal/crypto"
	"github.com/lyp256/airouter/internal/static"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Handlers 封装所有 HTTP 处理器
type Handlers struct {
	Auth     *handler.AuthHandler
	Proxy    *handler.ProxyHandler
	Provider *handler.ProviderHandler
	Model    *handler.ModelHandler
	User     *handler.UserHandler
	Stats    *handler.StatsHandler
}

// Setup 创建并配置 Gin 路由器
func Setup(cfg *config.Config, db *gorm.DB, encryptor *crypto.Encryptor, logger *zap.Logger, handlers *Handlers) *gin.Engine {
	router := gin.New()
	router.Use(middleware.Logger(logger))
	router.Use(middleware.Recovery(logger))
	router.Use(middleware.CORS())

	// JWT 配置
	jwtCfg := middleware.JWTConfig{
		Secret: cfg.Security.JWTSecret,
		Expire: cfg.Security.JWTExpire,
	}

	// 认证选择器中间件
	authSelector := middleware.AuthSelector(middleware.AuthSelectorConfig{
		DB:        db,
		Encryptor: encryptor,
		JWTConfig: jwtCfg,
	})

	// 管理员权限中间件
	requireAdmin := middleware.RequireAdmin()

	// 限流器（仅对 /v1/* 生效）
	rateLimiter := middleware.NewRateLimiter(cfg.RateLimit.DefaultRPM)

	// 注册路由
	setupV1Routes(router, authSelector, rateLimiter, handlers.Proxy)
	setupAuthRoutes(router, authSelector, handlers.Auth)
	setupAdminRoutes(router, authSelector, requireAdmin, handlers)
	setupHealthRoute(router)
	setupStaticRoutes(router, logger)

	return router
}

// setupV1Routes 注册对外 API 路由
func setupV1Routes(router *gin.Engine, authSelector gin.HandlerFunc, rateLimiter *middleware.RateLimiter, proxy *handler.ProxyHandler) {
	v1 := router.Group("/v1")
	v1.Use(authSelector) // API Key 认证
	v1.Use(middleware.RateLimitByUserKey(rateLimiter))
	{
		v1.POST("/chat/completions", proxy.ChatCompletions)
		v1.POST("/completions", proxy.Completions)
		v1.POST("/messages", proxy.AnthropicMessages)
		v1.GET("/models", proxy.Models)
		v1.POST("/embeddings", proxy.Embeddings)
	}
}

// setupAuthRoutes 注册认证路由
func setupAuthRoutes(router *gin.Engine, authSelector gin.HandlerFunc, auth *handler.AuthHandler) {
	authGroup := router.Group("/api/admin/auth")
	{
		authGroup.POST("/login", auth.Login)
		authGroup.POST("/logout", auth.Logout)
		authGroup.GET("/me", authSelector, auth.GetCurrentUser)
		authGroup.PUT("/password", authSelector, auth.ChangePassword)
	}
}

// setupAdminRoutes 注册管理 API 路由
func setupAdminRoutes(router *gin.Engine, authSelector gin.HandlerFunc, requireAdmin gin.HandlerFunc, handlers *Handlers) {
	api := router.Group("/api/admin")
	api.Use(authSelector) // JWT 认证
	{
		// 供应商管理 - 仅管理员
		api.GET("/providers", requireAdmin, handlers.Provider.ListProviders)
		api.GET("/providers/:id", requireAdmin, handlers.Provider.GetProvider)
		api.POST("/providers", requireAdmin, handlers.Provider.CreateProvider)
		api.PUT("/providers/:id", requireAdmin, handlers.Provider.UpdateProvider)
		api.DELETE("/providers/:id", requireAdmin, handlers.Provider.DeleteProvider)

		// 供应商密钥管理 - 仅管理员
		api.GET("/providers/:id/keys", requireAdmin, handlers.Provider.ListProviderKeys)
		api.POST("/providers/:id/keys", requireAdmin, handlers.Provider.CreateProviderKey)
		api.PUT("/provider-keys/:key_id", requireAdmin, handlers.Provider.UpdateProviderKey)
		api.DELETE("/provider-keys/:key_id", requireAdmin, handlers.Provider.DeleteProviderKey)

		// 模型管理 - 仅管理员
		api.GET("/models", requireAdmin, handlers.Model.ListModels)
		api.GET("/models/:id", requireAdmin, handlers.Model.GetModel)
		api.POST("/models", requireAdmin, handlers.Model.CreateModel)
		api.PUT("/models/:id", requireAdmin, handlers.Model.UpdateModel)
		api.DELETE("/models/:id", requireAdmin, handlers.Model.DeleteModel)
		api.POST("/models/:id/toggle", requireAdmin, handlers.Model.ToggleModel)

		// 上游模型管理 - 仅管理员
		api.GET("/upstreams", requireAdmin, handlers.Model.ListUpstreams)
		api.GET("/upstreams/:id", requireAdmin, handlers.Model.GetUpstream)
		api.GET("/models/:id/upstreams", requireAdmin, handlers.Model.ListModelUpstreams)
		api.POST("/models/:id/upstreams", requireAdmin, handlers.Model.CreateUpstream)
		api.PUT("/upstreams/:id", requireAdmin, handlers.Model.UpdateUpstream)
		api.DELETE("/upstreams/:id", requireAdmin, handlers.Model.DeleteUpstream)
		api.POST("/upstreams/:id/toggle", requireAdmin, handlers.Model.ToggleUpstream)
		api.POST("/upstreams/:id/reset-status", requireAdmin, handlers.Model.ResetUpstreamStatus)
		api.POST("/upstreams/:id/test", requireAdmin, handlers.Model.TestUpstream)
		api.POST("/models/:id/test-upstreams", requireAdmin, handlers.Model.TestModelUpstreams)

		// 用户管理 - 仅管理员
		api.GET("/users", requireAdmin, handlers.User.ListUsers)
		api.GET("/users/:id", requireAdmin, handlers.User.GetUser)
		api.POST("/users", requireAdmin, handlers.User.CreateUser)
		api.PUT("/users/:id", requireAdmin, handlers.User.UpdateUser)
		api.DELETE("/users/:id", requireAdmin, handlers.User.DeleteUser)

		// 用户密钥管理 - 混合权限（权限检查在 handler 中实现）
		// 管理员：可操作所有用户密钥
		// 普通用户：只能操作自己的密钥
		api.GET("/user-keys", handlers.User.ListUserKeys)
		api.GET("/user-keys/me", handlers.User.GetMyKeys) // 获取当前用户密钥列表
		api.POST("/user-keys", handlers.User.CreateUserKey)
		api.PUT("/user-keys/:key_id", handlers.User.UpdateUserKey)
		api.DELETE("/user-keys/:key_id", handlers.User.DeleteUserKey)
		api.POST("/user-keys/:key_id/regenerate", handlers.User.RegenerateUserKey)

		// 统计分析 - 仅管理员
		api.GET("/stats/dashboard", requireAdmin, handlers.Stats.GetDashboard)
		api.GET("/stats/trend", requireAdmin, handlers.Stats.GetUsageTrend)
		api.GET("/stats/models", requireAdmin, handlers.Stats.GetModelStats)
		api.GET("/stats/users", requireAdmin, handlers.Stats.GetUserStats)
		api.GET("/stats/logs", requireAdmin, handlers.Stats.UsageLogList)
		api.GET("/stats/filter-options", requireAdmin, handlers.Stats.GetFilterOptions)
	}
}

// setupHealthRoute 注册健康检查路由
func setupHealthRoute(router *gin.Engine) {
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
}

// setupStaticRoutes 注册静态文件服务路由
func setupStaticRoutes(router *gin.Engine, logger *zap.Logger) {
	staticHandler, err := static.NewHandler()
	if err != nil {
		logger.Warn("静态文件处理器初始化失败，前端服务不可用", zap.Error(err))
		return
	}

	// SPA fallback：所有未匹配的路由返回 index.html
	router.NoRoute(func(c *gin.Context) {
		// 如果是 API 路径但未匹配到任何路由，返回 404
		if static.IsAPIPath(c.Request.URL.Path) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "API endpoint not found",
			})
			return
		}
		// 非 API 路径，返回 index.html（SPA fallback）
		staticHandler.ServeIndexHTML(c)
	})

	// 静态资源路由（带缓存）
	router.GET("/assets/*filepath", func(c *gin.Context) {
		staticHandler.ServeStatic(c)
	})

	// 其他静态文件路由
	router.GET("/favicon.svg", func(c *gin.Context) {
		staticHandler.ServeStatic(c)
	})
	router.GET("/icons.svg", func(c *gin.Context) {
		staticHandler.ServeStatic(c)
	})
}
