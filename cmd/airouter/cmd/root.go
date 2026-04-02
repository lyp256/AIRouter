// Package cmd 提供 CLI 命令
package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lyp256/airouter/internal/api/handler"
	"github.com/lyp256/airouter/internal/api/middleware"
	"github.com/lyp256/airouter/internal/cache"
	"github.com/lyp256/airouter/internal/config"
	"github.com/lyp256/airouter/internal/crypto"
	"github.com/lyp256/airouter/internal/router"
	"github.com/lyp256/airouter/internal/service"
	"github.com/lyp256/airouter/internal/store/sqlite"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var (
	// 配置文件路径
	cfgFile string
	// 日志器
	logger *zap.Logger
	// 版本信息
	version   string
	buildTime string
)

// SetVersion 设置版本信息
func SetVersion(v, bt string) {
	version = v
	buildTime = bt
}

// rootCmd 根命令
var rootCmd = &cobra.Command{
	Use:   "airouter",
	Short: "AIRouter - 大模型 API 统一代理系统",
	Long: `AIRouter 是一个大模型 API 统一代理系统，提供多供应商统一代理、
密钥管理、负载均衡和使用统计。`,
}

// serveCmd 启动服务命令
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "启动服务",
	Long:  `启动 AIRouter 服务，监听并处理 API 请求。`,
	Run:   runServe,
}

// versionCmd 版本命令
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "显示版本信息",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("AIRouter %s\n", version)
		fmt.Printf("构建时间: %s\n", buildTime)
	},
}

// Execute 执行命令
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	// 全局持久化标志
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "配置文件路径")

	// 添加子命令
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(versionCmd)
}

// runServe 启动服务
func runServe(cmd *cobra.Command, args []string) {
	// 初始化日志
	var err error
	logger, err = zap.NewProduction()
	if err != nil {
		fmt.Fprintln(os.Stderr, "初始化日志失败:", err)
		os.Exit(1)
	}
	defer func() { _ = logger.Sync() }()

	// 加载配置
	cfg, err := config.Load(cfgFile)
	if err != nil {
		logger.Fatal("加载配置失败", zap.Error(err))
	}

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

	// 初始化缓存
	cacheInstance, err := cache.New(&cfg.Cache)
	if err != nil {
		logger.Fatal("初始化缓存失败", zap.Error(err))
	}

	// 初始化管理员账户
	if err := handler.InitAdmin(db, cfg.Admin.Username, cfg.Admin.Password, cfg.Admin.Email); err != nil {
		logger.Error("初始化管理员账户失败", zap.Error(err))
	}

	// 初始化上游模型选择器
	upstreamSelector := service.NewUpstreamSelector(db, encryptor, cacheInstance)

	// 创建处理器
	handlers := &router.Handlers{
		Auth:     handler.NewAuthHandler(db, middleware.JWTConfig{Secret: cfg.Security.JWTSecret, Expire: cfg.Security.JWTExpire}),
		Proxy:    handler.NewProxyHandler(db, logger, upstreamSelector, &cfg.Retry, cacheInstance),
		Provider: handler.NewProviderHandler(db, encryptor),
		Model:    handler.NewModelHandler(db, upstreamSelector, cacheInstance),
		User:     handler.NewUserHandler(db, encryptor),
		Stats:    handler.NewStatsHandler(db),
	}

	// 创建路由器
	routerEngine := router.Setup(cfg, db, encryptor, logger, cacheInstance, handlers)

	// 启动服务
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	server := &http.Server{
		Addr:    addr,
		Handler: routerEngine,
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

	_ = server.Shutdown(ctx)

	logger.Info("服务已关闭")
}
