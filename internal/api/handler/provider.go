package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lyp256/airouter/internal/model"
	"gorm.io/gorm"
)

// ProviderHandler 供应商处理器
type ProviderHandler struct {
	db *gorm.DB
}

// NewProviderHandler 创建供应商处理器
func NewProviderHandler(db *gorm.DB) *ProviderHandler {
	return &ProviderHandler{db: db}
}

// ListProviders 列出供应商
func (h *ProviderHandler) ListProviders(c *gin.Context) {
	var providers []model.Provider
	if err := h.db.Find(&providers).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": providers})
}

// GetProvider 获取供应商详情
func (h *ProviderHandler) GetProvider(c *gin.Context) {
	id := c.Param("id")

	var provider model.Provider
	if err := h.db.First(&provider, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "供应商不存在"})
		return
	}

	// 获取密钥列表
	var keys []model.ProviderKey
	h.db.Where("provider_id = ?", id).Find(&keys)

	c.JSON(http.StatusOK, gin.H{
		"data": provider,
		"keys": keys,
	})
}

// CreateProviderRequest 创建供应商请求
type CreateProviderRequest struct {
	Name        string `json:"name" binding:"required"`
	Type        string `json:"type" binding:"required"`
	BaseURL     string `json:"base_url"`
	APIPath     string `json:"api_path"`
	Description string `json:"description"`
}

// CreateProvider 创建供应商
func (h *ProviderHandler) CreateProvider(c *gin.Context) {
	var req CreateProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	// 检查名称是否已存在
	var count int64
	h.db.Model(&model.Provider{}).Where("name = ?", req.Name).Count(&count)
	if count > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "供应商名称已存在"})
		return
	}

	provider := model.Provider{
		ID:          uuid.New().String(),
		Name:        req.Name,
		Type:        req.Type,
		BaseURL:     req.BaseURL,
		APIPath:     req.APIPath,
		Description: req.Description,
		Enabled:     true,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := h.db.Create(&provider).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建失败"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": provider})
}

// UpdateProviderRequest 更新供应商请求
type UpdateProviderRequest struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	BaseURL     string `json:"base_url"`
	APIPath     string `json:"api_path"`
	Description string `json:"description"`
	Enabled     *bool  `json:"enabled"`
}

// UpdateProvider 更新供应商
func (h *ProviderHandler) UpdateProvider(c *gin.Context) {
	id := c.Param("id")

	var provider model.Provider
	if err := h.db.First(&provider, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "供应商不存在"})
		return
	}

	var req UpdateProviderRequest
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
	if req.Type != "" {
		updates["type"] = req.Type
	}
	// base_url 和 api_path 允许设为空值
	updates["base_url"] = req.BaseURL
	updates["api_path"] = req.APIPath
	// description 允许设为空值
	updates["description"] = req.Description
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}

	if err := h.db.Model(&provider).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新失败"})
		return
	}

	h.db.First(&provider, "id = ?", id)
	c.JSON(http.StatusOK, gin.H{"data": provider})
}

// DeleteProvider 删除供应商
func (h *ProviderHandler) DeleteProvider(c *gin.Context) {
	id := c.Param("id")

	// 开启事务
	tx := h.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	result := tx.Delete(&model.Provider{}, "id = ?", id)
	if result.Error != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除失败"})
		return
	}

	if result.RowsAffected == 0 {
		tx.Rollback()
		c.JSON(http.StatusNotFound, gin.H{"error": "供应商不存在"})
		return
	}

	// 获取要删除的密钥 ID 列表
	var keyIDs []string
	tx.Model(&model.ProviderKey{}).Where("provider_id = ?", id).Pluck("id", &keyIDs)

	// 删除关联的上游模型
	tx.Delete(&model.Upstream{}, "provider_id = ?", id)

	// 将使用日志中的 provider_key_id 设为空（保留日志用于统计）
	if len(keyIDs) > 0 {
		tx.Model(&model.UsageLog{}).Where("provider_key_id IN ?", keyIDs).Update("provider_key_id", "")
	}

	// 删除关联的密钥
	tx.Delete(&model.ProviderKey{}, "provider_id = ?", id)

	tx.Commit()
	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

// CreateProviderKeyRequest 创建供应商密钥请求
type CreateProviderKeyRequest struct {
	Name string `json:"name" binding:"required"`
	Key  string `json:"key" binding:"required"`
}

// CreateProviderKey 创建供应商密钥
func (h *ProviderHandler) CreateProviderKey(c *gin.Context) {
	providerID := c.Param("id")

	var provider model.Provider
	if err := h.db.First(&provider, "id = ?", providerID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "供应商不存在"})
		return
	}

	var req CreateProviderKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	apiKey := model.ProviderKey{
		ID:         uuid.New().String(),
		ProviderID: providerID,
		Name:       req.Name,
		Key:        req.Key,
		Status:     "active",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	if err := h.db.Create(&apiKey).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建失败"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": apiKey})
}

// ListProviderKeys 列出供应商密钥
func (h *ProviderHandler) ListProviderKeys(c *gin.Context) {
	providerID := c.Param("id")

	var keys []model.ProviderKey
	if err := h.db.Where("provider_id = ?", providerID).Find(&keys).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": keys})
}

// UpdateProviderKeyRequest 更新供应商密钥请求
type UpdateProviderKeyRequest struct {
	Name       string `json:"name"`
	Key        string `json:"key"`
	Status     string `json:"status"`
	QuotaLimit *int64 `json:"quota_limit"`
}

// UpdateProviderKey 更新供应商密钥
func (h *ProviderHandler) UpdateProviderKey(c *gin.Context) {
	keyID := c.Param("key_id")

	var apiKey model.ProviderKey
	if err := h.db.First(&apiKey, "id = ?", keyID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "密钥不存在"})
		return
	}

	var req UpdateProviderKeyRequest
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
	if req.Key != "" {
		updates["key"] = req.Key
	}
	if req.Status != "" {
		updates["status"] = req.Status
	}
	if req.QuotaLimit != nil {
		updates["quota_limit"] = *req.QuotaLimit
	}

	if err := h.db.Model(&apiKey).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新失败"})
		return
	}

	h.db.First(&apiKey, "id = ?", keyID)
	c.JSON(http.StatusOK, gin.H{"data": apiKey})
}

// DeleteProviderKey 删除供应商密钥
func (h *ProviderHandler) DeleteProviderKey(c *gin.Context) {
	keyID := c.Param("key_id")

	// 检查是否有关联的上游模型
	var count int64
	h.db.Model(&model.Upstream{}).Where("provider_key_id = ?", keyID).Count(&count)
	if count > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该密钥有关联的上游模型，无法删除"})
		return
	}

	result := h.db.Delete(&model.ProviderKey{}, "id = ?", keyID)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除失败"})
		return
	}

	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "密钥不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}
