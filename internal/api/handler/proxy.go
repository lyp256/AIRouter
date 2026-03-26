package handler

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lyp256/airouter/internal/api/middleware"
	"github.com/lyp256/airouter/internal/model"
	"github.com/lyp256/airouter/internal/provider"
	"github.com/lyp256/airouter/internal/service"
	"github.com/lyp256/airouter/pkg/anthropic"
	"github.com/lyp256/airouter/pkg/openai"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ProxyHandler 代理处理器
type ProxyHandler struct {
	db               *gorm.DB
	logger           *zap.Logger
	upstreamSelector *service.UpstreamSelector
}

// NewProxyHandler 创建代理处理器
func NewProxyHandler(db *gorm.DB, logger *zap.Logger, upstreamSelector *service.UpstreamSelector) *ProxyHandler {
	return &ProxyHandler{
		db:               db,
		logger:           logger,
		upstreamSelector: upstreamSelector,
	}
}

// ChatCompletions Chat Completions API
func (h *ProxyHandler) ChatCompletions(c *gin.Context) {
	var req openai.ChatCompletionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": "请求参数错误: " + err.Error(),
				"type":    "invalid_request_error",
			},
		})
		return
	}

	// 获取模型配置
	var modelCfg model.Model
	result := h.db.Where("name = ? AND enabled = ?", req.Model, true).First(&modelCfg)
	if result.Error != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": "模型不存在或未启用: " + req.Model,
				"type":    "invalid_request_error",
			},
		})
		return
	}

	// 选择上游模型
	selection, err := h.upstreamSelector.SelectUpstream(modelCfg.ID)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{
				"message": "没有可用的上游模型",
				"type":    "internal_error",
			},
		})
		return
	}

	// 创建客户端
	client := provider.NewClient(provider.ClientConfig{
		BaseURL: selection.Provider.BaseURL,
		APIKey:  selection.DecryptedKey,
	})

	// 准备请求
	reqBody := h.prepareRequestBody(&req, selection.Upstream)

	startTime := time.Now()
	requestID := middleware.GetRequestID(c)

	// 处理流式请求
	if req.Stream {
		h.handleStreamChat(c, client, reqBody, selection, modelCfg, startTime, requestID)
		return
	}

	// 非流式请求
	h.handleNormalChat(c, client, reqBody, selection, modelCfg, startTime, requestID)
}

// prepareRequestBody 准备请求体
func (h *ProxyHandler) prepareRequestBody(req *openai.ChatCompletionRequest, upstream *model.Upstream) map[string]interface{} {
	reqBody := make(map[string]interface{})
	reqBody["model"] = upstream.ProviderModel
	reqBody["messages"] = req.Messages

	if req.Temperature != nil {
		reqBody["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		reqBody["top_p"] = *req.TopP
	}
	if req.N != nil {
		reqBody["n"] = *req.N
	}
	if req.Stop != nil {
		reqBody["stop"] = req.Stop
	}
	if req.MaxTokens != nil {
		reqBody["max_tokens"] = *req.MaxTokens
	}
	if req.PresencePenalty != nil {
		reqBody["presence_penalty"] = *req.PresencePenalty
	}
	if req.FrequencyPenalty != nil {
		reqBody["frequency_penalty"] = *req.FrequencyPenalty
	}
	if req.LogitBias != nil {
		reqBody["logit_bias"] = req.LogitBias
	}
	if req.User != "" {
		reqBody["user"] = req.User
	}
	if req.Tools != nil {
		reqBody["tools"] = req.Tools
	}
	if req.ToolChoice != nil {
		reqBody["tool_choice"] = req.ToolChoice
	}
	if req.ResponseFormat != nil {
		reqBody["response_format"] = req.ResponseFormat
	}

	// 复制额外字段
	for k, v := range req.Extra {
		if _, exists := reqBody[k]; !exists {
			reqBody[k] = v
		}
	}

	return reqBody
}

// handleNormalChat 处理非流式请求
func (h *ProxyHandler) handleNormalChat(c *gin.Context, client *provider.Client, reqBody map[string]interface{},
	selection *service.UpstreamSelection, modelCfg model.Model, startTime time.Time, requestID string) {

	// 获取 API 路径，如果供应商配置了则使用，否则使用默认路径
	apiPath := selection.Provider.APIPath
	if apiPath == "" {
		apiPath = "/v1/chat/completions"
	}

	resp, err := client.Do(c.Request.Context(), provider.Request{
		Method: "POST",
		Path:   apiPath,
		Body:   reqBody,
	})
	if err != nil {
		h.logger.Error("请求上游失败", zap.Error(err), zap.String("request_id", requestID))
		h.upstreamSelector.MarkUpstreamError(selection.Upstream.ID)
		h.upstreamSelector.MarkAPIKeyError(selection.ProviderKey.ID)
		c.JSON(http.StatusBadGateway, gin.H{
			"error": gin.H{
				"message": "请求上游服务失败",
				"type":    "upstream_error",
			},
		})
		return
	}

	// 记录成功
	h.upstreamSelector.MarkUpstreamSuccess(selection.Upstream.ID)
	h.upstreamSelector.MarkAPIKeySuccess(selection.ProviderKey.ID)
	latency := time.Since(startTime).Milliseconds()

	// 解析响应计算 token
	var chatResp openai.ChatCompletionResponse
	if err := json.Unmarshal(resp.Body, &chatResp); err == nil && chatResp.Usage != nil {
		// 记录使用日志
		h.logUsage(c, selection, modelCfg.Name, chatResp.Usage, latency, "success", "")
	}

	// 返回响应
	c.Data(resp.StatusCode, "application/json", resp.Body)
}

// handleStreamChat 处理流式请求
func (h *ProxyHandler) handleStreamChat(c *gin.Context, client *provider.Client, reqBody map[string]interface{},
	selection *service.UpstreamSelection, modelCfg model.Model, startTime time.Time, requestID string) {

	// 确保设置 stream: true
	reqBody["stream"] = true

	// 获取 API 路径，如果供应商配置了则使用，否则使用默认路径
	apiPath := selection.Provider.APIPath
	if apiPath == "" {
		apiPath = "/v1/chat/completions"
	}

	resp, err := client.DoStream(c.Request.Context(), provider.Request{
		Method: "POST",
		Path:   apiPath,
		Body:   reqBody,
	})
	if err != nil {
		h.logger.Error("请求上游失败", zap.Error(err), zap.String("request_id", requestID))
		h.upstreamSelector.MarkUpstreamError(selection.Upstream.ID)
		h.upstreamSelector.MarkAPIKeyError(selection.ProviderKey.ID)
		c.JSON(http.StatusBadGateway, gin.H{
			"error": gin.H{
				"message": "请求上游服务失败",
				"type":    "upstream_error",
			},
		})
		return
	}
	defer resp.Body.Close()

	// 记录成功
	h.upstreamSelector.MarkUpstreamSuccess(selection.Upstream.ID)
	h.upstreamSelector.MarkAPIKeySuccess(selection.ProviderKey.ID)

	// 设置响应头
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Request-ID", requestID)

	// 流式传输
	reader := bufio.NewReader(resp.Body)
	flusher, _ := c.Writer.(http.Flusher)

	var totalTokens int

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			h.logger.Error("读取流响应失败", zap.Error(err))
			break
		}

		line = strings.TrimSpace(line)

		if line == "" {
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		if data == "[DONE]" {
			c.SSEvent("", "[DONE]")
			flusher.Flush()
			break
		}

		// 解析 chunk 计算 token
		var chunk openai.StreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err == nil {
			// 简单估算 token（实际应从 usage 获取）
			for _, choice := range chunk.Choices {
				if choice.Delta != nil && choice.Delta.Content != "" {
					totalTokens += len(choice.Delta.Content) / 4 // 粗略估算
				}
			}
		}

		// 转发给客户端
		fmt.Fprintf(c.Writer, "data: %s\n\n", data)
		flusher.Flush()
	}

	// 记录使用日志
	latency := time.Since(startTime).Milliseconds()
	h.logUsage(c, selection, modelCfg.Name, &openai.Usage{
		PromptTokens:     0,
		CompletionTokens: totalTokens,
		TotalTokens:      totalTokens,
	}, latency, "success", "")
}

// Models 模型列表 API
func (h *ProxyHandler) Models(c *gin.Context) {
	var models []model.Model
	h.db.Where("enabled = ?", true).Find(&models)

	data := make([]openai.ModelInfo, len(models))
	for i, m := range models {
		data[i] = openai.ModelInfo{
			ID:      m.Name,
			Object:  "model",
			Created: m.CreatedAt.Unix(),
			OwnedBy: "airouter",
		}
	}

	c.JSON(http.StatusOK, openai.ModelsResponse{Data: data})
}

// Completions 文本补全 API（旧版）
func (h *ProxyHandler) Completions(c *gin.Context) {
	var req openai.CompletionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": "请求参数错误: " + err.Error(),
				"type":    "invalid_request_error",
			},
		})
		return
	}

	// 获取模型配置
	var modelCfg model.Model
	result := h.db.Where("name = ? AND enabled = ?", req.Model, true).First(&modelCfg)
	if result.Error != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": "模型不存在或未启用: " + req.Model,
				"type":    "invalid_request_error",
			},
		})
		return
	}

	// 选择上游模型
	selection, err := h.upstreamSelector.SelectUpstream(modelCfg.ID)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{
				"message": "没有可用的上游模型",
				"type":    "internal_error",
			},
		})
		return
	}

	// 创建客户端
	client := provider.NewClient(provider.ClientConfig{
		BaseURL: selection.Provider.BaseURL,
		APIKey:  selection.DecryptedKey,
	})

	// 准备请求
	reqBody := h.prepareCompletionRequestBody(&req, selection.Upstream)

	startTime := time.Now()
	requestID := middleware.GetRequestID(c)

	// 处理流式请求
	if req.Stream {
		h.handleStreamCompletion(c, client, reqBody, selection, modelCfg, startTime, requestID)
		return
	}

	// 非流式请求
	h.handleNormalCompletion(c, client, reqBody, selection, modelCfg, startTime, requestID)
}

// prepareCompletionRequestBody 准备 Completions 请求体
func (h *ProxyHandler) prepareCompletionRequestBody(req *openai.CompletionRequest, upstream *model.Upstream) map[string]interface{} {
	reqBody := make(map[string]interface{})
	reqBody["model"] = upstream.ProviderModel
	reqBody["prompt"] = req.Prompt

	if req.Suffix != "" {
		reqBody["suffix"] = req.Suffix
	}
	if req.MaxTokens != nil {
		reqBody["max_tokens"] = *req.MaxTokens
	}
	if req.Temperature != nil {
		reqBody["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		reqBody["top_p"] = *req.TopP
	}
	if req.N != nil {
		reqBody["n"] = *req.N
	}
	if req.Echo {
		reqBody["echo"] = req.Echo
	}
	if req.Stop != nil {
		reqBody["stop"] = req.Stop
	}
	if req.PresencePenalty != nil {
		reqBody["presence_penalty"] = *req.PresencePenalty
	}
	if req.FrequencyPenalty != nil {
		reqBody["frequency_penalty"] = *req.FrequencyPenalty
	}
	if req.BestOf != nil {
		reqBody["best_of"] = *req.BestOf
	}
	if req.Logprobs != nil {
		reqBody["logprobs"] = *req.Logprobs
	}
	if req.LogitBias != nil {
		reqBody["logit_bias"] = req.LogitBias
	}
	if req.User != "" {
		reqBody["user"] = req.User
	}

	// 复制额外字段
	for k, v := range req.Extra {
		if _, exists := reqBody[k]; !exists {
			reqBody[k] = v
		}
	}

	return reqBody
}

// handleNormalCompletion 处理非流式 Completions 请求
func (h *ProxyHandler) handleNormalCompletion(c *gin.Context, client *provider.Client, reqBody map[string]interface{},
	selection *service.UpstreamSelection, modelCfg model.Model, startTime time.Time, requestID string) {

	// 获取 API 路径，如果供应商配置了则使用，否则使用默认路径
	apiPath := selection.Provider.APIPath
	if apiPath == "" {
		apiPath = "/v1/completions"
	}

	resp, err := client.Do(c.Request.Context(), provider.Request{
		Method: "POST",
		Path:   apiPath,
		Body:   reqBody,
	})
	if err != nil {
		h.logger.Error("请求上游失败", zap.Error(err), zap.String("request_id", requestID))
		h.upstreamSelector.MarkUpstreamError(selection.Upstream.ID)
		h.upstreamSelector.MarkAPIKeyError(selection.ProviderKey.ID)
		c.JSON(http.StatusBadGateway, gin.H{
			"error": gin.H{
				"message": "请求上游服务失败",
				"type":    "upstream_error",
			},
		})
		return
	}

	// 记录成功
	h.upstreamSelector.MarkUpstreamSuccess(selection.Upstream.ID)
	h.upstreamSelector.MarkAPIKeySuccess(selection.ProviderKey.ID)
	latency := time.Since(startTime).Milliseconds()

	// 解析响应计算 token
	var compResp openai.CompletionResponse
	if err := json.Unmarshal(resp.Body, &compResp); err == nil && compResp.Usage != nil {
		// 记录使用日志
		h.logUsage(c, selection, modelCfg.Name, compResp.Usage, latency, "success", "")
	}

	// 返回响应
	c.Data(resp.StatusCode, "application/json", resp.Body)
}

// handleStreamCompletion 处理流式 Completions 请求
func (h *ProxyHandler) handleStreamCompletion(c *gin.Context, client *provider.Client, reqBody map[string]interface{},
	selection *service.UpstreamSelection, modelCfg model.Model, startTime time.Time, requestID string) {

	// 确保设置 stream: true
	reqBody["stream"] = true

	// 获取 API 路径，如果供应商配置了则使用，否则使用默认路径
	apiPath := selection.Provider.APIPath
	if apiPath == "" {
		apiPath = "/v1/completions"
	}

	resp, err := client.DoStream(c.Request.Context(), provider.Request{
		Method: "POST",
		Path:   apiPath,
		Body:   reqBody,
	})
	if err != nil {
		h.logger.Error("请求上游失败", zap.Error(err), zap.String("request_id", requestID))
		h.upstreamSelector.MarkUpstreamError(selection.Upstream.ID)
		h.upstreamSelector.MarkAPIKeyError(selection.ProviderKey.ID)
		c.JSON(http.StatusBadGateway, gin.H{
			"error": gin.H{
				"message": "请求上游服务失败",
				"type":    "upstream_error",
			},
		})
		return
	}
	defer resp.Body.Close()

	// 记录成功
	h.upstreamSelector.MarkUpstreamSuccess(selection.Upstream.ID)
	h.upstreamSelector.MarkAPIKeySuccess(selection.ProviderKey.ID)

	// 设置响应头
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Request-ID", requestID)

	// 流式传输
	reader := bufio.NewReader(resp.Body)
	flusher, _ := c.Writer.(http.Flusher)

	var totalTokens int

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			h.logger.Error("读取流响应失败", zap.Error(err))
			break
		}

		line = strings.TrimSpace(line)

		if line == "" {
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		if data == "[DONE]" {
			c.SSEvent("", "[DONE]")
			flusher.Flush()
			break
		}

		// 解析 chunk 计算 token
		var chunk openai.CompletionStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err == nil {
			for _, choice := range chunk.Choices {
				totalTokens += len(choice.Text) / 4 // 粗略估算
			}
		}

		// 转发给客户端
		fmt.Fprintf(c.Writer, "data: %s\n\n", data)
		flusher.Flush()
	}

	// 记录使用日志
	latency := time.Since(startTime).Milliseconds()
	h.logUsage(c, selection, modelCfg.Name, &openai.Usage{
		PromptTokens:     0,
		CompletionTokens: totalTokens,
		TotalTokens:      totalTokens,
	}, latency, "success", "")
}

// Embeddings Embeddings API
func (h *ProxyHandler) Embeddings(c *gin.Context) {
	var req openai.EmbeddingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": "请求参数错误",
				"type":    "invalid_request_error",
			},
		})
		return
	}

	// 获取模型配置
	var modelCfg model.Model
	result := h.db.Where("name = ? AND enabled = ?", req.Model, true).First(&modelCfg)
	if result.Error != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": "模型不存在或未启用",
				"type":    "invalid_request_error",
			},
		})
		return
	}

	// 选择上游模型
	selection, err := h.upstreamSelector.SelectUpstream(modelCfg.ID)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{
				"message": "没有可用的上游模型",
				"type":    "internal_error",
			},
		})
		return
	}

	// 创建客户端
	client := provider.NewClient(provider.ClientConfig{
		BaseURL: selection.Provider.BaseURL,
		APIKey:  selection.DecryptedKey,
	})

	// 准备请求
	reqBody := map[string]interface{}{
		"model": selection.Upstream.ProviderModel,
		"input": req.Input,
	}
	if req.EncodingFormat != "" {
		reqBody["encoding_format"] = req.EncodingFormat
	}
	if req.Dimensions > 0 {
		reqBody["dimensions"] = req.Dimensions
	}

	startTime := time.Now()

	// 获取 API 路径，如果供应商配置了则使用，否则使用默认路径
	apiPath := selection.Provider.APIPath
	if apiPath == "" {
		apiPath = "/v1/embeddings"
	}

	resp, err := client.Do(c.Request.Context(), provider.Request{
		Method: "POST",
		Path:   apiPath,
		Body:   reqBody,
	})
	if err != nil {
		h.upstreamSelector.MarkUpstreamError(selection.Upstream.ID)
		h.upstreamSelector.MarkAPIKeyError(selection.ProviderKey.ID)
		c.JSON(http.StatusBadGateway, gin.H{
			"error": gin.H{
				"message": "请求上游服务失败",
				"type":    "upstream_error",
			},
		})
		return
	}

	h.upstreamSelector.MarkUpstreamSuccess(selection.Upstream.ID)
	h.upstreamSelector.MarkAPIKeySuccess(selection.ProviderKey.ID)
	latency := time.Since(startTime).Milliseconds()

	// 记录使用日志
	var embResp openai.EmbeddingResponse
	if err := json.Unmarshal(resp.Body, &embResp); err == nil && embResp.Usage != nil {
		h.logUsage(c, selection, modelCfg.Name, embResp.Usage, latency, "success", "")
	}

	c.Data(resp.StatusCode, "application/json", resp.Body)
}

// logUsage 记录使用日志
func (h *ProxyHandler) logUsage(c *gin.Context, selection *service.UpstreamSelection, modelName string, usage *openai.Usage, latency int64, status, errMsg string) {
	userID := middleware.GetUserID(c)
	userKeyID := middleware.GetUserKeyID(c)

	// 计算成本（简化版）
	cost := float64(usage.PromptTokens+usage.CompletionTokens) * 0.0001

	log := model.UsageLog{
		ID:            requestID(),
		UserID:        userID,
		UserKeyID:     userKeyID,
		UpstreamID:    selection.Upstream.ID,
		ProviderKeyID: selection.ProviderKey.ID,
		Model:         modelName,
		ProviderModel: selection.Upstream.ProviderModel,
		ProviderName:  selection.Provider.Name,
		InputTokens:   usage.PromptTokens,
		OutputTokens:  usage.CompletionTokens,
		Cost:          cost,
		Latency:       int(latency),
		Status:        status,
		ErrorMessage:  errMsg,
		RequestID:     middleware.GetRequestID(c),
		CreatedAt:     time.Now(),
	}

	h.db.Create(&log)
}

func requestID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// parseSSE 解析 SSE 数据
func parseSSE(data []byte) (string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			return strings.TrimPrefix(line, "data: "), nil
		}
	}
	return "", scanner.Err()
}

// AnthropicMessages Anthropic Messages API
func (h *ProxyHandler) AnthropicMessages(c *gin.Context) {
	var req anthropic.MessagesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"type":    "invalid_request_error",
			"message": "请求参数错误: " + err.Error(),
		})
		return
	}

	// 获取模型配置
	var modelCfg model.Model
	result := h.db.Where("name = ? AND enabled = ?", req.Model, true).First(&modelCfg)
	if result.Error != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"type":    "invalid_request_error",
			"message": "模型不存在或未启用: " + req.Model,
		})
		return
	}

	// 选择上游模型
	selection, err := h.upstreamSelector.SelectUpstream(modelCfg.ID)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"type":    "internal_error",
			"message": "没有可用的上游模型",
		})
		return
	}

	startTime := time.Now()
	requestID := middleware.GetRequestID(c)

	// 判断供应商类型
	if selection.Provider.Type == "anthropic" {
		// 使用原生 Anthropic 客户端
		h.handleAnthropicNative(c, &req, selection, &modelCfg, startTime, requestID)
	} else {
		// 使用 OpenAI 兼容模式转换
		h.handleAnthropicViaOpenAI(c, &req, selection, &modelCfg, startTime, requestID)
	}
}

// handleAnthropicNative 使用原生 Anthropic API 处理
func (h *ProxyHandler) handleAnthropicNative(c *gin.Context, req *anthropic.MessagesRequest,
	selection *service.UpstreamSelection, modelCfg *model.Model, startTime time.Time, requestID string) {

	client := provider.NewAnthropicClient(provider.AnthropicConfig{
		BaseURL: selection.Provider.BaseURL,
		APIKey:  selection.DecryptedKey,
	})

	// 处理流式请求
	if req.Stream {
		h.handleAnthropicStream(c, client, req, selection, modelCfg, startTime, requestID)
		return
	}

	// 非流式请求
	resp, err := client.Messages(c.Request.Context(), *req)
	if err != nil {
		h.logger.Error("请求 Anthropic 失败", zap.Error(err), zap.String("request_id", requestID))
		h.upstreamSelector.MarkUpstreamError(selection.Upstream.ID)
		h.upstreamSelector.MarkAPIKeyError(selection.ProviderKey.ID)
		c.JSON(http.StatusBadGateway, gin.H{
			"type":    "upstream_error",
			"message": "请求上游服务失败: " + err.Error(),
		})
		return
	}

	h.upstreamSelector.MarkUpstreamSuccess(selection.Upstream.ID)
	h.upstreamSelector.MarkAPIKeySuccess(selection.ProviderKey.ID)
	latency := time.Since(startTime).Milliseconds()

	// 记录使用日志
	if resp.Usage != nil {
		h.logUsage(c, selection, modelCfg.Name, &openai.Usage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		}, latency, "success", "")
	}

	c.JSON(http.StatusOK, resp)
}

// handleAnthropicStream 处理 Anthropic 流式请求
func (h *ProxyHandler) handleAnthropicStream(c *gin.Context, client *provider.AnthropicClient,
	req *anthropic.MessagesRequest, selection *service.UpstreamSelection, modelCfg *model.Model,
	startTime time.Time, requestID string) {

	resp, err := client.MessagesStream(c.Request.Context(), *req)
	if err != nil {
		h.logger.Error("请求 Anthropic 失败", zap.Error(err), zap.String("request_id", requestID))
		h.upstreamSelector.MarkUpstreamError(selection.Upstream.ID)
		h.upstreamSelector.MarkAPIKeyError(selection.ProviderKey.ID)
		c.JSON(http.StatusBadGateway, gin.H{
			"type":    "upstream_error",
			"message": "请求上游服务失败: " + err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	h.upstreamSelector.MarkUpstreamSuccess(selection.Upstream.ID)
	h.upstreamSelector.MarkAPIKeySuccess(selection.ProviderKey.ID)

	// 设置响应头
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Request-ID", requestID)

	// 流式传输
	reader := bufio.NewReader(resp.Body)
	flusher, _ := c.Writer.(http.Flusher)

	var totalTokens int

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			h.logger.Error("读取流响应失败", zap.Error(err))
			break
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		// 解析事件计算 token
		var event anthropic.StreamEvent
		if err := json.Unmarshal([]byte(data), &event); err == nil {
			if event.Type == "content_block_delta" && event.Delta != nil {
				totalTokens += len(event.Delta.Text) / 4
			}
			if event.Type == "message_delta" && event.DeltaUsage != nil {
				totalTokens = event.DeltaUsage.OutputTokens
			}
		}

		// 转发给客户端
		fmt.Fprintf(c.Writer, "data: %s\n\n", data)
		flusher.Flush()
	}

	// 记录使用日志
	latency := time.Since(startTime).Milliseconds()
	h.logUsage(c, selection, modelCfg.Name, &openai.Usage{
		PromptTokens:     0,
		CompletionTokens: totalTokens,
		TotalTokens:      totalTokens,
	}, latency, "success", "")
}

// handleAnthropicViaOpenAI 通过 OpenAI 兼容模式处理 Anthropic 请求
func (h *ProxyHandler) handleAnthropicViaOpenAI(c *gin.Context, req *anthropic.MessagesRequest,
	selection *service.UpstreamSelection, modelCfg *model.Model, startTime time.Time, requestID string) {

	// 转换请求格式
	messages := provider.ConvertToOpenAI(req)

	openAIReq := map[string]interface{}{
		"model":    selection.Upstream.ProviderModel,
		"messages": messages,
	}
	if req.MaxTokens > 0 {
		openAIReq["max_tokens"] = req.MaxTokens
	}
	if req.Temperature != nil {
		openAIReq["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		openAIReq["top_p"] = *req.TopP
	}
	if len(req.StopSequences) > 0 {
		openAIReq["stop"] = req.StopSequences
	}

	client := provider.NewClient(provider.ClientConfig{
		BaseURL: selection.Provider.BaseURL,
		APIKey:  selection.DecryptedKey,
	})

	// 获取 API 路径，如果供应商配置了则使用，否则使用默认路径
	apiPath := selection.Provider.APIPath
	if apiPath == "" {
		apiPath = "/v1/chat/completions"
	}

	// 处理流式请求
	if req.Stream {
		h.handleAnthropicStreamViaOpenAI(c, client, openAIReq, selection, modelCfg, startTime, requestID, req.Model, apiPath)
		return
	}

	// 非流式请求
	resp, err := client.Do(c.Request.Context(), provider.Request{
		Method: "POST",
		Path:   apiPath,
		Body:   openAIReq,
	})
	if err != nil {
		h.logger.Error("请求上游失败", zap.Error(err), zap.String("request_id", requestID))
		h.upstreamSelector.MarkUpstreamError(selection.Upstream.ID)
		h.upstreamSelector.MarkAPIKeyError(selection.ProviderKey.ID)
		c.JSON(http.StatusBadGateway, gin.H{
			"type":    "upstream_error",
			"message": "请求上游服务失败: " + err.Error(),
		})
		return
	}

	h.upstreamSelector.MarkUpstreamSuccess(selection.Upstream.ID)
	h.upstreamSelector.MarkAPIKeySuccess(selection.ProviderKey.ID)
	latency := time.Since(startTime).Milliseconds()

	// 转换响应格式
	var openAIResp map[string]interface{}
	if err := json.Unmarshal(resp.Body, &openAIResp); err == nil {
		anthropicResp, _ := provider.ConvertFromOpenAI(openAIResp)
		anthropicResp.Model = req.Model

		// 记录使用日志
		if anthropicResp.Usage != nil {
			h.logUsage(c, selection, modelCfg.Name, &openai.Usage{
				PromptTokens:     anthropicResp.Usage.InputTokens,
				CompletionTokens: anthropicResp.Usage.OutputTokens,
				TotalTokens:      anthropicResp.Usage.InputTokens + anthropicResp.Usage.OutputTokens,
			}, latency, "success", "")
		}

		c.JSON(http.StatusOK, anthropicResp)
		return
	}

	c.Data(resp.StatusCode, "application/json", resp.Body)
}

// handleAnthropicStreamViaOpenAI 通过 OpenAI 兼容模式处理 Anthropic 流式请求
func (h *ProxyHandler) handleAnthropicStreamViaOpenAI(c *gin.Context, client *provider.Client,
	reqBody map[string]interface{}, selection *service.UpstreamSelection, modelCfg *model.Model,
	startTime time.Time, requestID string, modelName string, apiPath string) {

	reqBody["stream"] = true

	resp, err := client.DoStream(c.Request.Context(), provider.Request{
		Method: "POST",
		Path:   apiPath,
		Body:   reqBody,
	})
	if err != nil {
		h.logger.Error("请求上游失败", zap.Error(err), zap.String("request_id", requestID))
		h.upstreamSelector.MarkUpstreamError(selection.Upstream.ID)
		h.upstreamSelector.MarkAPIKeyError(selection.ProviderKey.ID)
		c.JSON(http.StatusBadGateway, gin.H{
			"type":    "upstream_error",
			"message": "请求上游服务失败: " + err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	h.upstreamSelector.MarkUpstreamSuccess(selection.Upstream.ID)
	h.upstreamSelector.MarkAPIKeySuccess(selection.ProviderKey.ID)

	// 设置响应头
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Request-ID", requestID)

	// 流式传输 - 转换 OpenAI 格式为 Anthropic 格式
	reader := bufio.NewReader(resp.Body)
	flusher, _ := c.Writer.(http.Flusher)

	messageID := fmt.Sprintf("msg_%d", time.Now().UnixNano())
	blockIndex := 0
	var totalTokens int

	// 发送 message_start 事件
	startEvent := anthropic.MessageStartEvent{
		Type: "message_start",
		Message: anthropic.MessagesResponse{
			ID:    messageID,
			Type:  "message",
			Role:  "assistant",
			Model: modelName,
			Usage: &anthropic.Usage{},
		},
	}
	startBytes, _ := json.Marshal(startEvent)
	fmt.Fprintf(c.Writer, "data: %s\n\n", string(startBytes))
	flusher.Flush()

	// 发送 content_block_start 事件
	blockStartEvent := anthropic.ContentBlockStartEvent{
		Type:  "content_block_start",
		Index: blockIndex,
		ContentBlock: anthropic.ContentBlock{
			Type: "text",
			Text: "",
		},
	}
	blockBytes, _ := json.Marshal(blockStartEvent)
	fmt.Fprintf(c.Writer, "data: %s\n\n", string(blockBytes))
	flusher.Flush()

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			h.logger.Error("读取流响应失败", zap.Error(err))
			break
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		if data == "[DONE]" {
			// 发送 content_block_stop 事件
			blockStopEvent := anthropic.ContentBlockStopEvent{
				Type:  "content_block_stop",
				Index: blockIndex,
			}
			stopBytes, _ := json.Marshal(blockStopEvent)
			fmt.Fprintf(c.Writer, "data: %s\n\n", string(stopBytes))
			flusher.Flush()

			// 发送 message_delta 事件
			deltaEvent := anthropic.MessageDeltaEvent{
				Type: "message_delta",
				Delta: anthropic.StreamDelta{
					StopReason: "end_turn",
				},
				Usage: anthropic.DeltaUsage{
					OutputTokens: totalTokens,
				},
			}
			deltaBytes, _ := json.Marshal(deltaEvent)
			fmt.Fprintf(c.Writer, "data: %s\n\n", string(deltaBytes))
			flusher.Flush()

			// 发送 message_stop 事件
			msgStopEvent := anthropic.MessageStopEvent{Type: "message_stop"}
			msgStopBytes, _ := json.Marshal(msgStopEvent)
			fmt.Fprintf(c.Writer, "data: %s\n\n", string(msgStopBytes))
			flusher.Flush()
			break
		}

		// 解析 OpenAI chunk
		var chunk openai.StreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err == nil {
			for _, choice := range chunk.Choices {
				if choice.Delta != nil && choice.Delta.Content != "" {
					totalTokens += len(choice.Delta.Content) / 4

					// 发送 content_block_delta 事件
					deltaEvent := anthropic.ContentBlockDeltaEvent{
						Type:  "content_block_delta",
						Index: blockIndex,
						Delta: anthropic.StreamDelta{
							Type: "text_delta",
							Text: choice.Delta.Content,
						},
					}
					deltaBytes, _ := json.Marshal(deltaEvent)
					fmt.Fprintf(c.Writer, "data: %s\n\n", string(deltaBytes))
					flusher.Flush()
				}
			}
		}
	}

	// 记录使用日志
	latency := time.Since(startTime).Milliseconds()
	h.logUsage(c, selection, modelCfg.Name, &openai.Usage{
		PromptTokens:     0,
		CompletionTokens: totalTokens,
		TotalTokens:      totalTokens,
	}, latency, "success", "")
}
