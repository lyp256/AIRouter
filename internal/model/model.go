package model

import (
	"time"

	"gorm.io/gorm"
)

// User 用户模型
type User struct {
	ID        string         `gorm:"primaryKey;size:36" json:"id"`
	Username  string         `gorm:"uniqueIndex;size:64;not null" json:"username"`
	Email     string         `gorm:"uniqueIndex;size:128" json:"email"`
	Password  string         `gorm:"size:128;not null" json:"-"`           // 加密存储
	Role      string         `gorm:"size:16;default:user" json:"role"`     // admin, user
	Status    string         `gorm:"size:16;default:active" json:"status"` // active, disabled
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 指定表名
func (User) TableName() string {
	return "users"
}

// UserKey 用户密钥模型
type UserKey struct {
	ID          string         `gorm:"primaryKey;size:36" json:"id"`
	Name        string         `gorm:"size:128;not null" json:"name"`
	Key         string         `gorm:"size:256;not null" json:"-"` // 用户 API Key，加密存储
	UserID      string         `gorm:"index;size:36;not null" json:"user_id"`
	Permissions string         `gorm:"type:text" json:"permissions"` // JSON 数组：models:*, models:gpt-4
	RateLimit   int            `gorm:"default:60" json:"rate_limit"` // 请求/分钟
	QuotaLimit  int64          `gorm:"default:0" json:"quota_limit"` // 配额限制
	QuotaUsed   int64          `gorm:"default:0" json:"quota_used"`  // 已使用配额
	ExpiredAt   *time.Time     `json:"expired_at"`
	Status      string         `gorm:"size:16;default:active" json:"status"` // active, disabled, expired
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 指定表名
func (UserKey) TableName() string {
	return "user_keys"
}

// Provider 供应商模型
type Provider struct {
	ID          string         `gorm:"primaryKey;size:36" json:"id"`
	Name        string         `gorm:"uniqueIndex;size:64;not null" json:"name"` // openai, anthropic, azure, baidu, aliyun
	Type        string         `gorm:"size:32;not null" json:"type"`             // openai, anthropic, openai_compatible
	BaseURL     string         `gorm:"size:256" json:"base_url"`
	APIPath     string         `gorm:"size:256" json:"api_path"` // API 路径，留空使用默认路径
	Description string         `gorm:"size:512" json:"description"`
	Enabled     bool           `gorm:"default:true" json:"enabled"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 指定表名
func (Provider) TableName() string {
	return "providers"
}

// ProviderKey 供应商密钥模型
// 负载均衡参数已移至 Upstream
type ProviderKey struct {
	ID          string         `gorm:"primaryKey;size:36" json:"id"`
	ProviderID  string         `gorm:"index;size:36;not null" json:"provider_id"`
	Name        string         `gorm:"size:128;not null" json:"name"`        // 密钥名称/标识
	Key         string         `gorm:"size:256;not null" json:"-"`           // 加密存储的 API Key
	Status      string         `gorm:"size:16;default:active" json:"status"` // active, disabled, error
	QuotaLimit  int64          `gorm:"default:0" json:"quota_limit"`         // 配额限制
	QuotaUsed   int64          `gorm:"default:0" json:"quota_used"`          // 已使用配额
	LastUsedAt  *time.Time     `json:"last_used_at"`
	LastErrorAt *time.Time     `json:"last_error_at"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 指定表名
func (ProviderKey) TableName() string {
	return "provider_keys"
}

// Upstream 上游模型（新增）
// 对模型供应商模型调用的抽象，是负载均衡的基本单位
type Upstream struct {
	ID            string         `gorm:"primaryKey;size:36" json:"id"`
	ModelID       string         `gorm:"index;size:36;not null" json:"model_id"`                               // 关联对外模型
	ProviderID    string         `gorm:"index;size:36;not null" json:"provider_id"`                            // 关联供应商
	ProviderKeyID string         `gorm:"index;column:provider_key_id;size:36;not null" json:"provider_key_id"` // 关联供应商密钥
	ProviderModel string         `gorm:"size:64;not null" json:"provider_model"`                               // 供应商实际模型名
	Weight        int            `gorm:"default:1" json:"weight"`                                              // 权重（负载均衡用）
	Priority      int            `gorm:"default:0" json:"priority"`                                            // 优先级
	Status        string         `gorm:"size:16;default:active" json:"status"`                                 // active, disabled, error
	Enabled       bool           `gorm:"default:true" json:"enabled"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 指定表名
func (Upstream) TableName() string {
	return "upstreams"
}

// Model 对外大模型配置
// 移除了 ProviderID、ProviderModel、APIPath，通过 Upstream 关联
type Model struct {
	ID            string         `gorm:"primaryKey;size:36" json:"id"`
	Name          string         `gorm:"uniqueIndex;size:64;not null" json:"name"` // 模型名称（对外展示）
	Description   string         `gorm:"size:512" json:"description"`              // 模型描述
	InputPrice    float64        `gorm:"default:0" json:"input_price"`             // 输入价格（每1K token）
	OutputPrice   float64        `gorm:"default:0" json:"output_price"`            // 输出价格（每1K token）
	ContextWindow int            `gorm:"default:4096" json:"context_window"`       // 上下文窗口大小
	Enabled       bool           `gorm:"default:true" json:"enabled"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 指定表名
func (Model) TableName() string {
	return "models"
}

// UsageLog 使用日志模型
type UsageLog struct {
	ID            string    `gorm:"primaryKey;size:36" json:"id"`
	UserID        string    `gorm:"index;size:36" json:"user_id"`
	UserKeyID     string    `gorm:"index;size:36" json:"user_key_id"`
	UpstreamID    string    `gorm:"index;size:36" json:"upstream_id"`     // 关联上游模型
	ProviderKeyID string    `gorm:"index;size:36" json:"provider_key_id"` // 关联供应商密钥
	Model         string    `gorm:"index;size:64" json:"model"`           // 对外模型名称
	ProviderModel string    `gorm:"size:64" json:"provider_model"`        // 实际调用的供应商模型
	ProviderName  string    `gorm:"size:64" json:"provider_name"`         // 供应商名称
	InputTokens   int       `gorm:"default:0" json:"input_tokens"`
	OutputTokens  int       `gorm:"default:0" json:"output_tokens"`
	Cost          float64   `gorm:"default:0" json:"cost"`
	Latency       int       `gorm:"default:0" json:"latency"`    // 延迟(ms)
	Status        string    `gorm:"size:16;index" json:"status"` // success, error
	ErrorMessage  string    `gorm:"type:text" json:"error_message"`
	RequestID     string    `gorm:"size:36" json:"request_id"`
	CreatedAt     time.Time `gorm:"index" json:"created_at"`
}

// TableName 指定表名
func (UsageLog) TableName() string {
	return "usage_logs"
}
