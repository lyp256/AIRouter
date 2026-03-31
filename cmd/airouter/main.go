package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lyp256/airouter/internal/api/handler"
	"github.com/lyp256/airouter/internal/api/middleware"
	"github.com/lyp256/airouter/internal/config"
	"github.com/lyp256/airouter/internal/crypto"
	"github.com/lyp256/airouter/internal/service"
	"github.com/lyp256/airouter/internal/static"
	"github.com/lyp256/airouter/internal/store/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func main() {
	// 加载配置
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 初始化日志
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("初始化日志失败: %v", err)
	}
	defer logger.Sync()

	// 初始化数据库
	db, err := sqlite.Init(cfg.Database.Path)
	if err != nil {
		logger.Fatal("初始化数据库失败", zap.Error(err))
	}

	// 初始化加密器
	encryptor, err := crypto.NewEncryptor(cfg.Security.EncryptionKey)
	if err != nil {
		logger.Fatal("初始化加密器失败", zap.Error(err))
	}

	// 初始化管理员账户
	if err := handler.InitAdmin(db, cfg.Admin.Username, cfg.Admin.Password, cfg.Admin.Email); err != nil {
		logger.Error("初始化管理员账户失败", zap.Error(err))
	}

	// 初始化上游模型选择器
	upstreamSelector := service.NewUpstreamSelector(db, encryptor)

	// JWT 配置
	jwtCfg := middleware.JWTConfig{
		Secret: cfg.Security.JWTSecret,
		Expire: cfg.Security.JWTExpire,
	}

	// 创建处理器
	authHandler := handler.NewAuthHandler(db, jwtCfg)
	proxyHandler := handler.NewProxyHandler(db, logger, upstreamSelector)
	providerHandler := handler.NewProviderHandler(db, encryptor)
	modelHandler := handler.NewModelHandler(db, upstreamSelector)
	userHandler := handler.NewUserHandler(db, encryptor)
	statsHandler := handler.NewStatsHandler(db)

	// 创建统一路由器
	router := setupRouter(cfg, db, encryptor, logger, jwtCfg,
		authHandler, proxyHandler, providerHandler, modelHandler, userHandler, statsHandler)

	// 启动服务
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	server := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	// 启动服务
	go func() {
		logger.Info("启动服务", zap.String("addr", addr))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("服务启动失败", zap.Error(err))
		}
	}()

	logger.Info("AIRouter 启动成功", zap.String("addr", addr))

	// 优雅关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("正在关闭服务...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	server.Shutdown(ctx)

	logger.Info("服务已关闭")
}

// setupRouter 配置统一路由器
func setupRouter(cfg *config.Config, db *gorm.DB, encryptor *crypto.Encryptor, logger *zap.Logger,
	jwtCfg middleware.JWTConfig, authHandler *handler.AuthHandler, proxyHandler *handler.ProxyHandler,
	providerHandler *handler.ProviderHandler, modelHandler *handler.ModelHandler,
	userHandler *handler.UserHandler, statsHandler *handler.StatsHandler) *gin.Engine {

	router := gin.New()
	router.Use(middleware.Logger(logger))
	router.Use(middleware.Recovery(logger))
	router.Use(middleware.CORS())

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

	// ========== 对外 API 路由 ==========
	v1 := router.Group("/v1")
	v1.Use(authSelector) // API Key 认证
	v1.Use(middleware.RateLimitByUserKey(rateLimiter))
	{
		v1.POST("/chat/completions", proxyHandler.ChatCompletions)
		v1.POST("/completions", proxyHandler.Completions)
		v1.POST("/messages", proxyHandler.AnthropicMessages)
		v1.GET("/models", proxyHandler.Models)
		v1.POST("/embeddings", proxyHandler.Embeddings)
	}

	// ========== 管理 API 路由 ==========
	// 认证路由（部分无需认证）
	auth := router.Group("/api/admin/auth")
	{
		auth.POST("/login", authHandler.Login)
		auth.POST("/logout", authHandler.Logout)
		auth.GET("/me", authSelector, authHandler.GetCurrentUser)
		auth.PUT("/password", authSelector, authHandler.ChangePassword)
	}

	// 需要认证的管理路由
	api := router.Group("/api/admin")
	api.Use(authSelector) // JWT 认证
	{
		// 供应商管理 - 仅管理员
		api.GET("/providers", requireAdmin, providerHandler.ListProviders)
		api.GET("/providers/:id", requireAdmin, providerHandler.GetProvider)
		api.POST("/providers", requireAdmin, providerHandler.CreateProvider)
		api.PUT("/providers/:id", requireAdmin, providerHandler.UpdateProvider)
		api.DELETE("/providers/:id", requireAdmin, providerHandler.DeleteProvider)

		// 供应商密钥管理 - 仅管理员
		api.GET("/providers/:id/keys", requireAdmin, providerHandler.ListProviderKeys)
		api.POST("/providers/:id/keys", requireAdmin, providerHandler.CreateProviderKey)
		api.PUT("/provider-keys/:key_id", requireAdmin, providerHandler.UpdateProviderKey)
		api.DELETE("/provider-keys/:key_id", requireAdmin, providerHandler.DeleteProviderKey)

		// 模型管理 - 仅管理员
		api.GET("/models", requireAdmin, modelHandler.ListModels)
		api.GET("/models/:id", requireAdmin, modelHandler.GetModel)
		api.POST("/models", requireAdmin, modelHandler.CreateModel)
		api.PUT("/models/:id", requireAdmin, modelHandler.UpdateModel)
		api.DELETE("/models/:id", requireAdmin, modelHandler.DeleteModel)
		api.POST("/models/:id/toggle", requireAdmin, modelHandler.ToggleModel)

		// 上游模型管理 - 仅管理员
		api.GET("/upstreams", requireAdmin, modelHandler.ListUpstreams)
		api.GET("/upstreams/:id", requireAdmin, modelHandler.GetUpstream)
		api.GET("/models/:id/upstreams", requireAdmin, modelHandler.ListModelUpstreams)
		api.POST("/models/:id/upstreams", requireAdmin, modelHandler.CreateUpstream)
		api.PUT("/upstreams/:id", requireAdmin, modelHandler.UpdateUpstream)
		api.DELETE("/upstreams/:id", requireAdmin, modelHandler.DeleteUpstream)
		api.POST("/upstreams/:id/toggle", requireAdmin, modelHandler.ToggleUpstream)
		api.POST("/upstreams/:id/reset-status", requireAdmin, modelHandler.ResetUpstreamStatus)
		api.POST("/upstreams/:id/test", requireAdmin, modelHandler.TestUpstream)
		api.POST("/models/:id/test-upstreams", requireAdmin, modelHandler.TestModelUpstreams)

		// 用户管理 - 仅管理员
		api.GET("/users", requireAdmin, userHandler.ListUsers)
		api.GET("/users/:id", requireAdmin, userHandler.GetUser)
		api.POST("/users", requireAdmin, userHandler.CreateUser)
		api.PUT("/users/:id", requireAdmin, userHandler.UpdateUser)
		api.DELETE("/users/:id", requireAdmin, userHandler.DeleteUser)

		// 用户密钥管理 - 混合权限（权限检查在 handler 中实现）
		// 管理员：可操作所有用户密钥
		// 普通用户：只能操作自己的密钥
		api.GET("/user-keys", userHandler.ListUserKeys)
		api.GET("/user-keys/me", userHandler.GetMyKeys) // 获取当前用户密钥列表
		api.POST("/user-keys", userHandler.CreateUserKey)
		api.PUT("/user-keys/:key_id", userHandler.UpdateUserKey)
		api.DELETE("/user-keys/:key_id", userHandler.DeleteUserKey)
		api.POST("/user-keys/:key_id/regenerate", userHandler.RegenerateUserKey)

		// 统计分析 - 仅管理员
		api.GET("/stats/dashboard", requireAdmin, statsHandler.GetDashboard)
		api.GET("/stats/trend", requireAdmin, statsHandler.GetUsageTrend)
		api.GET("/stats/models", requireAdmin, statsHandler.GetModelStats)
		api.GET("/stats/users", requireAdmin, statsHandler.GetUserStats)
		api.GET("/stats/logs", requireAdmin, statsHandler.UsageLogList)
		api.GET("/stats/filter-options", requireAdmin, statsHandler.GetFilterOptions)
	}

	// ========== 健康检查 ==========
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// ========== 静态文件服务 ==========
	staticHandler, err := static.NewHandler()
	if err != nil {
		logger.Warn("静态文件处理器初始化失败，前端服务不可用", zap.Error(err))
	} else {
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

	return router
}
