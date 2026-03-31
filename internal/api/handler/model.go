package handler

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lyp256/airouter/internal/model"
	"github.com/lyp256/airouter/internal/provider"
	"github.com/lyp256/airouter/internal/service"
	"github.com/lyp256/airouter/pkg/anthropic"
	"github.com/lyp256/airouter/pkg/openai"
	"gorm.io/gorm"
)

// ModelHandler 模型处理器
type ModelHandler struct {
	db               *gorm.DB
	upstreamSelector *service.UpstreamSelector
}

// NewModelHandler 创建模型处理器
func NewModelHandler(db *gorm.DB, upstreamSelector *service.UpstreamSelector) *ModelHandler {
	return &ModelHandler{db: db, upstreamSelector: upstreamSelector}
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
	ProviderType    string `json:"provider_type"` // 供应商类型：openai, anthropic, openai_compatible
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
			detail.ProviderType = provider.Type
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
	Name          string `json:"name" binding:"required"`
	ProviderType  string `json:"provider_type" binding:"required"` // 供应商类型：openai, anthropic, openai_compatible
	Description   string `json:"description"`
	InputPrice    int64  `json:"input_price"`  // 输入价格（纳 BU/1K token）
	OutputPrice   int64  `json:"output_price"` // 输出价格（纳 BU/1K token）
	ContextWindow int    `json:"context_window"`
}

// CreateModel 创建模型
func (h *ModelHandler) CreateModel(c *gin.Context) {
	var req CreateModelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	// 验证供应商类型
	validTypes := map[string]bool{"openai": true, "anthropic": true, "openai_compatible": true}
	if !validTypes[req.ProviderType] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的供应商类型，可选值：openai, anthropic, openai_compatible"})
		return
	}

	// 检查模型名称+类型是否已存在
	var count int64
	h.db.Model(&model.Model{}).Where("name = ? AND provider_type = ?", req.Name, req.ProviderType).Count(&count)
	if count > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "相同类型的模型名称已存在"})
		return
	}

	m := model.Model{
		ID:            uuid.New().String(),
		Name:          req.Name,
		ProviderType:  req.ProviderType,
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
	Name          string `json:"name"`
	Description   string `json:"description"`
	InputPrice    *int64 `json:"input_price"`  // 输入价格（纳 BU/1K token）
	OutputPrice   *int64 `json:"output_price"` // 输出价格（纳 BU/1K token）
	ContextWindow *int   `json:"context_window"`
	Enabled       *bool  `json:"enabled"`
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
			detail.ProviderType = provider.Type
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
		detail.ProviderType = provider.Type
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
			detail.ProviderType = provider.Type
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

	// 检查供应商类型是否与模型类型匹配
	if provider.Type != m.ProviderType {
		c.JSON(http.StatusBadRequest, gin.H{"error": "供应商类型与模型类型不匹配，模型类型为 " + m.ProviderType})
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

// ResetUpstreamStatus 重置上游模型健康状态为 active
func (h *ModelHandler) ResetUpstreamStatus(c *gin.Context) {
	id := c.Param("id")

	var u model.Upstream
	if err := h.db.First(&u, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "上游模型不存在"})
		return
	}

	if err := h.db.Model(&u).Updates(map[string]interface{}{
		"status":     "active",
		"updated_at": time.Now(),
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "重置失败"})
		return
	}

	// 清除选择器缓存，确保下次请求能选到该上游
	h.upstreamSelector.InvalidateCache(u.ModelID)

	h.db.First(&u, "id = ?", id)
	c.JSON(http.StatusOK, gin.H{"data": u})
}

// testUpstreamResult 测试单个上游模型的结果
type testUpstreamResult struct {
	Success             bool   `json:"success"`
	LatencyMs           int64  `json:"latency_ms"`
	FirstTokenLatencyMs int64  `json:"first_token_latency_ms"`
	UpstreamID          string `json:"upstream_id"`
	ProviderName        string `json:"provider_name"`
	ProviderModel       string `json:"provider_model"`
	Message             string `json:"message"`
	ResponseContent     string `json:"response_content,omitempty"`
}

// doTestUpstream 执行单个上游模型测试（流式请求获取首 Token 延迟）
func doTestUpstream(ctx context.Context, selection *service.UpstreamSelection) *testUpstreamResult {
	result := &testUpstreamResult{
		UpstreamID:    selection.Upstream.ID,
		ProviderName:  selection.Provider.Name,
		ProviderModel: selection.Upstream.ProviderModel,
	}

	startTime := time.Now()

	switch selection.Provider.Type {
	case "openai", "openai_compatible":
		client := provider.NewClient(provider.ClientConfig{
			BaseURL: selection.Provider.BaseURL,
			APIKey:  selection.DecryptedKey,
		})
		apiPath := selection.Provider.APIPath
		if apiPath == "" {
			apiPath = "/v1/chat/completions"
		}

		resp, err := client.DoStream(ctx, provider.Request{
			Method: "POST",
			Path:   apiPath,
			Body: map[string]interface{}{
				"model":      selection.Upstream.ProviderModel,
				"messages":   []map[string]string{{"role": "user", "content": "Hi"}},
				"max_tokens": 5,
				"stream":     true,
			},
		})
		if err != nil {
			result.Message = "测试失败: " + err.Error()
			result.LatencyMs = time.Since(startTime).Milliseconds()
			return result
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 400 {
			body, _ := io.ReadAll(resp.Body)
			result.Message = parseOpenAIError(body)
			result.LatencyMs = time.Since(startTime).Milliseconds()
			return result
		}

		// 解析 SSE 流获取首 Token 延迟和内容
		var firstTokenLatency int64
		firstTokenRecorded := false
		var contentBuilder strings.Builder

		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					break
				}
				break
			}
			line = strings.TrimSpace(line)
			if line == "" || !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				break
			}

			var chunk openai.StreamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}
			for _, choice := range chunk.Choices {
				if choice.Delta != nil {
					if !firstTokenRecorded && (choice.Delta.Content != "" || choice.Delta.ReasoningContent != "") {
						firstTokenLatency = time.Since(startTime).Milliseconds()
						firstTokenRecorded = true
					}
					if choice.Delta.Content != "" {
						contentBuilder.WriteString(choice.Delta.Content)
					}
				}
			}
		}

		result.Success = true
		result.LatencyMs = time.Since(startTime).Milliseconds()
		result.FirstTokenLatencyMs = firstTokenLatency
		result.ResponseContent = contentBuilder.String()
		result.Message = "测试成功"

	case "anthropic":
		apiPath := selection.Provider.APIPath
		if apiPath == "" {
			apiPath = "/v1/messages"
		}
		client := provider.NewAnthropicClient(provider.AnthropicConfig{
			BaseURL: selection.Provider.BaseURL,
			APIKey:  selection.DecryptedKey,
			APIPath: apiPath,
		})

		resp, err := client.MessagesStream(ctx, anthropic.MessagesRequest{
			Model:     selection.Upstream.ProviderModel,
			Messages:  []anthropic.Message{{Role: "user", Content: "Hi"}},
			MaxTokens: 5,
		})
		if err != nil {
			result.Message = "测试失败: " + err.Error()
			result.LatencyMs = time.Since(startTime).Milliseconds()
			return result
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 400 {
			body, _ := io.ReadAll(resp.Body)
			var errResp struct {
				Error *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal(body, &errResp) == nil && errResp.Error != nil {
				result.Message = "测试失败: " + errResp.Error.Message
			} else {
				result.Message = "测试失败: HTTP " + resp.Status
			}
			result.LatencyMs = time.Since(startTime).Milliseconds()
			return result
		}

		// 解析 SSE 流获取首 Token 延迟和内容
		var firstTokenLatency int64
		firstTokenRecorded := false
		var contentBuilder strings.Builder

		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					break
				}
				break
			}
			line = strings.TrimSpace(line)
			if line == "" || !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")

			var event anthropic.StreamEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}
			if !firstTokenRecorded && event.Type == "content_block_delta" && event.Delta != nil && (event.Delta.Text != "" || event.Delta.Thinking != "") {
				firstTokenLatency = time.Since(startTime).Milliseconds()
				firstTokenRecorded = true
			}
			if event.Type == "content_block_delta" && event.Delta != nil && event.Delta.Text != "" {
				contentBuilder.WriteString(event.Delta.Text)
			}
		}

		result.Success = true
		result.LatencyMs = time.Since(startTime).Milliseconds()
		result.FirstTokenLatencyMs = firstTokenLatency
		result.ResponseContent = contentBuilder.String()
		result.Message = "测试成功"

	default:
		result.Message = "不支持的供应商类型: " + selection.Provider.Type
		result.LatencyMs = time.Since(startTime).Milliseconds()
	}

	return result
}

// parseOpenAIError 解析 OpenAI 错误响应
func parseOpenAIError(body []byte) string {
	var errResp openai.ErrorResponse
	if json.Unmarshal(body, &errResp) == nil && errResp.Message != "" {
		return "测试失败: " + errResp.Message
	}
	return "测试失败: " + string(body)
}

// TestUpstream 测试上游模型连通性
func (h *ModelHandler) TestUpstream(c *gin.Context) {
	id := c.Param("id")

	selection, err := h.upstreamSelector.GetUpstreamSelection(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()

	result := doTestUpstream(ctx, selection)
	c.JSON(http.StatusOK, gin.H{"data": result})
}

// TestModelUpstreams 批量测试模型的所有上游模型
func (h *ModelHandler) TestModelUpstreams(c *gin.Context) {
	modelID := c.Param("id")

	// 检查模型是否存在
	var m model.Model
	if err := h.db.First(&m, "id = ?", modelID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "模型不存在"})
		return
	}

	// 获取模型的所有上游模型
	upstreams, err := h.upstreamSelector.GetUpstreamsByModel(modelID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询上游模型失败"})
		return
	}

	if len(upstreams) == 0 {
		c.JSON(http.StatusOK, gin.H{"data": []testUpstreamResult{}})
		return
	}

	// 并发测试所有上游模型
	type indexedResult struct {
		index  int
		result *testUpstreamResult
	}
	ch := make(chan indexedResult, len(upstreams))

	for i, u := range upstreams {
		go func(idx int, upstream *model.Upstream) {
			selection, err := h.upstreamSelector.GetUpstreamSelection(upstream.ID)
			if err != nil {
				ch <- indexedResult{index: idx, result: &testUpstreamResult{
					Success:       false,
					UpstreamID:    upstream.ID,
					ProviderModel: upstream.ProviderModel,
					Message:       "获取上游配置失败: " + err.Error(),
				}}
				return
			}

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			ch <- indexedResult{index: idx, result: doTestUpstream(ctx, selection)}
		}(i, u)
	}

	// 收集结果并保持顺序
	results := make([]*testUpstreamResult, len(upstreams))
	for i := 0; i < len(upstreams); i++ {
		r := <-ch
		results[r.index] = r.result
	}

	c.JSON(http.StatusOK, gin.H{"data": results})
}
