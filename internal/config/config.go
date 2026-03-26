package config

import (
	"time"

	"github.com/spf13/viper"
)

// Config 应用配置
type Config struct {
	Server    ServerConfig    `mapstructure:"server"`
	Database  DatabaseConfig  `mapstructure:"database"`
	Security  SecurityConfig  `mapstructure:"security"`
	Logging   LoggingConfig   `mapstructure:"logging"`
	RateLimit RateLimitConfig `mapstructure:"rate_limit"`
	Retry     RetryConfig     `mapstructure:"retry"`
	Admin     AdminConfig     `mapstructure:"admin"`
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

// DatabaseConfig 数据库配置
type DatabaseConfig struct {
	Type     string `mapstructure:"type"`
	Path     string `mapstructure:"path"`
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	Database string `mapstructure:"database"`
}

// SecurityConfig 安全配置
type SecurityConfig struct {
	EncryptionKey string        `mapstructure:"encryption_key"`
	JWTSecret     string        `mapstructure:"jwt_secret"`
	JWTExpire     time.Duration `mapstructure:"jwt_expire"`
	AdminToken    string        `mapstructure:"admin_token"`
}

// LoggingConfig 日志配置
type LoggingConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

// RateLimitConfig 限流配置
type RateLimitConfig struct {
	Enabled    bool `mapstructure:"enabled"`
	DefaultRPM int  `mapstructure:"default_rpm"`
}

// RetryConfig 重试配置
type RetryConfig struct {
	Enabled      bool          `mapstructure:"enabled"`        // 是否启用重试
	MaxAttempts  int           `mapstructure:"max_attempts"`   // 最大重试次数（包含首次请求）
	InitialWait  time.Duration `mapstructure:"initial_wait"`   // 初始等待时间
	MaxWait      time.Duration `mapstructure:"max_wait"`       // 最大等待时间
	Multiplier   float64       `mapstructure:"multiplier"`     // 退避乘数
	RetryOnCodes []int         `mapstructure:"retry_on_codes"` // 触发重试的状态码
}

// AdminConfig 管理员配置
type AdminConfig struct {
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	Email    string `mapstructure:"email"`
}

// Load 加载配置
func Load() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("./configs")
	viper.AddConfigPath(".")

	// 设置默认值
	viper.SetDefault("server.host", "0.0.0.0")
	viper.SetDefault("server.port", 8080)
	viper.SetDefault("database.type", "sqlite")
	viper.SetDefault("database.path", "./data/airouter.db")
	viper.SetDefault("logging.level", "info")
	viper.SetDefault("logging.format", "json")
	viper.SetDefault("rate_limit.enabled", true)
	viper.SetDefault("rate_limit.default_rpm", 60)
	viper.SetDefault("retry.enabled", true)
	viper.SetDefault("retry.max_attempts", 3)
	viper.SetDefault("retry.initial_wait", "1s")
	viper.SetDefault("retry.max_wait", "30s")
	viper.SetDefault("retry.multiplier", 2.0)
	viper.SetDefault("retry.retry_on_codes", []int{429, 500, 502, 503, 504})
	viper.SetDefault("security.jwt_expire", "24h")

	// 支持环境变量
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
