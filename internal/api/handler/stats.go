package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lyp256/airouter/internal/model"
	"gorm.io/gorm"
)

// StatsHandler 统计处理器
type StatsHandler struct {
	db *gorm.DB
}

// NewStatsHandler 创建统计处理器
func NewStatsHandler(db *gorm.DB) *StatsHandler {
	return &StatsHandler{db: db}
}

// UsageLogResponse 日志列表响应（包含关联查询字段）
type UsageLogResponse struct {
	ID                 string `json:"id"`
	UserID             string `json:"user_id"`
	Username           string `json:"username"`
	UserKeyID          string `json:"user_key_id"`
	UpstreamID         string `json:"upstream_id"`
	ProviderKeyID      string `json:"provider_key_id"`
	Model              string `json:"model"`
	ProviderType       string `json:"provider_type"`
	ProviderModel      string `json:"provider_model"`
	ProviderName       string `json:"provider_name"`
	InputTokens        int    `json:"input_tokens"`
	OutputTokens       int    `json:"output_tokens"`
	Cost               int64  `json:"cost"`
	Latency            int    `json:"latency"`
	FirstTokenLatency  int    `json:"first_token_latency"`
	TotalDuration      int    `json:"total_duration"`
	Status             string `json:"status"`
	UpstreamStatusCode int    `json:"upstream_status_code"`
	ErrorMessage       string `json:"error_message"`
	RequestID          string `json:"request_id"`
	CreatedAt          string `json:"created_at"`
}

// DashboardStats 仪表盘统计数据
type DashboardStats struct {
	TodayRequests int64   `json:"today_requests"`
	TodayTokens   int64   `json:"today_tokens"`
	TodayCost     int64   `json:"today_cost"` // 今日消费（纳 BU）
	ActiveUsers   int64   `json:"active_users"`
	ActiveKeys    int64   `json:"active_keys"`
	SuccessRate   float64 `json:"success_rate"`
}

// GetDashboard 获取仪表盘统计
func (h *StatsHandler) GetDashboard(c *gin.Context) {
	today := time.Now().Format("2006-01-02")
	todayStart, _ := time.Parse("2006-01-02", today)

	var stats DashboardStats

	// 今日请求数
	h.db.Model(&model.UsageLog{}).
		Where("created_at >= ?", todayStart).
		Count(&stats.TodayRequests)

	// 今日 Token 数
	var tokenSum struct {
		Input  int64
		Output int64
	}
	h.db.Model(&model.UsageLog{}).
		Where("created_at >= ?", todayStart).
		Select("COALESCE(SUM(input_tokens), 0) as input, COALESCE(SUM(output_tokens), 0) as output").
		Scan(&tokenSum)
	stats.TodayTokens = tokenSum.Input + tokenSum.Output

	// 今日消费
	var costSum int64
	h.db.Model(&model.UsageLog{}).
		Where("created_at >= ?", todayStart).
		Select("COALESCE(SUM(cost), 0)").
		Scan(&costSum)
	stats.TodayCost = costSum

	// 活跃用户数
	h.db.Model(&model.User{}).
		Where("status = ?", "active").
		Count(&stats.ActiveUsers)

	// 活跃密钥数
	h.db.Model(&model.UserKey{}).
		Where("status = ?", "active").
		Count(&stats.ActiveKeys)

	// 成功率
	var totalRequests, successRequests int64
	h.db.Model(&model.UsageLog{}).Count(&totalRequests)
	h.db.Model(&model.UsageLog{}).
		Where("status = ?", "success").
		Count(&successRequests)
	if totalRequests > 0 {
		stats.SuccessRate = float64(successRequests) / float64(totalRequests) * 100
	}

	c.JSON(http.StatusOK, gin.H{"data": stats})
}

// UsageTrend 使用趋势
type UsageTrend struct {
	Date     string `json:"date"`
	Requests int64  `json:"requests"`
	Tokens   int64  `json:"tokens"`
	Cost     int64  `json:"cost"` // 费用（纳 BU）
}

// GetUsageTrend 获取使用趋势
func (h *StatsHandler) GetUsageTrend(c *gin.Context) {
	days := 7
	if d := c.Query("days"); d != "" {
		if parsed, err := strconv.Atoi(d); err == nil && parsed > 0 {
			days = parsed
		}
	}

	if days > 30 {
		days = 30
	}

	trends := make([]UsageTrend, 0, days)

	for i := days - 1; i >= 0; i-- {
		date := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		dateStart, _ := time.Parse("2006-01-02", date)
		dateEnd := dateStart.Add(24 * time.Hour)

		var trend UsageTrend
		trend.Date = date

		// 请求数
		h.db.Model(&model.UsageLog{}).
			Where("created_at >= ? AND created_at < ?", dateStart, dateEnd).
			Count(&trend.Requests)

		// Token 数
		var tokenSum struct {
			Total int64
		}
		h.db.Model(&model.UsageLog{}).
			Where("created_at >= ? AND created_at < ?", dateStart, dateEnd).
			Select("COALESCE(SUM(input_tokens + output_tokens), 0) as total").
			Scan(&tokenSum)
		trend.Tokens = tokenSum.Total

		// 成本
		h.db.Model(&model.UsageLog{}).
			Where("created_at >= ? AND created_at < ?", dateStart, dateEnd).
			Select("COALESCE(SUM(cost), 0)").
			Scan(&trend.Cost)

		trends = append(trends, trend)
	}

	c.JSON(http.StatusOK, gin.H{"data": trends})
}

// ModelStats 模型统计
type ModelStats struct {
	Model    string `json:"model"`
	Requests int64  `json:"requests"`
	Tokens   int64  `json:"tokens"`
	Cost     int64  `json:"cost"` // 费用（纳 BU）
}

// GetModelStats 获取模型使用统计
func (h *StatsHandler) GetModelStats(c *gin.Context) {
	days := 7
	if d := c.Query("days"); d != "" {
		if parsed, err := strconv.Atoi(d); err == nil && parsed > 0 {
			days = parsed
		}
	}

	startDate := time.Now().AddDate(0, 0, -days)

	var stats []ModelStats
	h.db.Model(&model.UsageLog{}).
		Select("model, COUNT(*) as requests, SUM(input_tokens + output_tokens) as tokens, SUM(cost) as cost").
		Where("created_at >= ?", startDate).
		Group("model").
		Order("requests DESC").
		Limit(20).
		Scan(&stats)
	if stats == nil {
		stats = []ModelStats{}
	}

	c.JSON(http.StatusOK, gin.H{"data": stats})
}

// UserStats 用户统计
type UserStats struct {
	UserID     string `json:"user_id"`
	Username   string `json:"username"`
	Requests   int64  `json:"requests"`
	Tokens     int64  `json:"tokens"`
	Cost       int64  `json:"cost"` // 费用（纳 BU）
	LastUsedAt string `json:"last_used_at"`
}

// GetUserStats 获取用户使用统计
func (h *StatsHandler) GetUserStats(c *gin.Context) {
	days := 7
	if d := c.Query("days"); d != "" {
		if parsed, err := strconv.Atoi(d); err == nil && parsed > 0 {
			days = parsed
		}
	}

	startDate := time.Now().AddDate(0, 0, -days)

	var stats []UserStats

	// 使用 JOIN 一次性获取用户名
	h.db.Table("usage_logs ul").
		Select(`ul.user_id, u.username, COUNT(*) as requests,
				SUM(ul.input_tokens + ul.output_tokens) as tokens,
				SUM(ul.cost) as cost, MAX(ul.created_at) as last_used_at`).
		Joins("LEFT JOIN users u ON ul.user_id = u.id").
		Where("ul.created_at >= ?", startDate).
		Group("ul.user_id, u.username").
		Order("requests DESC").
		Limit(50).
		Scan(&stats)
	if stats == nil {
		stats = []UserStats{}
	}

	c.JSON(http.StatusOK, gin.H{"data": stats})
}

// UsageLogList 使用日志列表
func (h *StatsHandler) UsageLogList(c *gin.Context) {
	page := 1
	pageSize := 20

	if p := c.Query("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			page = v
		}
	}
	if ps := c.Query("page_size"); ps != "" {
		if v, err := strconv.Atoi(ps); err == nil && v > 0 && v <= 100 {
			pageSize = v
		}
	}

	offset := (page - 1) * pageSize

	// 筛选条件构建（用于 count 和 data 两次查询）
	applyFilters := func(q *gorm.DB) *gorm.DB {
		if userID := c.Query("user_id"); userID != "" {
			q = q.Where("ul.user_id = ?", userID)
		}
		if modelName := c.Query("model"); modelName != "" {
			q = q.Where("ul.model = ?", modelName)
		}
		if providerType := c.Query("provider_type"); providerType != "" {
			q = q.Where("m.provider_type = ?", providerType)
		}
		if providerName := c.Query("provider_name"); providerName != "" {
			q = q.Where("p.name = ?", providerName)
		}
		if providerKeyID := c.Query("provider_key_id"); providerKeyID != "" {
			q = q.Where("ul.provider_key_id = ?", providerKeyID)
		}
		if status := c.Query("status"); status != "" {
			q = q.Where("ul.status = ?", status)
		}
		return q
	}

	// 基础 JOIN（筛选所需的关联表）
	filterBase := func() *gorm.DB {
		return h.db.Table("usage_logs ul").
			Joins("LEFT JOIN upstreams up ON ul.upstream_id = up.id").
			Joins("LEFT JOIN models m ON up.model_id = m.id").
			Joins("LEFT JOIN providers p ON up.provider_id = p.id")
	}

	// 计算总数（独立查询，避免 Select 干扰 Count）
	var total int64
	applyFilters(filterBase()).Count(&total)

	// 分页数据查询
	var logs []UsageLogResponse
	applyFilters(filterBase()).
		Select(`ul.id, ul.user_id, u.username, ul.user_key_id, ul.upstream_id,
				ul.provider_key_id, ul.model, m.provider_type, up.provider_model,
				p.name as provider_name, ul.input_tokens, ul.output_tokens, ul.cost,
				ul.latency, ul.first_token_latency, ul.total_duration, ul.status,
				ul.upstream_status_code, ul.error_message,
				ul.request_id, ul.created_at`).
		Joins("LEFT JOIN users u ON ul.user_id = u.id").
		Order("ul.created_at DESC").
		Offset(offset).
		Limit(pageSize).
		Scan(&logs)
	if logs == nil {
		logs = []UsageLogResponse{}
	}

	c.JSON(http.StatusOK, gin.H{
		"data":      logs,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// ProviderKeyOption 供应商密钥筛选选项
type ProviderKeyOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// FilterOptions 筛选选项
type FilterOptions struct {
	Models        []string            `json:"models"`
	ProviderTypes []string            `json:"provider_types"`
	ProviderNames []string            `json:"provider_names"`
	ProviderKeys  []ProviderKeyOption `json:"provider_keys"`
	Statuses      []string            `json:"statuses"`
}

// GetFilterOptions 获取筛选选项
func (h *StatsHandler) GetFilterOptions(c *gin.Context) {
	var opts FilterOptions

	// 获取所有模型（从日志表直接获取）
	h.db.Table("usage_logs ul").
		Distinct("ul.model").
		Where("ul.model != ''").
		Pluck("model", &opts.Models)

	// 获取所有协议类型（通过 JOIN 从 models 表获取）
	h.db.Table("usage_logs ul").
		Joins("LEFT JOIN upstreams up ON ul.upstream_id = up.id").
		Joins("LEFT JOIN models m ON up.model_id = m.id").
		Distinct("m.provider_type").
		Where("m.provider_type IS NOT NULL AND m.provider_type != ''").
		Pluck("provider_type", &opts.ProviderTypes)

	// 获取所有厂商（通过 JOIN 从 providers 表获取）
	h.db.Table("usage_logs ul").
		Joins("LEFT JOIN upstreams up ON ul.upstream_id = up.id").
		Joins("LEFT JOIN providers p ON up.provider_id = p.id").
		Distinct("p.name").
		Where("p.name IS NOT NULL AND p.name != ''").
		Pluck("name", &opts.ProviderNames)

	// 获取所有供应商密钥（从日志中实际使用过的）
	h.db.Table("usage_logs ul").
		Joins("LEFT JOIN provider_keys pk ON ul.provider_key_id = pk.id").
		Select("DISTINCT pk.id, pk.name").
		Where("pk.id IS NOT NULL AND pk.name IS NOT NULL AND pk.name != ''").
		Scan(&opts.ProviderKeys)

	// 确保 slice 不为 nil（避免 JSON 序列化为 null）
	if opts.Models == nil {
		opts.Models = []string{}
	}
	if opts.ProviderTypes == nil {
		opts.ProviderTypes = []string{}
	}
	if opts.ProviderNames == nil {
		opts.ProviderNames = []string{}
	}
	if opts.ProviderKeys == nil {
		opts.ProviderKeys = []ProviderKeyOption{}
	}
	opts.Statuses = []string{"success", "error"}

	c.JSON(http.StatusOK, gin.H{"data": opts})
}
