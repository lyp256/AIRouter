package handler

import (
	"net/http"
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

// DashboardStats 仪表盘统计数据
type DashboardStats struct {
	TodayRequests int64   `json:"today_requests"`
	TodayTokens   int64   `json:"today_tokens"`
	TodayCost     float64 `json:"today_cost"`
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
	var costSum float64
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
	Date     string  `json:"date"`
	Requests int64   `json:"requests"`
	Tokens   int64   `json:"tokens"`
	Cost     float64 `json:"cost"`
}

// GetUsageTrend 获取使用趋势
func (h *StatsHandler) GetUsageTrend(c *gin.Context) {
	days := 7
	if d := c.Query("days"); d != "" {
		if parsed, _ := time.ParseDuration(d + "h"); parsed > 0 {
			days = int(parsed.Hours() / 24)
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
	Model    string  `json:"model"`
	Requests int64   `json:"requests"`
	Tokens   int64   `json:"tokens"`
	Cost     float64 `json:"cost"`
}

// GetModelStats 获取模型使用统计
func (h *StatsHandler) GetModelStats(c *gin.Context) {
	days := 7
	if d := c.Query("days"); d != "" {
		if parsed, _ := time.ParseDuration(d + "h"); parsed > 0 {
			days = int(parsed.Hours() / 24)
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

	c.JSON(http.StatusOK, gin.H{"data": stats})
}

// UserStats 用户统计
type UserStats struct {
	UserID     string  `json:"user_id"`
	Username   string  `json:"username"`
	Requests   int64   `json:"requests"`
	Tokens     int64   `json:"tokens"`
	Cost       float64 `json:"cost"`
	LastUsedAt string  `json:"last_used_at"`
}

// GetUserStats 获取用户使用统计
func (h *StatsHandler) GetUserStats(c *gin.Context) {
	days := 7
	if d := c.Query("days"); d != "" {
		if parsed, _ := time.ParseDuration(d + "h"); parsed > 0 {
			days = int(parsed.Hours() / 24)
		}
	}

	startDate := time.Now().AddDate(0, 0, -days)

	var stats []UserStats

	h.db.Model(&model.UsageLog{}).
		Select("user_id, COUNT(*) as requests, SUM(input_tokens + output_tokens) as tokens, SUM(cost) as cost, MAX(created_at) as last_used_at").
		Where("created_at >= ?", startDate).
		Group("user_id").
		Order("requests DESC").
		Limit(50).
		Scan(&stats)

	// 获取用户名
	for i := range stats {
		var user model.User
		if h.db.Select("username").First(&user, "id = ?", stats[i].UserID).Error == nil {
			stats[i].Username = user.Username
		}
	}

	c.JSON(http.StatusOK, gin.H{"data": stats})
}

// UsageLogList 使用日志列表
func (h *StatsHandler) UsageLogList(c *gin.Context) {
	page := 1
	pageSize := 20

	var total int64
	h.db.Model(&model.UsageLog{}).Count(&total)

	var logs []model.UsageLog
	offset := (page - 1) * pageSize

	query := h.db.Model(&model.UsageLog{}).Order("created_at DESC")

	// 筛选条件
	if userID := c.Query("user_id"); userID != "" {
		query = query.Where("user_id = ?", userID)
	}
	if model_name := c.Query("model"); model_name != "" {
		query = query.Where("model = ?", model_name)
	}
	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status)
	}

	query.Count(&total)
	query.Offset(offset).Limit(pageSize).Find(&logs)

	c.JSON(http.StatusOK, gin.H{
		"data":      logs,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}
