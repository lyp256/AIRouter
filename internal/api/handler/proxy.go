package handler

import (
	"bufio"
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

	// 获取模型配置 - OpenAI 协议只匹配 openai 或 openai_compatible 类型
	var modelCfg model.Model
	result := h.db.Where("name = ? AND enabled = ? AND provider_type IN ?", req.Model, true, []string{"openai", "openai_compatible"}).First(&modelCfg)
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
	latency := time.Since(startTime).Milliseconds()

	if err != nil {
		h.logger.Error("请求上游失败", zap.Error(err), zap.String("request_id", requestID))
		h.upstreamSelector.MarkUpstreamError(selection.Upstream.ID)
		h.upstreamSelector.MarkAPIKeyError(selection.ProviderKey.ID)
		// 记录失败日志
		h.logUsage(c, selection, &modelCfg, &openai.Usage{}, latency, 0, latency, "error", 0, err.Error())
		c.JSON(http.StatusBadGateway, gin.H{
			"error": gin.H{
				"message": "请求上游服务失败: " + err.Error(),
				"type":    "upstream_error",
			},
		})
		return
	}

	// 检查上游错误响应
	if resp.StatusCode >= 400 {
		h.logger.Error("上游返回错误",
			zap.Int("status", resp.StatusCode),
			zap.String("body", string(resp.Body)),
			zap.String("request_id", requestID))
		h.upstreamSelector.MarkUpstreamError(selection.Upstream.ID)
		// 尝试解析上游错误响应
		var upstreamErr openai.ErrorResponse
		errMsg := string(resp.Body)
		if err := json.Unmarshal(resp.Body, &upstreamErr); err == nil && upstreamErr.Message != "" {
			errMsg = upstreamErr.Message
			c.JSON(resp.StatusCode, gin.H{
				"error": gin.H{
					"message": upstreamErr.Message,
					"type":    upstreamErr.Type,
					"code":    upstreamErr.Code,
				},
			})
		} else {
			// 无法解析，原样返回
			c.Data(resp.StatusCode, "application/json", resp.Body)
		}
		// 记录失败日志
		h.logUsage(c, selection, &modelCfg, &openai.Usage{}, latency, 0, latency, "error", resp.StatusCode, errMsg)
		return
	}

	// 记录成功
	h.upstreamSelector.MarkUpstreamSuccess(selection.Upstream.ID)
	h.upstreamSelector.MarkAPIKeySuccess(selection.ProviderKey.ID)

	// 解析响应计算 token
	var chatResp openai.ChatCompletionResponse
	if err := json.Unmarshal(resp.Body, &chatResp); err == nil && chatResp.Usage != nil {
		// 记录使用日志
		h.logUsage(c, selection, &modelCfg, chatResp.Usage, latency, 0, latency, "success", 200, "")
	}

	// 返回响应
	c.Data(resp.StatusCode, "application/json", resp.Body)
}

// handleStreamChat 处理流式请求
func (h *ProxyHandler) handleStreamChat(c *gin.Context, client *provider.Client, reqBody map[string]interface{},
	selection *service.UpstreamSelection, modelCfg model.Model, startTime time.Time, requestID string) {

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
		h.logger.Error("请求上游失败", zap.Error(err), zap.String("request_id", requestID))
		h.upstreamSelector.MarkUpstreamError(selection.Upstream.ID)
		h.upstreamSelector.MarkAPIKeyError(selection.ProviderKey.ID)
		// 记录失败日志
		h.logUsage(c, selection, &modelCfg, &openai.Usage{}, latency, 0, latency, "error", 0, err.Error())
		c.JSON(http.StatusBadGateway, gin.H{
			"error": gin.H{
				"message": "请求上游服务失败: " + err.Error(),
				"type":    "upstream_error",
			},
		})
		return
	}
	defer resp.Body.Close()

	// 检查上游错误响应
	if resp.StatusCode >= 400 {
		h.logger.Error("上游返回错误",
			zap.Int("status", resp.StatusCode),
			zap.String("request_id", requestID))
		h.upstreamSelector.MarkUpstreamError(selection.Upstream.ID)
		// 读取错误响应体
		bodyBytes, readErr := io.ReadAll(resp.Body)
		errMsg := "读取上游错误响应失败"
		if readErr != nil {
			c.JSON(resp.StatusCode, gin.H{
				"error": gin.H{
					"message": errMsg,
					"type":    "upstream_error",
				},
			})
		} else {
			// 尝试解析上游错误响应
			var upstreamErr openai.ErrorResponse
			if json.Unmarshal(bodyBytes, &upstreamErr) == nil && upstreamErr.Message != "" {
				errMsg = upstreamErr.Message
				c.JSON(resp.StatusCode, gin.H{
					"error": gin.H{
						"message": upstreamErr.Message,
						"type":    upstreamErr.Type,
						"code":    upstreamErr.Code,
					},
				})
			} else {
				errMsg = string(bodyBytes)
				// 无法解析，原样返回
				c.Data(resp.StatusCode, "application/json", bodyBytes)
			}
		}
		// 记录失败日志
		h.logUsage(c, selection, &modelCfg, &openai.Usage{}, latency, 0, latency, "error", resp.StatusCode, errMsg)
		return
	}

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
	var modelCfg model.Model
	result := h.db.Where("name = ? AND enabled = ? AND provider_type IN ?", req.Model, true, []string{"openai", "openai_compatible"}).First(&modelCfg)
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
	latency := time.Since(startTime).Milliseconds()

	if err != nil {
		h.logger.Error("请求上游失败", zap.Error(err), zap.String("request_id", requestID))
		h.upstreamSelector.MarkUpstreamError(selection.Upstream.ID)
		h.upstreamSelector.MarkAPIKeyError(selection.ProviderKey.ID)
		// 记录失败日志
		h.logUsage(c, selection, &modelCfg, &openai.Usage{}, latency, 0, latency, "error", 0, err.Error())
		c.JSON(http.StatusBadGateway, gin.H{
			"error": gin.H{
				"message": "请求上游服务失败: " + err.Error(),
				"type":    "upstream_error",
			},
		})
		return
	}

	// 检查上游错误响应
	if resp.StatusCode >= 400 {
		h.logger.Error("上游返回错误",
			zap.Int("status", resp.StatusCode),
			zap.String("body", string(resp.Body)),
			zap.String("request_id", requestID))
		h.upstreamSelector.MarkUpstreamError(selection.Upstream.ID)
		// 尝试解析上游错误响应
		var upstreamErr openai.ErrorResponse
		errMsg := string(resp.Body)
		if err := json.Unmarshal(resp.Body, &upstreamErr); err == nil && upstreamErr.Message != "" {
			errMsg = upstreamErr.Message
			c.JSON(resp.StatusCode, gin.H{
				"error": gin.H{
					"message": upstreamErr.Message,
					"type":    upstreamErr.Type,
					"code":    upstreamErr.Code,
				},
			})
		} else {
			// 无法解析，原样返回
			c.Data(resp.StatusCode, "application/json", resp.Body)
		}
		// 记录失败日志
		h.logUsage(c, selection, &modelCfg, &openai.Usage{}, latency, 0, latency, "error", resp.StatusCode, errMsg)
		return
	}

	// 记录成功
	h.upstreamSelector.MarkUpstreamSuccess(selection.Upstream.ID)
	h.upstreamSelector.MarkAPIKeySuccess(selection.ProviderKey.ID)

	// 解析响应计算 token
	var compResp openai.CompletionResponse
	if err := json.Unmarshal(resp.Body, &compResp); err == nil && compResp.Usage != nil {
		// 记录使用日志
		h.logUsage(c, selection, &modelCfg, compResp.Usage, latency, 0, latency, "success", 200, "")
	}

	// 返回响应
	c.Data(resp.StatusCode, "application/json", resp.Body)
}

// handleStreamCompletion 处理流式 Completions 请求
func (h *ProxyHandler) handleStreamCompletion(c *gin.Context, client *provider.Client, reqBody map[string]interface{},
	selection *service.UpstreamSelection, modelCfg model.Model, startTime time.Time, requestID string) {

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
		h.logger.Error("请求上游失败", zap.Error(err), zap.String("request_id", requestID))
		h.upstreamSelector.MarkUpstreamError(selection.Upstream.ID)
		h.upstreamSelector.MarkAPIKeyError(selection.ProviderKey.ID)
		// 记录失败日志
		h.logUsage(c, selection, &modelCfg, &openai.Usage{}, latency, 0, latency, "error", 0, err.Error())
		c.JSON(http.StatusBadGateway, gin.H{
			"error": gin.H{
				"message": "请求上游服务失败: " + err.Error(),
				"type":    "upstream_error",
			},
		})
		return
	}
	defer resp.Body.Close()

	// 检查上游错误响应
	if resp.StatusCode >= 400 {
		h.logger.Error("上游返回错误",
			zap.Int("status", resp.StatusCode),
			zap.String("request_id", requestID))
		h.upstreamSelector.MarkUpstreamError(selection.Upstream.ID)
		// 读取错误响应体
		bodyBytes, readErr := io.ReadAll(resp.Body)
		errMsg := "读取上游错误响应失败"
		if readErr != nil {
			c.JSON(resp.StatusCode, gin.H{
				"error": gin.H{
					"message": errMsg,
					"type":    "upstream_error",
				},
			})
		} else {
			// 尝试解析上游错误响应
			var upstreamErr openai.ErrorResponse
			if json.Unmarshal(bodyBytes, &upstreamErr) == nil && upstreamErr.Message != "" {
				errMsg = upstreamErr.Message
				c.JSON(resp.StatusCode, gin.H{
					"error": gin.H{
						"message": upstreamErr.Message,
						"type":    upstreamErr.Type,
						"code":    upstreamErr.Code,
					},
				})
			} else {
				errMsg = string(bodyBytes)
				// 无法解析，原样返回
				c.Data(resp.StatusCode, "application/json", bodyBytes)
			}
		}
		// 记录失败日志
		h.logUsage(c, selection, &modelCfg, &openai.Usage{}, latency, 0, latency, "error", resp.StatusCode, errMsg)
		return
	}

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
					estimatedTokens += len(choice.Text) / 4
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
	var modelCfg model.Model
	result := h.db.Where("name = ? AND enabled = ? AND provider_type IN ?", req.Model, true, []string{"openai", "openai_compatible"}).First(&modelCfg)
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
	latency := time.Since(startTime).Milliseconds()

	if err != nil {
		h.logger.Error("请求上游失败", zap.Error(err))
		h.upstreamSelector.MarkUpstreamError(selection.Upstream.ID)
		h.upstreamSelector.MarkAPIKeyError(selection.ProviderKey.ID)
		// 记录失败日志
		h.logUsage(c, selection, &modelCfg, &openai.Usage{}, latency, 0, latency, "error", 0, err.Error())
		c.JSON(http.StatusBadGateway, gin.H{
			"error": gin.H{
				"message": "请求上游服务失败: " + err.Error(),
				"type":    "upstream_error",
			},
		})
		return
	}

	// 检查上游错误响应
	if resp.StatusCode >= 400 {
		h.logger.Error("上游返回错误",
			zap.Int("status", resp.StatusCode),
			zap.String("body", string(resp.Body)))
		h.upstreamSelector.MarkUpstreamError(selection.Upstream.ID)
		// 尝试解析上游错误响应
		var upstreamErr openai.ErrorResponse
		errMsg := string(resp.Body)
		if err := json.Unmarshal(resp.Body, &upstreamErr); err == nil && upstreamErr.Message != "" {
			errMsg = upstreamErr.Message
			c.JSON(resp.StatusCode, gin.H{
				"error": gin.H{
					"message": upstreamErr.Message,
					"type":    upstreamErr.Type,
					"code":    upstreamErr.Code,
				},
			})
		} else {
			// 无法解析，原样返回
			c.Data(resp.StatusCode, "application/json", resp.Body)
		}
		// 记录失败日志
		h.logUsage(c, selection, &modelCfg, &openai.Usage{}, latency, 0, latency, "error", resp.StatusCode, errMsg)
		return
	}

	h.upstreamSelector.MarkUpstreamSuccess(selection.Upstream.ID)
	h.upstreamSelector.MarkAPIKeySuccess(selection.ProviderKey.ID)

	// 记录使用日志
	var embResp openai.EmbeddingResponse
	if err := json.Unmarshal(resp.Body, &embResp); err == nil && embResp.Usage != nil {
		h.logUsage(c, selection, &modelCfg, embResp.Usage, latency, 0, latency, "success", 200, "")
	}

	c.Data(resp.StatusCode, "application/json", resp.Body)
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

func requestID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
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
	var modelCfg model.Model
	result := h.db.Where("name = ? AND enabled = ? AND provider_type = ?", req.Model, true, "anthropic").First(&modelCfg)
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

	// 使用原生 Anthropic 客户端
	h.handleAnthropicNative(c, &req, selection, &modelCfg, startTime, requestID)
}

// handleAnthropicNative 使用原生 Anthropic API 处理
func (h *ProxyHandler) handleAnthropicNative(c *gin.Context, req *anthropic.MessagesRequest,
	selection *service.UpstreamSelection, modelCfg *model.Model, startTime time.Time, requestID string) {

	// 获取 API 路径，如果供应商配置了则使用，否则使用默认路径
	apiPath := selection.Provider.APIPath
	if apiPath == "" {
		apiPath = "/v1/messages"
	}

	client := provider.NewAnthropicClient(provider.AnthropicConfig{
		BaseURL: selection.Provider.BaseURL,
		APIKey:  selection.DecryptedKey,
		APIPath: apiPath,
	})

	// 替换为上游实际模型名
	req.Model = selection.Upstream.ProviderModel

	// 处理流式请求
	if req.Stream {
		h.handleAnthropicStream(c, client, req, selection, modelCfg, startTime, requestID)
		return
	}

	// 非流式请求
	resp, err := client.Messages(c.Request.Context(), *req)
	latency := time.Since(startTime).Milliseconds()

	if err != nil {
		h.logger.Error("请求 Anthropic 失败", zap.Error(err), zap.String("request_id", requestID))
		h.upstreamSelector.MarkUpstreamError(selection.Upstream.ID)
		h.upstreamSelector.MarkAPIKeyError(selection.ProviderKey.ID)
		// 记录失败日志
		h.logUsage(c, selection, modelCfg, &openai.Usage{}, latency, 0, latency, "error", 0, err.Error())
		c.JSON(http.StatusBadGateway, gin.H{
			"type":    "upstream_error",
			"message": "请求上游服务失败: " + err.Error(),
		})
		return
	}

	h.upstreamSelector.MarkUpstreamSuccess(selection.Upstream.ID)
	h.upstreamSelector.MarkAPIKeySuccess(selection.ProviderKey.ID)

	// 记录使用日志
	if resp.Usage != nil {
		h.logUsage(c, selection, modelCfg, &openai.Usage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		}, latency, 0, latency, "success", 200, "")
	}

	c.JSON(http.StatusOK, resp)
}

// handleAnthropicStream 处理 Anthropic 流式请求
func (h *ProxyHandler) handleAnthropicStream(c *gin.Context, client *provider.AnthropicClient,
	req *anthropic.MessagesRequest, selection *service.UpstreamSelection, modelCfg *model.Model,
	startTime time.Time, requestID string) {

	resp, err := client.MessagesStream(c.Request.Context(), *req)
	latency := time.Since(startTime).Milliseconds()

	if err != nil {
		h.logger.Error("请求 Anthropic 失败", zap.Error(err), zap.String("request_id", requestID))
		h.upstreamSelector.MarkUpstreamError(selection.Upstream.ID)
		h.upstreamSelector.MarkAPIKeyError(selection.ProviderKey.ID)
		// 记录失败日志
		h.logUsage(c, selection, modelCfg, &openai.Usage{}, latency, 0, latency, "error", 0, err.Error())
		c.JSON(http.StatusBadGateway, gin.H{
			"type":    "upstream_error",
			"message": "请求上游服务失败: " + err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	// 检查上游错误响应
	if resp.StatusCode >= 400 {
		h.logger.Error("上游返回错误",
			zap.Int("status", resp.StatusCode),
			zap.String("request_id", requestID))
		h.upstreamSelector.MarkUpstreamError(selection.Upstream.ID)
		// 读取错误响应体
		bodyBytes, readErr := io.ReadAll(resp.Body)
		errMsg := "读取上游错误响应失败"
		if readErr != nil {
			c.JSON(resp.StatusCode, gin.H{
				"type":    "upstream_error",
				"message": errMsg,
			})
		} else {
			// 尝试解析上游错误响应
			var upstreamErr anthropic.ErrorResponse
			if json.Unmarshal(bodyBytes, &upstreamErr) == nil && upstreamErr.Message != "" {
				errMsg = upstreamErr.Message
				c.JSON(resp.StatusCode, gin.H{
					"type":    upstreamErr.Type,
					"message": upstreamErr.Message,
				})
			} else {
				errMsg = string(bodyBytes)
				// 无法解析，原样返回
				c.Data(resp.StatusCode, "application/json", bodyBytes)
			}
		}
		// 记录失败日志
		h.logUsage(c, selection, modelCfg, &openai.Usage{}, latency, 0, latency, "error", resp.StatusCode, errMsg)
		return
	}

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
}
