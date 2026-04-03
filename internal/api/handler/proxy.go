package handler

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lyp256/airouter/internal/api/middleware"
	"github.com/lyp256/airouter/internal/cache"
	"github.com/lyp256/airouter/internal/config"
	"github.com/lyp256/airouter/internal/model"
	"github.com/lyp256/airouter/internal/provider"
	"github.com/lyp256/airouter/internal/service"
	"github.com/lyp256/airouter/pkg/anthropic"
	"github.com/lyp256/airouter/pkg/openai"
	"github.com/lyp256/airouter/pkg/tokenizer"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ProxyHandler 代理处理器
type ProxyHandler struct {
	db               *gorm.DB
	logger           *zap.Logger
	upstreamSelector *service.UpstreamSelector
	retryService     *service.RetryService
	retryConfig      *config.RetryConfig
	cache            cache.Cache
}

// NewProxyHandler 创建代理处理器
func NewProxyHandler(db *gorm.DB, logger *zap.Logger, upstreamSelector *service.UpstreamSelector, retryConfig *config.RetryConfig, c cache.Cache) *ProxyHandler {
	var retryService *service.RetryService
	if retryConfig != nil && retryConfig.Enabled {
		retryService = service.NewRetryService(retryConfig, nil)
	}
	return &ProxyHandler{
		db:               db,
		logger:           logger,
		upstreamSelector: upstreamSelector,
		retryService:     retryService,
		retryConfig:      retryConfig,
		cache:            c,
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

	// 获取模型配置 - OpenAI 协议只匹配 openai 或 openai_compatible 类型
	modelCfg, err := h.getModelByName(c.Request.Context(), req.Model, []string{"openai", "openai_compatible"})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": "模型不存在或未启用: " + req.Model,
				"type":    "invalid_request_error",
			},
		})
		return
	}

	// 校验用户密钥权限
	if !h.checkModelPermission(c, req.Model) {
		c.JSON(http.StatusForbidden, gin.H{
			"error": gin.H{
				"message": "无权访问模型: " + req.Model,
				"type":    "permission_denied",
			},
		})
		return
	}

	startTime := time.Now()
	requestID := middleware.GetRequestID(c)

	// 处理流式请求
	if req.Stream {
		h.handleStreamChatWithRetry(c, &req, modelCfg, startTime, requestID)
		return
	}

	// 非流式请求 - 支持重试
	h.handleNormalChatWithRetry(c, &req, modelCfg, startTime, requestID)
}

// handleStreamChatWithRetry 处理流式请求（支持初始连接重试）
func (h *ProxyHandler) handleStreamChatWithRetry(c *gin.Context, req *openai.ChatCompletionRequest, modelCfg *model.Model, startTime time.Time, requestID string) {
	maxRetries := 3
	if h.retryConfig != nil && h.retryConfig.Enabled {
		maxRetries = h.retryConfig.MaxAttempts
	}

	excludeUpstreams := make([]string, 0)
	var lastErr error
	var lastStatusCode int
	var lastErrMsg string

	for attempt := 1; attempt <= maxRetries; attempt++ {
		// 选择上游模型（排除已失败的上游）
		selection, err := h.upstreamSelector.SelectUpstream(modelCfg.ID, excludeUpstreams...)
		if err != nil {
			if lastErr != nil {
				break
			}
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
			APIKey:  selection.RawKey,
		})

		// 准备请求
		reqBody := h.prepareRequestBody(req, selection.Upstream)

		err = h.handleStreamChat(c, client, reqBody, selection, *modelCfg, startTime, requestID)
		if err == nil {
			return
		}

		// 发生了错误，检查是否可重试
		h.logger.Warn("流式请求上游失败，准备重试",
			zap.Error(err),
			zap.String("request_id", requestID),
			zap.String("upstream_id", selection.Upstream.ID),
			zap.Int("attempt", attempt))

		_ = h.upstreamSelector.MarkUpstreamError(selection.Upstream.ID)
		excludeUpstreams = append(excludeUpstreams, selection.Upstream.ID)
		lastErr = err

		if retryErr, ok := err.(*service.RetryableError); ok {
			lastStatusCode = retryErr.StatusCode
			lastErrMsg = retryErr.Err.Error()
		} else {
			lastErrMsg = err.Error()
		}
	}

	// 所有重试都失败
	latency := time.Since(startTime).Milliseconds()
	h.logger.Error("流式请求所有上游重试失败",
		zap.String("request_id", requestID),
		zap.Int("attempts", maxRetries),
		zap.String("last_error", lastErrMsg))

	// 记录失败日志
	h.logUsage(c, nil, modelCfg, &openai.Usage{}, latency, 0, latency, "error", lastStatusCode, lastErrMsg)

	c.JSON(http.StatusBadGateway, gin.H{
		"error": gin.H{
			"message": "请求上游服务失败: " + lastErrMsg,
			"type":    "upstream_error",
		},
	})
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

// handleNormalChatWithRetry 处理非流式请求（支持重试和故障转移）
func (h *ProxyHandler) handleNormalChatWithRetry(c *gin.Context, req *openai.ChatCompletionRequest, modelCfg *model.Model, startTime time.Time, requestID string) {
	maxRetries := 3
	if h.retryConfig != nil && h.retryConfig.Enabled {
		maxRetries = h.retryConfig.MaxAttempts
	}

	excludeUpstreams := make([]string, 0)
	var lastErr error
	var lastStatusCode int
	var lastErrMsg string

	for attempt := 1; attempt <= maxRetries; attempt++ {
		// 选择上游模型（排除已失败的上游）
		selection, err := h.upstreamSelector.SelectUpstream(modelCfg.ID, excludeUpstreams...)
		if err != nil {
			if lastErr != nil {
				// 所有上游都已尝试过，返回最后的错误
				break
			}
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
			APIKey:  selection.RawKey,
		})

		// 准备请求
		reqBody := h.prepareRequestBody(req, selection.Upstream)
		reqBody["stream"] = false

		// 获取 API 路径
		apiPath := selection.Provider.APIPath
		if apiPath == "" {
			apiPath = "/v1/chat/completions"
		}

		resp, err := client.Do(c.Request.Context(), provider.Request{
			Method: "POST",
			Path:   apiPath,
			Body:   reqBody,
		})
		latency := time.Since(startTime).Milliseconds()

		if err != nil {
			h.logger.Warn("请求上游失败，准备重试",
				zap.Error(err),
				zap.String("request_id", requestID),
				zap.String("upstream_id", selection.Upstream.ID),
				zap.Int("attempt", attempt))
			_ = h.upstreamSelector.MarkUpstreamError(selection.Upstream.ID)
			excludeUpstreams = append(excludeUpstreams, selection.Upstream.ID)
			lastErr = err
			lastErrMsg = err.Error()
			continue
		}

		// 检查上游错误响应
		if resp.StatusCode >= 400 {
			// 判断是否应该重试（5xx 错误或 429 限流）
			shouldRetry := resp.StatusCode >= 500 || resp.StatusCode == 429
			if shouldRetry && attempt < maxRetries {
				h.logger.Warn("上游返回错误，准备重试",
					zap.Int("status", resp.StatusCode),
					zap.String("request_id", requestID),
					zap.String("upstream_id", selection.Upstream.ID),
					zap.Int("attempt", attempt))
				_ = h.upstreamSelector.MarkUpstreamError(selection.Upstream.ID)
				excludeUpstreams = append(excludeUpstreams, selection.Upstream.ID)
				lastStatusCode = resp.StatusCode
				lastErrMsg = string(resp.Body)
				continue
			}

			// 不可重试的错误或已达到最大重试次数
			h.logger.Error("上游返回错误",
				zap.Int("status", resp.StatusCode),
				zap.String("body", string(resp.Body)),
				zap.String("request_id", requestID))
			_ = h.upstreamSelector.MarkUpstreamError(selection.Upstream.ID)
			excludeUpstreams = append(excludeUpstreams, selection.Upstream.ID)

			var upstreamErr openai.ErrorResponse
			errMsg := string(resp.Body)
			if jsonErr := json.Unmarshal(resp.Body, &upstreamErr); jsonErr == nil && upstreamErr.Message != "" {
				errMsg = upstreamErr.Message
				c.JSON(resp.StatusCode, gin.H{
					"error": gin.H{
						"message": upstreamErr.Message,
						"type":    upstreamErr.Type,
						"code":    upstreamErr.Code,
					},
				})
			} else {
				c.Data(resp.StatusCode, "application/json", resp.Body)
			}
			h.logUsage(c, selection, modelCfg, &openai.Usage{}, latency, 0, latency, "error", resp.StatusCode, errMsg)
			return
		}

		// 成功
		_ = h.upstreamSelector.MarkUpstreamSuccess(selection.Upstream.ID)

		var usage openai.Usage
		var chatResp openai.ChatCompletionResponse
		if jsonErr := json.Unmarshal(resp.Body, &chatResp); jsonErr == nil && chatResp.Usage != nil {
			usage = *chatResp.Usage
		}

		h.logUsage(c, selection, modelCfg, &usage, latency, 0, latency, "success", 200, "")
		c.Data(resp.StatusCode, "application/json", resp.Body)
		return
	}

	// 所有重试都失败
	latency := time.Since(startTime).Milliseconds()
	h.logger.Error("所有上游重试失败",
		zap.String("request_id", requestID),
		zap.Int("attempts", maxRetries),
		zap.String("last_error", lastErrMsg))

	// 记录失败日志
	h.logUsage(c, nil, modelCfg, &openai.Usage{}, latency, 0, latency, "error", lastStatusCode, lastErrMsg)

	errMsg := "请求上游服务失败"
	if lastErrMsg != "" {
		errMsg = lastErrMsg
	}
	statusCode := http.StatusBadGateway
	if lastStatusCode > 0 {
		statusCode = lastStatusCode
	}
	c.JSON(statusCode, gin.H{
		"error": gin.H{
			"message": errMsg,
			"type":    "upstream_error",
		},
	})
}

// handleStreamChat 处理流式请求
func (h *ProxyHandler) handleStreamChat(c *gin.Context, client *provider.Client, reqBody map[string]interface{},
	selection *service.UpstreamSelection, modelCfg model.Model, startTime time.Time, requestID string) error {

	// 确保设置 stream: true
	reqBody["stream"] = true
	// 添加 stream_options 以获取 usage 信息
	reqBody["stream_options"] = map[string]interface{}{
		"include_usage": true,
	}

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
	latency := time.Since(startTime).Milliseconds()

	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// 检查上游错误响应
	if resp.StatusCode >= 400 {
		// 读取错误响应体
		bodyBytes, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("上游返回错误 %d 且读取响应失败: %w", resp.StatusCode, readErr)
		}
		return &service.RetryableError{
			Err:        fmt.Errorf("上游返回错误: %s", string(bodyBytes)),
			StatusCode: resp.StatusCode,
		}
	}

	// 记录成功
	_ = h.upstreamSelector.MarkUpstreamSuccess(selection.Upstream.ID)

	// 设置响应头
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Request-ID", requestID)

	// 流式传输
	reader := bufio.NewReader(resp.Body)
	flusher, _ := c.Writer.(http.Flusher)

	var promptTokens, completionTokens int
	var estimatedTokens int
	var firstTokenLatency int64
	firstTokenRecorded := false

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

		// 解析 chunk
		var chunk openai.StreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err == nil {
			// 提取 usage 信息（最后一个 chunk）
			if chunk.Usage != nil {
				promptTokens = chunk.Usage.PromptTokens
				completionTokens = chunk.Usage.CompletionTokens
			}
			// 如果没有 usage，估算 token
			for _, choice := range chunk.Choices {
				if choice.Delta != nil {
					// 记录首 Token 延迟（包含 text 和 reasoning_content）
					if !firstTokenRecorded && (choice.Delta.Content != "" || choice.Delta.ReasoningContent != "") {
						firstTokenLatency = time.Since(startTime).Milliseconds()
						firstTokenRecorded = true
					}
					if choice.Delta.Content != "" {
						estimatedTokens += len(choice.Delta.Content) / 4 // 粗略估算
					}
					if choice.Delta.ReasoningContent != "" {
						estimatedTokens += len(choice.Delta.ReasoningContent) / 4
					}
				}
			}
		}

		// 转发给客户端
		fmt.Fprintf(c.Writer, "data: %s\n\n", data)
		flusher.Flush()
	}

	// 如果没有从 usage 获取到 token，使用估算值
	if completionTokens == 0 {
		completionTokens = estimatedTokens
	}

	// 记录使用日志
	totalDuration := time.Since(startTime).Milliseconds()
	h.logUsage(c, selection, &modelCfg, &openai.Usage{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
	}, latency, firstTokenLatency, totalDuration, "success", 200, "")

	return nil
}

// Models 模型列表 API
func (h *ProxyHandler) Models(c *gin.Context) {
	var models []model.Model
	h.db.Where("enabled = ?", true).Find(&models)

	data := make([]openai.ModelInfo, len(models))
	for i, m := range models {
		data[i] = openai.ModelInfo{
			ID:           m.Name,
			Object:       "model",
			Created:      m.CreatedAt.Unix(),
			OwnedBy:      "airouter",
			ProviderType: m.ProviderType,
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

	// 获取模型配置 - OpenAI 协议只匹配 openai 或 openai_compatible 类型
	modelCfg, err := h.getModelByName(c.Request.Context(), req.Model, []string{"openai", "openai_compatible"})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": "模型不存在或未启用: " + req.Model,
				"type":    "invalid_request_error",
			},
		})
		return
	}

	// 校验用户密钥权限
	if !h.checkModelPermission(c, req.Model) {
		c.JSON(http.StatusForbidden, gin.H{
			"error": gin.H{
				"message": "无权访问模型: " + req.Model,
				"type":    "permission_denied",
			},
		})
		return
	}

	startTime := time.Now()
	requestID := middleware.GetRequestID(c)

	// 处理流式请求
	if req.Stream {
		h.handleStreamCompletionWithRetry(c, &req, modelCfg, startTime, requestID)
		return
	}

	// 非流式请求（支持重试）
	maxRetries := 3
	if h.retryConfig != nil && h.retryConfig.Enabled {
		maxRetries = h.retryConfig.MaxAttempts
	}

	excludeUpstreams := make([]string, 0)
	var lastErr error
	var lastStatusCode int
	var lastErrMsg string

	for attempt := 1; attempt <= maxRetries; attempt++ {
		// 选择上游模型（排除已失败的上游）
		selection, err := h.upstreamSelector.SelectUpstream(modelCfg.ID, excludeUpstreams...)
		if err != nil {
			if lastErr != nil {
				break
			}
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
			APIKey:  selection.RawKey,
		})

		// 准备请求
		reqBody := h.prepareCompletionRequestBody(&req, selection.Upstream)

		err = h.handleNormalCompletion(c, client, reqBody, selection, *modelCfg, startTime, requestID)
		if err == nil {
			return
		}

		h.logger.Warn("Completions 请求上游失败，准备重试",
			zap.Error(err),
			zap.String("request_id", requestID),
			zap.String("upstream_id", selection.Upstream.ID),
			zap.Int("attempt", attempt))

		_ = h.upstreamSelector.MarkUpstreamError(selection.Upstream.ID)
		excludeUpstreams = append(excludeUpstreams, selection.Upstream.ID)
		lastErr = err

		if retryErr, ok := err.(*service.RetryableError); ok {
			lastStatusCode = retryErr.StatusCode
			lastErrMsg = retryErr.Err.Error()
		} else {
			lastErrMsg = err.Error()
		}
	}

	// 所有重试都失败
	latency := time.Since(startTime).Milliseconds()
	h.logUsage(c, nil, modelCfg, &openai.Usage{}, latency, 0, latency, "error", lastStatusCode, lastErrMsg)
	c.JSON(http.StatusBadGateway, gin.H{
		"error": gin.H{
			"message": "请求上游服务失败: " + lastErrMsg,
			"type":    "upstream_error",
		},
	})
}

// handleStreamCompletionWithRetry 处理流式 Completions 请求（支持初始连接重试）
func (h *ProxyHandler) handleStreamCompletionWithRetry(c *gin.Context, req *openai.CompletionRequest, modelCfg *model.Model, startTime time.Time, requestID string) {
	maxRetries := 3
	if h.retryConfig != nil && h.retryConfig.Enabled {
		maxRetries = h.retryConfig.MaxAttempts
	}

	excludeUpstreams := make([]string, 0)
	var lastErr error
	var lastStatusCode int
	var lastErrMsg string

	for attempt := 1; attempt <= maxRetries; attempt++ {
		// 选择上游模型（排除已失败的上游）
		selection, err := h.upstreamSelector.SelectUpstream(modelCfg.ID, excludeUpstreams...)
		if err != nil {
			if lastErr != nil {
				break
			}
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
			APIKey:  selection.RawKey,
		})

		// 准备请求
		reqBody := h.prepareCompletionRequestBody(req, selection.Upstream)

		err = h.handleStreamCompletion(c, client, reqBody, selection, *modelCfg, startTime, requestID)
		if err == nil {
			return
		}

		// 发生了错误，检查是否可重试
		h.logger.Warn("流式 Completions 请求上游失败，准备重试",
			zap.Error(err),
			zap.String("request_id", requestID),
			zap.String("upstream_id", selection.Upstream.ID),
			zap.Int("attempt", attempt))

		_ = h.upstreamSelector.MarkUpstreamError(selection.Upstream.ID)
		excludeUpstreams = append(excludeUpstreams, selection.Upstream.ID)
		lastErr = err

		if retryErr, ok := err.(*service.RetryableError); ok {
			lastStatusCode = retryErr.StatusCode
			lastErrMsg = retryErr.Err.Error()
		} else {
			lastErrMsg = err.Error()
		}
	}

	// 所有重试都失败
	latency := time.Since(startTime).Milliseconds()
	h.logger.Error("流式 Completions 请求所有上游重试失败",
		zap.String("request_id", requestID),
		zap.Int("attempts", maxRetries),
		zap.String("last_error", lastErrMsg))

	// 记录失败日志
	h.logUsage(c, nil, modelCfg, &openai.Usage{}, latency, 0, latency, "error", lastStatusCode, lastErrMsg)

	c.JSON(http.StatusBadGateway, gin.H{
		"error": gin.H{
			"message": "请求上游服务失败: " + lastErrMsg,
			"type":    "upstream_error",
		},
	})
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
	selection *service.UpstreamSelection, modelCfg model.Model, startTime time.Time, requestID string) error {

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
	latency := time.Since(startTime).Milliseconds()

	if err != nil {
		return err
	}

	// 检查上游错误响应
	if resp.StatusCode >= 400 {
		// 读取错误响应体
		bodyBytes, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("上游返回错误 %d 且读取响应失败: %w", resp.StatusCode, readErr)
		}
		return &service.RetryableError{
			Err:        fmt.Errorf("上游返回错误: %s", string(bodyBytes)),
			StatusCode: resp.StatusCode,
		}
	}

	// 记录成功
	_ = h.upstreamSelector.MarkUpstreamSuccess(selection.Upstream.ID)

	// 解析响应计算 token
	var usage openai.Usage
	var compResp openai.CompletionResponse
	if err := json.Unmarshal(resp.Body, &compResp); err == nil && compResp.Usage != nil {
		usage = *compResp.Usage
	}

	// 记录使用日志（无论是否有 usage 都记录）
	h.logUsage(c, selection, &modelCfg, &usage, latency, 0, latency, "success", 200, "")

	// 返回响应
	c.Data(resp.StatusCode, "application/json", resp.Body)
	return nil
}

// handleStreamCompletion 处理流式 Completions 请求
func (h *ProxyHandler) handleStreamCompletion(c *gin.Context, client *provider.Client, reqBody map[string]interface{},
	selection *service.UpstreamSelection, modelCfg model.Model, startTime time.Time, requestID string) error {

	// 确保设置 stream: true
	reqBody["stream"] = true
	// 添加 stream_options 以获取 usage 信息
	reqBody["stream_options"] = map[string]interface{}{
		"include_usage": true,
	}

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
	latency := time.Since(startTime).Milliseconds()

	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// 检查上游错误响应
	if resp.StatusCode >= 400 {
		// 读取错误响应体
		bodyBytes, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("上游返回错误 %d 且读取响应失败: %w", resp.StatusCode, readErr)
		}
		return &service.RetryableError{
			Err:        fmt.Errorf("上游返回错误: %s", string(bodyBytes)),
			StatusCode: resp.StatusCode,
		}
	}

	// 记录成功
	_ = h.upstreamSelector.MarkUpstreamSuccess(selection.Upstream.ID)

	// 设置响应头
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Request-ID", requestID)

	// 流式传输
	reader := bufio.NewReader(resp.Body)
	flusher, _ := c.Writer.(http.Flusher)

	var promptTokens, completionTokens int
	var estimatedTokens int
	var firstTokenLatency int64
	firstTokenRecorded := false

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

		// 解析 chunk
		var chunk openai.CompletionStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err == nil {
			// Completions API 的 chunk 没有 usage，只能估算
			for _, choice := range chunk.Choices {
				if choice.Text != "" {
					// 记录首 Token 延迟
					if !firstTokenRecorded {
						firstTokenLatency = time.Since(startTime).Milliseconds()
						firstTokenRecorded = true
					}
					estimatedTokens += tokenizer.GetTokens(modelCfg.Name, choice.Text)
				}
			}
		}

		// 转发给客户端
		fmt.Fprintf(c.Writer, "data: %s\n\n", data)
		flusher.Flush()
	}

	// Completions API 不支持 stream_options，使用估算值
	completionTokens = estimatedTokens

	// 记录使用日志
	totalDuration := time.Since(startTime).Milliseconds()
	h.logUsage(c, selection, &modelCfg, &openai.Usage{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
	}, latency, firstTokenLatency, totalDuration, "success", 200, "")

	return nil
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

	// 获取模型配置 - Embeddings 只匹配 openai 或 openai_compatible 类型
	modelCfg, err := h.getModelByName(c.Request.Context(), req.Model, []string{"openai", "openai_compatible"})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": "模型不存在或未启用",
				"type":    "invalid_request_error",
			},
		})
		return
	}

	// 校验用户密钥权限
	if !h.checkModelPermission(c, req.Model) {
		c.JSON(http.StatusForbidden, gin.H{
			"error": gin.H{
				"message": "无权访问模型: " + req.Model,
				"type":    "permission_denied",
			},
		})
		return
	}

	startTime := time.Now()
	maxRetries := 3
	if h.retryConfig != nil && h.retryConfig.Enabled {
		maxRetries = h.retryConfig.MaxAttempts
	}

	excludeUpstreams := make([]string, 0)
	var lastErr error
	var lastStatusCode int
	var lastErrMsg string

	for attempt := 1; attempt <= maxRetries; attempt++ {
		// 选择上游模型（排除已失败的上游）
		selection, err := h.upstreamSelector.SelectUpstream(modelCfg.ID, excludeUpstreams...)
		if err != nil {
			if lastErr != nil {
				break
			}
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
			APIKey:  selection.RawKey,
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

		// 获取 API 路径
		apiPath := selection.Provider.APIPath
		if apiPath == "" {
			apiPath = "/v1/embeddings"
		}

		resp, err := client.Do(c.Request.Context(), provider.Request{
			Method: "POST",
			Path:   apiPath,
			Body:   reqBody,
		})
		latency := time.Since(startTime).Milliseconds()

		if err != nil {
			h.logger.Warn("Embeddings 请求上游失败，准备重试",
				zap.Error(err),
				zap.String("upstream_id", selection.Upstream.ID),
				zap.Int("attempt", attempt))
			_ = h.upstreamSelector.MarkUpstreamError(selection.Upstream.ID)
			excludeUpstreams = append(excludeUpstreams, selection.Upstream.ID)
			lastErr = err
			lastErrMsg = err.Error()
			continue
		}

		if resp.StatusCode >= 400 {
			h.logger.Warn("Embeddings 上游返回错误，准备重试",
				zap.Int("status", resp.StatusCode),
				zap.String("upstream_id", selection.Upstream.ID),
				zap.Int("attempt", attempt))
			_ = h.upstreamSelector.MarkUpstreamError(selection.Upstream.ID)
			excludeUpstreams = append(excludeUpstreams, selection.Upstream.ID)
			lastStatusCode = resp.StatusCode
			lastErrMsg = string(resp.Body)
			continue
		}

		_ = h.upstreamSelector.MarkUpstreamSuccess(selection.Upstream.ID)

		var usage openai.Usage
		var embResp openai.EmbeddingResponse
		if err := json.Unmarshal(resp.Body, &embResp); err == nil && embResp.Usage != nil {
			usage = *embResp.Usage
		}
		h.logUsage(c, selection, modelCfg, &usage, latency, 0, latency, "success", 200, "")

		c.Data(resp.StatusCode, "application/json", resp.Body)
		return
	}

	// 所有重试都失败
	latency := time.Since(startTime).Milliseconds()
	h.logUsage(c, nil, modelCfg, &openai.Usage{}, latency, 0, latency, "error", lastStatusCode, lastErrMsg)
	c.JSON(http.StatusBadGateway, gin.H{
		"error": gin.H{
			"message": "请求上游服务失败: " + lastErrMsg,
			"type":    "upstream_error",
		},
	})
}

// logUsage 记录使用日志
func (h *ProxyHandler) logUsage(c *gin.Context, selection *service.UpstreamSelection, modelCfg *model.Model, usage *openai.Usage, latency int64, firstTokenLatency int64, totalDuration int64, status string, upstreamStatusCode int, errMsg string) {
	userID := middleware.GetUserID(c)
	userKeyID := middleware.GetUserKeyID(c)

	// 使用模型配置的价格计算费用（纳 BU）
	inputCost := int64(usage.PromptTokens) * modelCfg.InputPrice / 1000
	outputCost := int64(usage.CompletionTokens) * modelCfg.OutputPrice / 1000
	cost := inputCost + outputCost

	// 截断过长的错误信息
	if len(errMsg) > 500 {
		errMsg = errMsg[:497] + "..."
	}

	log := model.UsageLog{
		ID:                 requestID(),
		UserID:             userID,
		UserKeyID:          userKeyID,
		UpstreamID:         selection.Upstream.ID,
		ProviderKeyID:      selection.ProviderKey.ID,
		Model:              modelCfg.Name, // 保留用于索引优化
		InputTokens:        usage.PromptTokens,
		OutputTokens:       usage.CompletionTokens,
		Cost:               cost,
		Latency:            int(latency),
		FirstTokenLatency:  int(firstTokenLatency),
		TotalDuration:      int(totalDuration),
		Status:             status,
		UpstreamStatusCode: upstreamStatusCode,
		ErrorMessage:       errMsg,
		RequestID:          middleware.GetRequestID(c),
		CreatedAt:          time.Now(),
	}

	h.db.Create(&log)
}

// checkModelPermission 检查用户是否有权限访问指定模型
func (h *ProxyHandler) checkModelPermission(c *gin.Context, modelName string) bool {
	userKey := middleware.GetUserKey(c)
	if userKey == nil {
		// 没有用户密钥信息（可能是 JWT 认证的管理员），允许访问
		return true
	}

	// 如果权限字段为空，允许访问所有模型
	if userKey.Permissions == "" {
		return true
	}

	// 解析权限配置
	// 格式：models:* 或 models:gpt-4,models:claude-3
	permissions := parsePermissions(userKey.Permissions)

	// 检查是否有通配符权限
	if permissions["models"] == "*" {
		return true
	}

	// 检查是否在允许列表中
	if allowedModels, ok := permissions["models_list"].([]string); ok {
		for _, m := range allowedModels {
			if m == modelName {
				return true
			}
		}
	}

	return false
}

// parsePermissions 解析权限配置字符串
func parsePermissions(permissions string) map[string]interface{} {
	result := make(map[string]interface{})
	if permissions == "" {
		return result
	}

	// 格式：models:* 或 models:gpt-4,claude-3
	// 或 JSON 格式：{"models": ["gpt-4", "claude-3"]}

	// 尝试解析 JSON 格式
	if permissions[0] == '{' {
		var jsonPerms map[string]interface{}
		if err := json.Unmarshal([]byte(permissions), &jsonPerms); err == nil {
			// 处理 JSON 中的 models 数组
			if models, ok := jsonPerms["models"].([]interface{}); ok {
				modelList := make([]string, 0, len(models))
				for _, m := range models {
					if ms, ok := m.(string); ok {
						modelList = append(modelList, ms)
					}
				}
				result["models_list"] = modelList
			}
			return result
		}
	}

	// 解析简单格式 models:* 或 models:gpt-4,claude-3
	parts := strings.Split(permissions, ":")
	if len(parts) == 2 && parts[0] == "models" {
		if parts[1] == "*" {
			result["models"] = "*"
		} else {
			modelList := strings.Split(parts[1], ",")
			result["models_list"] = modelList
		}
	}

	return result
}

func requestID() string {
	// 使用 UUID 替代时间戳，避免高并发下重复
	return fmt.Sprintf("%d%06d", time.Now().UnixNano(), randomInt(100000, 999999))
}

// randomInt 生成指定范围内的随机整数
func randomInt(min, max int) int {
	return min + rand.Intn(max-min+1)
}

// getModelByName 通过模型名称和供应商类型获取模型配置（带缓存）
func (h *ProxyHandler) getModelByName(ctx context.Context, name string, providerTypes []string) (*model.Model, error) {
	cacheKey := fmt.Sprintf("model:name:%s:type:%v", name, providerTypes)
	var m model.Model
	if err := h.cache.Once(ctx, cacheKey, &m, 10*time.Minute, func() (interface{}, error) {
		var result model.Model
		if err := h.db.Where("name = ? AND enabled = ? AND provider_type IN ?", name, true, providerTypes).First(&result).Error; err != nil {
			return nil, err
		}
		return result, nil
	}); err != nil {
		return nil, err
	}
	return &m, nil
}

// InvalidateModelCache 使模型缓存失效
func (h *ProxyHandler) InvalidateModelCache(name string) {
	ctx := context.Background()
	// 清除可能的多种 providerTypes 组合缓存
	for _, types := range [][]string{
		{"openai", "openai_compatible"},
		{"openai"},
		{"anthropic"},
	} {
		_ = h.cache.Delete(ctx, fmt.Sprintf("model:name:%s:type:%v", name, types))
	}
	_ = h.cache.Delete(ctx, "models:enabled")
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

	// 获取模型配置 - Anthropic 协议只匹配 anthropic 类型
	modelCfg, err := h.getModelByName(c.Request.Context(), req.Model, []string{"anthropic"})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"type":    "invalid_request_error",
			"message": "模型不存在或未启用: " + req.Model,
		})
		return
	}

	// 校验用户密钥权限
	if !h.checkModelPermission(c, req.Model) {
		c.JSON(http.StatusForbidden, gin.H{
			"type":    "permission_denied",
			"message": "无权访问模型: " + req.Model,
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

	// 使用原生 Anthropic 客户端
	h.handleAnthropicNative(c, &req, selection, modelCfg, startTime, requestID)
}

// handleAnthropicNative 使用原生 Anthropic API 处理
func (h *ProxyHandler) handleAnthropicNative(c *gin.Context, req *anthropic.MessagesRequest,
	selection *service.UpstreamSelection, modelCfg *model.Model, startTime time.Time, requestID string) {

	// 提取客户端的 anthropic-beta 头用于透传
	betaHeader := c.GetHeader("anthropic-beta")

	// 替换为上游实际模型名
	req.Model = selection.Upstream.ProviderModel

	// 处理流式请求
	if req.Stream {
		excludeUpstreams := make([]string, 0)
		var lastErr error
		var lastStatusCode int
		var lastErrMsg string

		for attempt := 1; attempt <= maxRetries; attempt++ {
			// 选择上游模型（排除已失败的上游）
			var sel *service.UpstreamSelection
			var err error
			if attempt == 1 {
				sel = selection
			} else {
				sel, err = h.upstreamSelector.SelectUpstream(modelCfg.ID, excludeUpstreams...)
				if err != nil {
					if lastErr != nil {
						break
					}
					c.JSON(http.StatusServiceUnavailable, gin.H{
						"type":    "internal_error",
						"message": "没有可用的上游模型",
					})
					return
				}
			}

			apiPath := sel.Provider.APIPath
			if apiPath == "" {
				apiPath = "/v1/messages"
			}
			client := provider.NewAnthropicClient(provider.AnthropicConfig{
				BaseURL:    sel.Provider.BaseURL,
				APIKey:     sel.RawKey,
				APIPath:    apiPath,
				BetaHeader: betaHeader,
			})

			// 替换为当前选中的模型
			req.Model = sel.Upstream.ProviderModel

			err = h.handleAnthropicStream(c, client, req, sel, modelCfg, startTime, requestID)
			if err == nil {
				return
			}

			// 发生了错误，检查是否可重试
			h.logger.Warn("Anthropic 流式请求上游失败，准备重试",
				zap.Error(err),
				zap.String("request_id", requestID),
				zap.String("upstream_id", sel.Upstream.ID),
				zap.Int("attempt", attempt))

			_ = h.upstreamSelector.MarkUpstreamError(sel.Upstream.ID)
			excludeUpstreams = append(excludeUpstreams, sel.Upstream.ID)
			lastErr = err

			if retryErr, ok := err.(*service.RetryableError); ok {
				lastStatusCode = retryErr.StatusCode
				lastErrMsg = retryErr.Err.Error()
			} else {
				lastErrMsg = err.Error()
			}
		}

		// 所有重试都失败
		latency := time.Since(startTime).Milliseconds()
		h.logger.Error("请求 Anthropic 所有流式重试失败",
			zap.String("request_id", requestID),
			zap.Int("last_status", lastStatusCode),
			zap.String("last_error", lastErrMsg))
		h.logUsage(c, selection, modelCfg, &openai.Usage{}, latency, 0, latency, "error", lastStatusCode, lastErrMsg)
		c.JSON(http.StatusBadGateway, gin.H{
			"type":    "upstream_error",
			"message": "请求上游服务失败: " + lastErrMsg,
		})
		return
	}

	// 非流式请求（支持重试）
	maxRetries := 3
	if h.retryConfig != nil && h.retryConfig.Enabled {
		maxRetries = h.retryConfig.MaxAttempts
	}

	excludeUpstreams := make([]string, 0)
	var lastErr error
	var lastErrMsg string
	var lastStatusCode int

	for attempt := 1; attempt <= maxRetries; attempt++ {
		// 选择上游模型（排除已失败的上游）
		var sel *service.UpstreamSelection
		var err error
		if attempt == 1 {
			sel = selection
		} else {
			sel, err = h.upstreamSelector.SelectUpstream(modelCfg.ID, excludeUpstreams...)
			if err != nil {
				if lastErr != nil {
					break
				}
				c.JSON(http.StatusServiceUnavailable, gin.H{
					"type":    "internal_error",
					"message": "没有可用的上游模型",
				})
				return
			}
		}

		apiPath := sel.Provider.APIPath
		if apiPath == "" {
			apiPath = "/v1/messages"
		}

		client := provider.NewAnthropicClient(provider.AnthropicConfig{
			BaseURL:    sel.Provider.BaseURL,
			APIKey:     sel.RawKey,
			APIPath:    apiPath,
			BetaHeader: betaHeader,
		})

		resp, err := client.Messages(c.Request.Context(), *req)
		latency := time.Since(startTime).Milliseconds()

		if err != nil {
			h.logger.Warn("请求 Anthropic 失败，准备重试",
				zap.Error(err),
				zap.String("request_id", requestID),
				zap.String("upstream_id", sel.Upstream.ID),
				zap.Int("attempt", attempt))
			_ = h.upstreamSelector.MarkUpstreamError(sel.Upstream.ID)
			excludeUpstreams = append(excludeUpstreams, sel.Upstream.ID)
			lastErr = err
			lastErrMsg = err.Error()
			continue
		}

		_ = h.upstreamSelector.MarkUpstreamSuccess(sel.Upstream.ID)

		// 记录使用日志
		var usage openai.Usage
		if resp.Usage != nil {
			usage = openai.Usage{
				PromptTokens:     resp.Usage.InputTokens,
				CompletionTokens: resp.Usage.OutputTokens,
				TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
			}
		}
		h.logUsage(c, sel, modelCfg, &usage, latency, 0, latency, "success", 200, "")

		c.JSON(http.StatusOK, resp)
		return
	}

	// 所有重试都失败
	latency := time.Since(startTime).Milliseconds()
	h.logger.Error("请求 Anthropic 所有重试失败",
		zap.String("request_id", requestID),
		zap.Int("last_status", lastStatusCode),
		zap.String("last_error", lastErrMsg))
	h.logUsage(c, selection, modelCfg, &openai.Usage{}, latency, 0, latency, "error", lastStatusCode, lastErrMsg)
	c.JSON(http.StatusBadGateway, gin.H{
		"type":    "upstream_error",
		"message": "请求上游服务失败: " + lastErrMsg,
	})
}

// handleAnthropicStream 处理 Anthropic 流式请求
func (h *ProxyHandler) handleAnthropicStream(c *gin.Context, client *provider.AnthropicClient,
	req *anthropic.MessagesRequest, selection *service.UpstreamSelection, modelCfg *model.Model,
	startTime time.Time, requestID string) error {

	resp, err := client.MessagesStream(c.Request.Context(), *req)
	latency := time.Since(startTime).Milliseconds()

	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// 检查上游错误响应
	if resp.StatusCode >= 400 {
		// 读取错误响应体
		bodyBytes, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("上游返回错误 %d 且读取响应失败: %w", resp.StatusCode, readErr)
		}
		return &service.RetryableError{
			Err:        fmt.Errorf("上游返回错误: %s", string(bodyBytes)),
			StatusCode: resp.StatusCode,
		}
	}

	// 记录成功
	_ = h.upstreamSelector.MarkUpstreamSuccess(selection.Upstream.ID)

	// 设置响应头
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Request-ID", requestID)

	// 透传上游限流相关响应头
	for key, values := range resp.Header {
		lowerKey := strings.ToLower(key)
		if strings.HasPrefix(lowerKey, "anthropic-ratelimit-") || lowerKey == "retry-after" || lowerKey == "request-id" {
			for _, v := range values {
				c.Header(key, v)
			}
		}
	}

	// 流式传输
	reader := bufio.NewReader(resp.Body)
	flusher, _ := c.Writer.(http.Flusher)

	var promptTokens, completionTokens int
	var firstTokenLatency int64
	firstTokenRecorded := false

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

		// 解析事件提取 token 信息
		var event anthropic.StreamEvent
		if err := json.Unmarshal([]byte(data), &event); err == nil {
			// 从 message_start 事件获取 input_tokens
			if event.Type == "message_start" && event.Message != nil && event.Message.Usage != nil {
				promptTokens = event.Message.Usage.InputTokens
			}
			// 从 message_delta 事件获取 token（智谱等 API 在此返回完整统计）
			if event.Type == "message_delta" && event.DeltaUsage != nil {
				// 如果 message_delta 包含 input_tokens，使用它（覆盖 message_start 的值）
				if event.DeltaUsage.InputTokens > 0 {
					promptTokens = event.DeltaUsage.InputTokens
				}
				completionTokens = event.DeltaUsage.OutputTokens
			}
			// 记录首 Token 延迟（在 content_block_delta 事件中，包含 text 和 thinking）
			if !firstTokenRecorded && event.Type == "content_block_delta" && event.Delta != nil && (event.Delta.Text != "" || event.Delta.Thinking != "") {
				firstTokenLatency = time.Since(startTime).Milliseconds()
				firstTokenRecorded = true
			}
		}

		// 转发给客户端
		fmt.Fprintf(c.Writer, "data: %s\n\n", data)
		flusher.Flush()
	}

	// 记录使用日志
	totalDuration := time.Since(startTime).Milliseconds()
	h.logUsage(c, selection, modelCfg, &openai.Usage{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
	}, latency, firstTokenLatency, totalDuration, "success", 200, "")

	return nil
}
