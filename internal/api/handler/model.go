package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lyp256/airouter/internal/model"
	"gorm.io/gorm"
)

// ModelHandler 模型处理器
type ModelHandler struct {
	db *gorm.DB
}

// NewModelHandler 创建模型处理器
func NewModelHandler(db *gorm.DB) *ModelHandler {
	return &ModelHandler{db: db}
}

// ModelWithUpstreams 模型及其上游模型
type ModelWithUpstreams struct {
	model.Model
	Upstreams []UpstreamDetail `json:"upstreams"`
}

// UpstreamDetail 上游模型详情
type UpstreamDetail struct {
	model.Upstream
	ProviderName    string `json:"provider_name"`
	ProviderKeyName string `json:"provider_key_name"`
}

// ListModels 列出模型
func (h *ModelHandler) ListModels(c *gin.Context) {
	var models []model.Model
	if err := h.db.Find(&models).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": models})
}

// GetModel 获取模型详情
func (h *ModelHandler) GetModel(c *gin.Context) {
	id := c.Param("id")

	var m model.Model
	if err := h.db.First(&m, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "模型不存在"})
		return
	}

	// 获取上游模型列表
	var upstreams []model.Upstream
	h.db.Where("model_id = ?", m.ID).Find(&upstreams)

	// 组装详情
	details := make([]UpstreamDetail, 0, len(upstreams))
	for _, u := range upstreams {
		detail := UpstreamDetail{Upstream: u}

		var provider model.Provider
		if err := h.db.First(&provider, "id = ?", u.ProviderID).Error; err == nil {
			detail.ProviderName = provider.Name
		}

		var apiKey model.ProviderKey
		if err := h.db.First(&apiKey, "id = ?", u.ProviderKeyID).Error; err == nil {
			detail.ProviderKeyName = apiKey.Name
		}

		details = append(details, detail)
	}

	c.JSON(http.StatusOK, gin.H{
		"data": ModelWithUpstreams{
			Model:     m,
			Upstreams: details,
		},
	})
}

// CreateModelRequest 创建模型请求
type CreateModelRequest struct {
	Name          string  `json:"name" binding:"required"`
	Description   string  `json:"description"`
	InputPrice    float64 `json:"input_price"`
	OutputPrice   float64 `json:"output_price"`
	ContextWindow int     `json:"context_window"`
}

// CreateModel 创建模型
func (h *ModelHandler) CreateModel(c *gin.Context) {
	var req CreateModelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	// 检查模型名称是否已存在
	var count int64
	h.db.Model(&model.Model{}).Where("name = ?", req.Name).Count(&count)
	if count > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "模型名称已存在"})
		return
	}

	m := model.Model{
		ID:            uuid.New().String(),
		Name:          req.Name,
		Description:   req.Description,
		InputPrice:    req.InputPrice,
		OutputPrice:   req.OutputPrice,
		ContextWindow: req.ContextWindow,
		Enabled:       true,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	if m.ContextWindow == 0 {
		m.ContextWindow = 4096
	}

	if err := h.db.Create(&m).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建失败"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": m})
}

// UpdateModelRequest 更新模型请求
type UpdateModelRequest struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	InputPrice    *float64 `json:"input_price"`
	OutputPrice   *float64 `json:"output_price"`
	ContextWindow *int     `json:"context_window"`
	Enabled       *bool    `json:"enabled"`
}

// UpdateModel 更新模型
func (h *ModelHandler) UpdateModel(c *gin.Context) {
	id := c.Param("id")

	var m model.Model
	if err := h.db.First(&m, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "模型不存在"})
		return
	}

	var req UpdateModelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	updates := map[string]interface{}{
		"updated_at": time.Now(),
	}

	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Description != "" {
		updates["description"] = req.Description
	}
	if req.InputPrice != nil {
		updates["input_price"] = *req.InputPrice
	}
	if req.OutputPrice != nil {
		updates["output_price"] = *req.OutputPrice
	}
	if req.ContextWindow != nil {
		updates["context_window"] = *req.ContextWindow
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}

	if err := h.db.Model(&m).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新失败"})
		return
	}

	h.db.First(&m, "id = ?", id)
	c.JSON(http.StatusOK, gin.H{"data": m})
}

// DeleteModel 删除模型
func (h *ModelHandler) DeleteModel(c *gin.Context) {
	id := c.Param("id")

	// 开启事务
	tx := h.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// 删除关联的上游模型
	if err := tx.Where("model_id = ?", id).Delete(&model.Upstream{}).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除关联上游模型失败"})
		return
	}

	// 删除模型
	result := tx.Delete(&model.Model{}, "id = ?", id)
	if result.Error != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除失败"})
		return
	}

	if result.RowsAffected == 0 {
		tx.Rollback()
		c.JSON(http.StatusNotFound, gin.H{"error": "模型不存在"})
		return
	}

	tx.Commit()
	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

// ToggleModel 切换模型启用状态
func (h *ModelHandler) ToggleModel(c *gin.Context) {
	id := c.Param("id")

	var m model.Model
	if err := h.db.First(&m, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "模型不存在"})
		return
	}

	m.Enabled = !m.Enabled
	m.UpdatedAt = time.Now()

	if err := h.db.Save(&m).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": m})
}

// ========== 上游模型管理 ==========

// ListUpstreams 列出所有上游模型
func (h *ModelHandler) ListUpstreams(c *gin.Context) {
	var upstreams []model.Upstream
	if err := h.db.Find(&upstreams).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}

	// 组装详情
	details := make([]UpstreamDetail, 0, len(upstreams))
	for _, u := range upstreams {
		detail := UpstreamDetail{Upstream: u}

		var provider model.Provider
		if err := h.db.First(&provider, "id = ?", u.ProviderID).Error; err == nil {
			detail.ProviderName = provider.Name
		}

		var apiKey model.ProviderKey
		if err := h.db.First(&apiKey, "id = ?", u.ProviderKeyID).Error; err == nil {
			detail.ProviderKeyName = apiKey.Name
		}

		details = append(details, detail)
	}

	c.JSON(http.StatusOK, gin.H{"data": details})
}

// GetUpstream 获取上游模型详情
func (h *ModelHandler) GetUpstream(c *gin.Context) {
	id := c.Param("id")

	var u model.Upstream
	if err := h.db.First(&u, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "上游模型不存在"})
		return
	}

	detail := UpstreamDetail{Upstream: u}

	var provider model.Provider
	if err := h.db.First(&provider, "id = ?", u.ProviderID).Error; err == nil {
		detail.ProviderName = provider.Name
	}

	var apiKey model.ProviderKey
	if err := h.db.First(&apiKey, "id = ?", u.ProviderKeyID).Error; err == nil {
		detail.ProviderKeyName = apiKey.Name
	}

	c.JSON(http.StatusOK, gin.H{"data": detail})
}

// ListModelUpstreams 列出模型的上游模型
func (h *ModelHandler) ListModelUpstreams(c *gin.Context) {
	modelID := c.Param("id")

	// 检查模型是否存在
	var m model.Model
	if err := h.db.First(&m, "id = ?", modelID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "模型不存在"})
		return
	}

	var upstreams []model.Upstream
	if err := h.db.Where("model_id = ?", modelID).Find(&upstreams).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}

	// 组装详情
	details := make([]UpstreamDetail, 0, len(upstreams))
	for _, u := range upstreams {
		detail := UpstreamDetail{Upstream: u}

		var provider model.Provider
		if err := h.db.First(&provider, "id = ?", u.ProviderID).Error; err == nil {
			detail.ProviderName = provider.Name
		}

		var apiKey model.ProviderKey
		if err := h.db.First(&apiKey, "id = ?", u.ProviderKeyID).Error; err == nil {
			detail.ProviderKeyName = apiKey.Name
		}

		details = append(details, detail)
	}

	c.JSON(http.StatusOK, gin.H{"data": details})
}

// CreateUpstreamRequest 创建上游模型请求
type CreateUpstreamRequest struct {
	ProviderID    string `json:"provider_id" binding:"required"`
	ProviderKeyID string `json:"provider_key_id" binding:"required"`
	ProviderModel string `json:"provider_model" binding:"required"`
	Weight        int    `json:"weight"`
	Priority      int    `json:"priority"`
}

// CreateUpstream 为模型添加上游模型
func (h *ModelHandler) CreateUpstream(c *gin.Context) {
	modelID := c.Param("id")

	// 检查模型是否存在
	var m model.Model
	if err := h.db.First(&m, "id = ?", modelID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "模型不存在"})
		return
	}

	var req CreateUpstreamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	// 检查供应商是否存在
	var provider model.Provider
	if err := h.db.First(&provider, "id = ?", req.ProviderID).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "供应商不存在"})
		return
	}

	// 检查供应商密钥是否存在
	var apiKey model.ProviderKey
	if err := h.db.First(&apiKey, "id = ?", req.ProviderKeyID).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "供应商密钥不存在"})
		return
	}

	// 检查密钥是否属于该供应商
	if apiKey.ProviderID != req.ProviderID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "密钥不属于该供应商"})
		return
	}

	u := model.Upstream{
		ID:            uuid.New().String(),
		ModelID:       modelID,
		ProviderID:    req.ProviderID,
		ProviderKeyID: req.ProviderKeyID,
		ProviderModel: req.ProviderModel,
		Weight:        req.Weight,
		Priority:      req.Priority,
		Status:        "active",
		Enabled:       true,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	if u.Weight == 0 {
		u.Weight = 1
	}

	if err := h.db.Create(&u).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建失败"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": u})
}

// UpdateUpstreamRequest 更新上游模型请求
type UpdateUpstreamRequest struct {
	ProviderKeyID string `json:"provider_key_id"`
	ProviderModel string `json:"provider_model"`
	Weight        *int   `json:"weight"`
	Priority      *int   `json:"priority"`
	Enabled       *bool  `json:"enabled"`
}

// UpdateUpstream 更新上游模型
func (h *ModelHandler) UpdateUpstream(c *gin.Context) {
	id := c.Param("id")

	var u model.Upstream
	if err := h.db.First(&u, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "上游模型不存在"})
		return
	}

	var req UpdateUpstreamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	updates := map[string]interface{}{
		"updated_at": time.Now(),
	}

	if req.ProviderKeyID != "" {
		// 检查密钥是否存在且属于同一供应商
		var apiKey model.ProviderKey
		if err := h.db.First(&apiKey, "id = ?", req.ProviderKeyID).Error; err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "供应商密钥不存在"})
			return
		}
		if apiKey.ProviderID != u.ProviderID {
			c.JSON(http.StatusBadRequest, gin.H{"error": "密钥不属于该供应商"})
			return
		}
		updates["provider_key_id"] = req.ProviderKeyID
	}
	if req.ProviderModel != "" {
		updates["provider_model"] = req.ProviderModel
	}
	if req.Weight != nil {
		updates["weight"] = *req.Weight
	}
	if req.Priority != nil {
		updates["priority"] = *req.Priority
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}

	if err := h.db.Model(&u).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新失败"})
		return
	}

	h.db.First(&u, "id = ?", id)
	c.JSON(http.StatusOK, gin.H{"data": u})
}

// DeleteUpstream 删除上游模型
func (h *ModelHandler) DeleteUpstream(c *gin.Context) {
	id := c.Param("id")

	result := h.db.Delete(&model.Upstream{}, "id = ?", id)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除失败"})
		return
	}

	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "上游模型不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

// ToggleUpstream 切换上游模型启用状态
func (h *ModelHandler) ToggleUpstream(c *gin.Context) {
	id := c.Param("id")

	var u model.Upstream
	if err := h.db.First(&u, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "上游模型不存在"})
		return
	}

	u.Enabled = !u.Enabled
	u.UpdatedAt = time.Now()

	if err := h.db.Save(&u).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": u})
}
