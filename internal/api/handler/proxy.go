package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/bytedance/sonic"
	"github.com/bytedance/sonic/ast"
	"github.com/gin-gonic/gin"
	"github.com/lyp256/airouter/internal/api/middleware"
	"github.com/lyp256/airouter/internal/cache"
	"github.com/lyp256/airouter/internal/config"
	"github.com/lyp256/airouter/internal/model"
	"github.com/lyp256/airouter/internal/service"
	airllm "github.com/lyp256/airouter/pkg/llm"
	llmprovider "github.com/lyp256/airouter/pkg/llm/provider"
	"github.com/lyp256/airouter/pkg/openai"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ProxyHandler 代理处理器
type ProxyHandler struct {
	db               *gorm.DB
	logger           *zap.Logger
	upstreamSelector *service.UpstreamSelector
	retryConfig      *config.RetryConfig
	cache            cache.Cache
}

// NewProxyHandler 创建代理处理器
func NewProxyHandler(db *gorm.DB, logger *zap.Logger, upstreamSelector *service.UpstreamSelector, retryConfig *config.RetryConfig, c cache.Cache) *ProxyHandler {
	return &ProxyHandler{
		db:               db,
		logger:           logger,
		upstreamSelector: upstreamSelector,
		retryConfig:      retryConfig,
		cache:            c,
	}
}

// ChatCompletions Chat Completions API
func (h *ProxyHandler) ChatCompletions(c *gin.Context) {
	rawBody, err := readRawJSONBody(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": "请求参数错误: " + err.Error(),
				"type":    "invalid_request_error",
			},
		})
		return
	}
	model, _ := GetJsonPath(rawBody, "model").String()

	modelCfg, err := h.getModelByName(c.Request.Context(), model)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": "模型不存在或未启用: " + model,
				"type":    "invalid_request_error",
			},
		})
		return
	}

	// 校验用户密钥权限
	if !h.checkModelPermission(c, model) {
		c.JSON(http.StatusForbidden, gin.H{
			"error": gin.H{
				"message": "无权访问模型: " + model,
				"type":    "permission_denied",
			},
		})
		return
	}

	startTime := time.Now()
	requestID := middleware.GetRequestID(c)
	stream, _ := GetJsonPath(rawBody, "stream").Bool()
	h.handleTranslatedWithRetry(c, translatedProxyRequest{
		SourceFormat: sdktranslator.FormatOpenAI,
		ClientModel:  model,
		RawBody:      rawBody,
		Stream:       stream,
		ModelCfg:     modelCfg,
		StartTime:    startTime,
		RequestID:    requestID,
	})
}

// Responses OpenAI Responses API.
func (h *ProxyHandler) Responses(c *gin.Context) {
	rawBody, err := readRawJSONBody(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": "请求参数错误: " + err.Error(),
				"type":    "invalid_request_error",
			},
		})
		return
	}

	modelName := strings.TrimSpace(gjson.GetBytes(rawBody, "model").String())
	if modelName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": "missing required field: model",
				"type":    "invalid_request_error",
				"param":   "model",
			},
		})
		return
	}

	modelCfg, err := h.getModelByName(c.Request.Context(), modelName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": "模型不存在或未启用: " + modelName,
				"type":    "invalid_request_error",
			},
		})
		return
	}

	if !h.checkModelPermission(c, modelName) {
		c.JSON(http.StatusForbidden, gin.H{
			"error": gin.H{
				"message": "无权访问模型: " + modelName,
				"type":    "permission_denied",
			},
		})
		return
	}

	h.handleTranslatedWithRetry(c, translatedProxyRequest{
		SourceFormat: sdktranslator.FormatOpenAIResponse,
		ClientModel:  modelName,
		RawBody:      rawBody,
		Stream:       gjson.GetBytes(rawBody, "stream").Bool(),
		ModelCfg:     modelCfg,
		StartTime:    time.Now(),
		RequestID:    middleware.GetRequestID(c),
	})
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

type translatedProxyRequest struct {
	SourceFormat sdktranslator.Format
	ClientModel  string
	RawBody      []byte
	Stream       bool
	ModelCfg     *model.Model
	StartTime    time.Time
	RequestID    string
	BetaHeader   string
}

func (h *ProxyHandler) handleTranslatedWithRetry(c *gin.Context, req translatedProxyRequest) {
	maxRetries := 3
	if h.retryConfig != nil && h.retryConfig.Enabled {
		maxRetries = h.retryConfig.MaxAttempts
	}

	excludeUpstreams := make([]string, 0)
	var lastErr error
	var lastStatusCode int
	var lastErrMsg string

	for attempt := 1; attempt <= maxRetries; attempt++ {
		selection, err := h.upstreamSelector.SelectUpstream(req.ModelCfg.ID, excludeUpstreams...)
		if err != nil {
			if lastErr != nil {
				break
			}
			writeTranslatedError(c, req.SourceFormat, http.StatusServiceUnavailable, "没有可用的上游模型", "internal_error")
			return
		}

		if req.Stream {
			err = h.handleTranslatedStream(c, req, selection)
		} else {
			err = h.handleTranslatedNonStream(c, req, selection)
		}
		if err == nil {
			return
		}

		h.logger.Warn("协议转换请求上游失败，准备重试",
			zap.Error(err),
			zap.String("request_id", req.RequestID),
			zap.String("upstream_id", selection.Upstream.ID),
			zap.Int("attempt", attempt))

		excludeUpstreams = append(excludeUpstreams, selection.Upstream.ID)
		lastErr = err
		if retryErr, ok := err.(*service.RetryableError); ok {
			lastStatusCode = retryErr.StatusCode
			lastErrMsg = retryErr.Err.Error()
		} else {
			lastErrMsg = err.Error()
		}
	}

	latency := time.Since(req.StartTime).Milliseconds()
	h.logUsage(c, nil, req.ModelCfg, string(req.SourceFormat), &openai.Usage{}, latency, 0, latency, "error", lastStatusCode, lastErrMsg)
	writeTranslatedError(c, req.SourceFormat, http.StatusBadGateway, "请求上游服务失败: "+lastErrMsg, "upstream_error")
}

func (h *ProxyHandler) handleTranslatedNonStream(c *gin.Context, req translatedProxyRequest, selection *service.UpstreamSelection) error {
	resp, err := h.doTranslatedRequest(c.Request.Context(), selection, req, req.BetaHeader)
	latency := time.Since(req.StartTime).Milliseconds()
	if err != nil {
		return err
	}
	if providerStatusCode(*resp) >= 400 {
		return &service.RetryableError{
			Err:        fmt.Errorf("上游返回错误: %s", string(resp.Payload)),
			StatusCode: providerStatusCode(*resp),
		}
	}

	usage := llmprovider.ParseUsage(req.SourceFormat, resp.Payload)
	h.logUsage(c, selection, req.ModelCfg, string(req.SourceFormat), &usage, latency, 0, latency, "success", providerStatusCode(*resp), "")
	c.Data(providerStatusCode(*resp), "application/json", resp.Payload)
	return nil
}

func (h *ProxyHandler) handleTranslatedStream(c *gin.Context, req translatedProxyRequest, selection *service.UpstreamSelection) error {
	resp, err := h.doTranslatedStreamRequest(c.Request.Context(), selection, req, req.BetaHeader)
	latency := time.Since(req.StartTime).Milliseconds()
	if err != nil {
		return err
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Request-ID", req.RequestID)
	copyRateLimitHeaders(c, resp.Headers)

	flusher, _ := c.Writer.(http.Flusher)

	var usage openai.Usage
	var firstTokenLatency int64
	firstTokenRecorded := false

	for chunk := range resp.Chunks {
		if chunk.Err != nil {
			return chunk.Err
		}
		line := bytes.TrimSpace(chunk.Payload)
		if len(line) == 0 {
			continue
		}
		if isSSEMetadataOnly(line) {
			continue
		}

		usage = llmprovider.MergeUsage(usage, llmprovider.ParseUsage(req.SourceFormat, line))
		if !firstTokenRecorded && llmprovider.StreamChunkHasContent(req.SourceFormat, line) {
			firstTokenLatency = time.Since(req.StartTime).Milliseconds()
			firstTokenRecorded = true
		}
		writeSSEPayload(c, line)
		if flusher != nil {
			flusher.Flush()
		}
	}

	if llmprovider.NeedsClientDoneMarker(req.SourceFormat) {
		fmt.Fprint(c.Writer, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}

	totalDuration := time.Since(req.StartTime).Milliseconds()
	h.logUsage(c, selection, req.ModelCfg, string(req.SourceFormat), &usage, latency, firstTokenLatency, totalDuration, "success", http.StatusOK, "")
	return nil
}

func readRawJSONBody(c *gin.Context) ([]byte, error) {
	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, err
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(rawBody))
	if !gjson.ValidBytes(rawBody) {
		return nil, fmt.Errorf("invalid JSON")
	}
	return rawBody, nil
}

func (h *ProxyHandler) doTranslatedRequest(ctx context.Context, selection *service.UpstreamSelection, req translatedProxyRequest, betaHeader string) (*airllm.Response, error) {
	format := llmprovider.FormatForProviderType(selection.Provider.Type)
	executor := providerExecutor(format)
	resp, err := executor.Response(ctx, providerAuth(selection, format, betaHeader), translatedProviderRequest(selection, req))
	if err != nil {
		return nil, translatedExecutorError(err)
	}
	return &resp, nil
}

func (h *ProxyHandler) doTranslatedStreamRequest(ctx context.Context, selection *service.UpstreamSelection, req translatedProxyRequest, betaHeader string) (*airllm.StreamResult, error) {
	format := llmprovider.FormatForProviderType(selection.Provider.Type)
	executor := providerExecutor(format)
	resp, err := executor.Stream(ctx, providerAuth(selection, format, betaHeader), translatedProviderRequest(selection, req))
	if err != nil {
		return nil, translatedExecutorError(err)
	}
	return resp, nil
}

func translatedProviderRequest(selection *service.UpstreamSelection, req translatedProxyRequest) airllm.Request {
	return airllm.Request{
		Model:       selection.Upstream.ProviderModel,
		ClientModel: req.ClientModel,
		Payload:     req.RawBody,
		Format:      req.SourceFormat,
	}
}

func translatedExecutorError(err error) error {
	type statusCoder interface {
		StatusCode() int
	}
	if statusErr, ok := err.(statusCoder); ok {
		return &service.RetryableError{
			Err:        fmt.Errorf("上游返回错误: %s", err.Error()),
			StatusCode: statusErr.StatusCode(),
		}
	}
	return err
}

func providerStatusCode(resp airllm.Response) int {
	if resp.StatusCode != 0 {
		return resp.StatusCode
	}
	return http.StatusOK
}

func providerExecutor(format sdktranslator.Format) airllm.Provider {
	return llmprovider.ProviderForFormat(format)
}

func providerAuth(selection *service.UpstreamSelection, format sdktranslator.Format, betaHeader string) airllm.Auth {
	apiPath := selection.Provider.APIPath
	if apiPath == "" {
		apiPath = llmprovider.DefaultAPIPath(format)
	}
	return providerAuthWithPath(selection, apiPath, betaHeader)
}

func providerAuthWithPath(selection *service.UpstreamSelection, apiPath, betaHeader string) airllm.Auth {
	apiKey := strings.TrimSpace(selection.RawKey)
	if apiKey == "" && selection.ProviderKey != nil {
		apiKey = strings.TrimSpace(selection.ProviderKey.Key)
	}
	attrs := map[string]string{
		"base_url": selection.Provider.BaseURL,
		"api_key":  apiKey,
	}
	if strings.TrimSpace(betaHeader) != "" {
		attrs["anthropic_beta"] = betaHeader
	}
	return airllm.Auth{
		APIPath:    apiPath,
		Attributes: attrs,
	}
}

func writeTranslatedError(c *gin.Context, format sdktranslator.Format, status int, message, typ string) {
	if format == sdktranslator.FormatClaude {
		c.JSON(status, gin.H{"type": typ, "message": message})
		return
	}
	c.JSON(status, gin.H{"error": gin.H{"message": message, "type": typ}})
}

func copyRateLimitHeaders(c *gin.Context, headers http.Header) {
	for key, values := range headers {
		lowerKey := strings.ToLower(key)
		if strings.HasPrefix(lowerKey, "anthropic-ratelimit-") || lowerKey == "retry-after" || lowerKey == "request-id" {
			for _, v := range values {
				c.Header(key, v)
			}
		}
	}
}

func isSSEMetadataOnly(payload []byte) bool {
	hasMetadata := false
	for _, rawLine := range bytes.Split(bytes.TrimSpace(payload), []byte("\n")) {
		line := bytes.TrimSpace(rawLine)
		if len(line) == 0 {
			continue
		}
		if bytes.HasPrefix(line, []byte("data:")) {
			return false
		}
		if bytes.HasPrefix(line, []byte("event:")) || bytes.HasPrefix(line, []byte("id:")) ||
			bytes.HasPrefix(line, []byte("retry:")) || bytes.HasPrefix(line, []byte(":")) {
			hasMetadata = true
			continue
		}
		return false
	}
	return hasMetadata
}

func writeSSEPayload(c *gin.Context, payload []byte) {
	trimmed := bytes.TrimSpace(payload)
	switch {
	case len(trimmed) == 0:
		return
	case bytes.HasPrefix(trimmed, []byte("event:")) ||
		bytes.HasPrefix(trimmed, []byte("id:")) ||
		bytes.HasPrefix(trimmed, []byte("retry:")) ||
		bytes.HasPrefix(trimmed, []byte(":")):
		fmt.Fprintf(c.Writer, "%s\n\n", trimmed)
	case bytes.HasPrefix(trimmed, []byte("data:")):
		fmt.Fprintf(c.Writer, "%s\n\n", trimmed)
	case bytes.Equal(trimmed, []byte("[DONE]")):
		fmt.Fprint(c.Writer, "data: [DONE]\n\n")
	default:
		fmt.Fprintf(c.Writer, "data: %s\n\n", trimmed)
	}
}

// logUsage 记录使用日志
func (h *ProxyHandler) logUsage(c *gin.Context, selection *service.UpstreamSelection, modelCfg *model.Model, protocol string, usage *openai.Usage, latency int64, firstTokenLatency int64, totalDuration int64, status string, upstreamStatusCode int, errMsg string) {
	userID := middleware.GetUserID(c)
	userKeyID := middleware.GetUserKeyID(c)

	// 使用模型配置的价格计算费用（nano credits）
	inputCost := int64(usage.PromptTokens) * modelCfg.InputPrice / 1000
	outputCost := int64(usage.CompletionTokens) * modelCfg.OutputPrice / 1000
	cost := inputCost + outputCost

	// 截断过长的错误信息
	if len(errMsg) > 500 {
		errMsg = errMsg[:497] + "..."
	}

	var upstreamID, providerKeyID, providerType string
	if selection != nil {
		if selection.Upstream != nil {
			upstreamID = selection.Upstream.ID
		}
		if selection.Provider != nil {
			providerType = selection.Provider.Type
		}
		if selection.ProviderKey != nil {
			providerKeyID = selection.ProviderKey.ID
		}
	}

	log := model.UsageLog{
		ID:                 requestID(),
		UserID:             userID,
		UserKeyID:          userKeyID,
		UpstreamID:         upstreamID,
		ProviderKeyID:      providerKeyID,
		Model:              modelCfg.Name, // 保留用于索引优化
		Protocol:           protocol,
		ProviderType:       providerType,
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

// getModelByName 通过模型名称获取模型配置（带缓存）
func (h *ProxyHandler) getModelByName(ctx context.Context, name string) (*model.Model, error) {
	cacheKey := fmt.Sprintf("model:name:%s", name)
	var m model.Model
	if err := h.cache.Once(ctx, cacheKey, &m, 10*time.Minute, func() (interface{}, error) {
		var result model.Model
		if err := h.db.Where("name = ? AND enabled = ?", name, true).First(&result).Error; err != nil {
			return nil, err
		}
		return result, nil
	}); err != nil {
		return nil, err
	}
	return &m, nil
}

// AnthropicMessages Anthropic Messages API
func (h *ProxyHandler) AnthropicMessages(c *gin.Context) {
	rawBody, err := readRawJSONBody(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"type":    "invalid_request_error",
			"message": "请求参数错误: " + err.Error(),
		})
		return
	}

	modelName, _ := GetJsonPath(rawBody, "model").String()
	stream, _ := GetJsonPath(rawBody, "stream").Bool()

	modelCfg, err := h.getModelByName(c.Request.Context(), modelName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"type":    "invalid_request_error",
			"message": "模型不存在或未启用: " + modelName,
		})
		return
	}

	// 校验用户密钥权限
	if !h.checkModelPermission(c, modelName) {
		c.JSON(http.StatusForbidden, gin.H{
			"type":    "permission_denied",
			"message": "无权访问模型: " + modelName,
		})
		return
	}

	startTime := time.Now()
	requestID := middleware.GetRequestID(c)

	h.handleTranslatedWithRetry(c, translatedProxyRequest{
		SourceFormat: sdktranslator.FormatClaude,
		ClientModel:  modelName,
		RawBody:      rawBody,
		Stream:       stream,
		ModelCfg:     modelCfg,
		StartTime:    startTime,
		RequestID:    requestID,
		BetaHeader:   c.GetHeader("anthropic-beta"),
	})
}

func GetJsonPath(rawBody []byte, path ...interface{}) *ast.Node {
	node, err := sonic.Get(rawBody, path...)
	if err != nil {
		return nil
	}
	return &node
}
