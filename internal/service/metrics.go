package service

import (
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// Metrics 指标收集器
type Metrics struct {
	// 请求计数
	RequestsTotal *CounterVec
	// 请求延迟
	RequestDuration *HistogramVec
	// 活跃请求
	ActiveRequests *Gauge
	// Token 使用量
	TokensTotal *CounterVec
	// 费用
	CostTotal *CounterVec
	// 错误计数
	ErrorsTotal *CounterVec
	// 供应商健康状态
	ProviderHealth *GaugeVec
	// 密钥状态
	KeyStatus *GaugeVec
	// 配额使用
	QuotaUsage *GaugeVec
}

// CounterVec 计数器向量
type CounterVec struct {
	name   string
	labels []string
	data   map[string]int64
	mu     sync.RWMutex
}

// NewCounterVec 创建计数器向量
func NewCounterVec(name string, labels []string) *CounterVec {
	return &CounterVec{
		name:   name,
		labels: labels,
		data:   make(map[string]int64),
	}
}

// Inc 增加计数
func (c *CounterVec) Inc(labelValues ...string) {
	key := makeKey(labelValues)
	c.mu.Lock()
	c.data[key]++
	c.mu.Unlock()
}

// Add 增加指定值
func (c *CounterVec) Add(value int64, labelValues ...string) {
	key := makeKey(labelValues)
	c.mu.Lock()
	c.data[key] += value
	c.mu.Unlock()
}

// Get 获取值
func (c *CounterVec) Get(labelValues ...string) int64 {
	key := makeKey(labelValues)
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.data[key]
}

// GetAll 获取所有值
func (c *CounterVec) GetAll() map[string]int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make(map[string]int64)
	for k, v := range c.data {
		result[k] = v
	}
	return result
}

// HistogramVec 直方图向量
type HistogramVec struct {
	name    string
	labels  []string
	buckets []float64
	data    map[string]*Histogram
	mu      sync.RWMutex
}

// Histogram 直方图
type Histogram struct {
	count int64
	sum   float64
	// buckets: bucket boundary -> count
	bucketCounts map[float64]int64
}

// NewHistogramVec 创建直方图向量
func NewHistogramVec(name string, labels []string, buckets []float64) *HistogramVec {
	return &HistogramVec{
		name:    name,
		labels:  labels,
		buckets: buckets,
		data:    make(map[string]*Histogram),
	}
}

// Observe 观察值
func (h *HistogramVec) Observe(value float64, labelValues ...string) {
	key := makeKey(labelValues)
	h.mu.Lock()
	defer h.mu.Unlock()

	hist, exists := h.data[key]
	if !exists {
		hist = &Histogram{
			bucketCounts: make(map[float64]int64),
		}
		for _, b := range h.buckets {
			hist.bucketCounts[b] = 0
		}
		h.data[key] = hist
	}

	hist.count++
	hist.sum += value

	// 更新桶计数
	for _, bucket := range h.buckets {
		if value <= bucket {
			hist.bucketCounts[bucket]++
		}
	}
}

// GetCount 获取计数
func (h *HistogramVec) GetCount(labelValues ...string) int64 {
	key := makeKey(labelValues)
	h.mu.RLock()
	defer h.mu.RUnlock()
	if hist, exists := h.data[key]; exists {
		return hist.count
	}
	return 0
}

// GetSum 获取总和
func (h *HistogramVec) GetSum(labelValues ...string) float64 {
	key := makeKey(labelValues)
	h.mu.RLock()
	defer h.mu.RUnlock()
	if hist, exists := h.data[key]; exists {
		return hist.sum
	}
	return 0
}

// Gauge 仪表
type Gauge struct {
	name string
	data map[string]float64
	mu   sync.RWMutex
}

// NewGauge 创建仪表
func NewGauge(name string) *Gauge {
	return &Gauge{
		name: name,
		data: make(map[string]float64),
	}
}

// Set 设置值
func (g *Gauge) Set(value float64, labelValues ...string) {
	key := makeKey(labelValues)
	g.mu.Lock()
	g.data[key] = value
	g.mu.Unlock()
}

// Get 获取值
func (g *Gauge) Get(labelValues ...string) float64 {
	key := makeKey(labelValues)
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.data[key]
}

// GetAll 获取所有值
func (g *Gauge) GetAll() map[string]float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()
	result := make(map[string]float64)
	for k, v := range g.data {
		result[k] = v
	}
	return result
}

// GaugeVec 仪表向量
type GaugeVec struct {
	name   string
	labels []string
	data   map[string]float64
	mu     sync.RWMutex
}

// NewGaugeVec 创建仪表向量
func NewGaugeVec(name string, labels []string) *GaugeVec {
	return &GaugeVec{
		name:   name,
		labels: labels,
		data:   make(map[string]float64),
	}
}

// Set 设置值
func (g *GaugeVec) Set(value float64, labelValues ...string) {
	key := makeKey(labelValues)
	g.mu.Lock()
	g.data[key] = value
	g.mu.Unlock()
}

// Get 获取值
func (g *GaugeVec) Get(labelValues ...string) float64 {
	key := makeKey(labelValues)
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.data[key]
}

// GetAll 获取所有值
func (g *GaugeVec) GetAll() map[string]float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()
	result := make(map[string]float64)
	for k, v := range g.data {
		result[k] = v
	}
	return result
}

// makeKey 生成键
func makeKey(labelValues []string) string {
	key := ""
	for i, v := range labelValues {
		if i > 0 {
			key += "|"
		}
		key += v
	}
	return key
}

// NewMetrics 创建指标收集器
func NewMetrics() *Metrics {
	return &Metrics{
		RequestsTotal: NewCounterVec("airouter_requests_total", []string{"method", "path", "status"}),
		RequestDuration: NewHistogramVec("airouter_request_duration_seconds", []string{"method", "path"},
			[]float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}),
		ActiveRequests: NewGauge("airouter_active_requests"),
		TokensTotal:    NewCounterVec("airouter_tokens_total", []string{"type", "model", "provider"}),
		CostTotal:      NewCounterVec("airouter_cost_total", []string{"model", "provider"}),
		ErrorsTotal:    NewCounterVec("airouter_errors_total", []string{"type", "model", "provider"}),
		ProviderHealth: NewGaugeVec("airouter_provider_health", []string{"provider"}),
		KeyStatus:      NewGaugeVec("airouter_key_status", []string{"provider", "key_id", "status"}),
		QuotaUsage:     NewGaugeVec("airouter_quota_usage", []string{"type", "id"}),
	}
}

// MetricsMiddleware 指标中间件
func MetricsMiddleware(metrics *Metrics) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 跳过指标端点本身
		if c.Request.URL.Path == "/metrics" {
			c.Next()
			return
		}

		// 增加活跃请求
		metrics.ActiveRequests.Set(metrics.ActiveRequests.Get() + 1)

		start := time.Now()

		c.Next()

		// 减少活跃请求
		metrics.ActiveRequests.Set(metrics.ActiveRequests.Get() - 1)

		// 记录请求
		duration := time.Since(start).Seconds()
		status := strconv.Itoa(c.Writer.Status())

		metrics.RequestsTotal.Inc(c.Request.Method, c.Request.URL.Path, status)
		metrics.RequestDuration.Observe(duration, c.Request.Method, c.Request.URL.Path)
	}
}

// ExportPrometheus 导出 Prometheus 格式
func (m *Metrics) ExportPrometheus() string {
	var result string

	// 请求总数
	result += "# HELP airouter_requests_total 请求总数\n"
	result += "# TYPE airouter_requests_total counter\n"
	for k, v := range m.RequestsTotal.GetAll() {
		result += "airouter_requests_total{" + k + "} " + strconv.FormatInt(v, 10) + "\n"
	}

	// 请求延迟
	result += "\n# HELP airouter_request_duration_seconds 请求延迟\n"
	result += "# TYPE airouter_request_duration_seconds histogram\n"
	for k, v := range m.RequestDuration.data {
		result += "airouter_request_duration_seconds_count{" + k + "} " + strconv.FormatInt(v.count, 10) + "\n"
		result += "airouter_request_duration_seconds_sum{" + k + "} " + strconv.FormatFloat(v.sum, 'f', 6, 64) + "\n"
	}

	// 活跃请求
	result += "\n# HELP airouter_active_requests 活跃请求数\n"
	result += "# TYPE airouter_active_requests gauge\n"
	result += "airouter_active_requests " + strconv.FormatFloat(m.ActiveRequests.Get(), 'f', 0, 64) + "\n"

	// Token 使用量
	result += "\n# HELP airouter_tokens_total Token 使用总量\n"
	result += "# TYPE airouter_tokens_total counter\n"
	for k, v := range m.TokensTotal.GetAll() {
		result += "airouter_tokens_total{" + k + "} " + strconv.FormatInt(v, 10) + "\n"
	}

	// 费用
	result += "\n# HELP airouter_cost_total 总费用\n"
	result += "# TYPE airouter_cost_total counter\n"
	for k, v := range m.CostTotal.GetAll() {
		result += "airouter_cost_total{" + k + "} " + strconv.FormatInt(v, 10) + "\n"
	}

	// 错误计数
	result += "\n# HELP airouter_errors_total 错误总数\n"
	result += "# TYPE airouter_errors_total counter\n"
	for k, v := range m.ErrorsTotal.GetAll() {
		result += "airouter_errors_total{" + k + "} " + strconv.FormatInt(v, 10) + "\n"
	}

	// 供应商健康状态
	result += "\n# HELP airouter_provider_health 供应商健康状态 (1=healthy, 0=unhealthy)\n"
	result += "# TYPE airouter_provider_health gauge\n"
	for k, v := range m.ProviderHealth.GetAll() {
		result += "airouter_provider_health{" + k + "} " + strconv.FormatFloat(v, 'f', 0, 64) + "\n"
	}

	// 密钥状态
	result += "\n# HELP airouter_key_status 密钥状态 (1=active, 0=inactive)\n"
	result += "# TYPE airouter_key_status gauge\n"
	for k, v := range m.KeyStatus.GetAll() {
		result += "airouter_key_status{" + k + "} " + strconv.FormatFloat(v, 'f', 0, 64) + "\n"
	}

	// 配额使用
	result += "\n# HELP airouter_quota_usage 配额使用量\n"
	result += "# TYPE airouter_quota_usage gauge\n"
	for k, v := range m.QuotaUsage.GetAll() {
		result += "airouter_quota_usage{" + k + "} " + strconv.FormatFloat(v, 'f', 0, 64) + "\n"
	}

	return result
}

// RecordTokenUsage 记录 Token 使用量
func (m *Metrics) RecordTokenUsage(tokenType, model, provider string, count int64) {
	m.TokensTotal.Add(count, tokenType, model, provider)
}

// RecordCost 记录费用（单位：纳 BU）
func (m *Metrics) RecordCost(model, provider string, cost int64) {
	m.CostTotal.Add(cost, model, provider)
}

// RecordError 记录错误
func (m *Metrics) RecordError(errType, model, provider string) {
	m.ErrorsTotal.Inc(errType, model, provider)
}

// SetProviderHealth 设置供应商健康状态
func (m *Metrics) SetProviderHealth(provider string, healthy bool) {
	var value float64
	if healthy {
		value = 1
	}
	m.ProviderHealth.Set(value, provider)
}

// SetKeyStatus 设置密钥状态
func (m *Metrics) SetKeyStatus(provider, keyID, status string) {
	var value float64
	if status == "active" {
		value = 1
	}
	m.KeyStatus.Set(value, provider, keyID, status)
}

// SetQuotaUsage 设置配额使用量
func (m *Metrics) SetQuotaUsage(idType, id string, usage float64) {
	m.QuotaUsage.Set(usage, idType, id)
}
