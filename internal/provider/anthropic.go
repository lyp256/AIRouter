package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/lyp256/airouter/pkg/anthropic"
)

// AnthropicClient Anthropic API 客户端
type AnthropicClient struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	version    string // API 版本
}

// AnthropicConfig Anthropic 客户端配置
type AnthropicConfig struct {
	BaseURL    string
	APIKey     string
	Timeout    time.Duration
	HTTPClient *http.Client
	Version    string // API 版本，默认 2023-06-01
}

// NewAnthropicClient 创建 Anthropic 客户端
func NewAnthropicClient(cfg AnthropicConfig) *AnthropicClient {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 120 * time.Second // Anthropic 响应可能较慢
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: timeout,
		}
	}

	version := cfg.Version
	if version == "" {
		version = "2023-06-01"
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}

	return &AnthropicClient{
		httpClient: httpClient,
		baseURL:    baseURL,
		apiKey:     cfg.APIKey,
		version:    version,
	}
}

// Messages 发送 Messages 请求
func (c *AnthropicClient) Messages(ctx context.Context, req anthropic.MessagesRequest) (*anthropic.MessagesResponse, error) {
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/messages", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("发送请求失败: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode >= 400 {
		var errResp anthropic.MessagesResponse
		if err := json.Unmarshal(bodyBytes, &errResp); err == nil && errResp.Error != nil {
			return nil, fmt.Errorf("API 错误: %s", errResp.Error.Message)
		}
		return nil, fmt.Errorf("请求失败: %d - %s", resp.StatusCode, string(bodyBytes))
	}

	var result anthropic.MessagesResponse
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	return &result, nil
}

// MessagesStream 发送流式 Messages 请求
func (c *AnthropicClient) MessagesStream(ctx context.Context, req anthropic.MessagesRequest) (*http.Response, error) {
	req.Stream = true

	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/messages", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	c.setHeaders(httpReq)
	httpReq.Header.Set("Accept", "text/event-stream")

	return c.httpClient.Do(httpReq)
}

// setHeaders 设置请求头
func (c *AnthropicClient) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", c.version)
}

// ParseStreamEvent 解析流式事件
func ParseStreamEvent(data string) (*anthropic.StreamEvent, error) {
	var event anthropic.StreamEvent
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return nil, err
	}
	return &event, nil
}

// StreamReader 流式读取器
type StreamReader struct {
	reader  *bufio.Reader
	scanner *bufio.Scanner
}

// NewStreamReader 创建流式读取器
func NewStreamReader(body io.Reader) *StreamReader {
	return &StreamReader{
		reader:  bufio.NewReader(body),
		scanner: bufio.NewScanner(body),
	}
}

// ReadEvent 读取下一个事件
func (r *StreamReader) ReadEvent() (*anthropic.StreamEvent, error) {
	for {
		line, err := r.reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return nil, io.EOF
			}
			return nil, err
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "" {
			continue
		}

		return ParseStreamEvent(data)
	}
}

// ConvertToOpenAI 将 Anthropic 消息请求转换为 OpenAI 格式
func ConvertToOpenAI(req *anthropic.MessagesRequest) (messages []map[string]interface{}) {
	messages = make([]map[string]interface{}, 0)

	// 添加系统消息
	if req.System != "" {
		messages = append(messages, map[string]interface{}{
			"role":    "system",
			"content": req.System,
		})
	}

	// 转换消息
	for _, msg := range req.Messages {
		content := msg.Content
		// 如果 content 是字符串，直接使用
		if str, ok := content.(string); ok {
			messages = append(messages, map[string]interface{}{
				"role":    msg.Role,
				"content": str,
			})
		} else if blocks, ok := content.([]interface{}); ok {
			// 如果是内容块数组，提取文本
			textContent := ""
			for _, block := range blocks {
				if b, ok := block.(map[string]interface{}); ok {
					if t, ok := b["type"].(string); ok && t == "text" {
						if text, ok := b["text"].(string); ok {
							textContent += text
						}
					}
				}
			}
			messages = append(messages, map[string]interface{}{
				"role":    msg.Role,
				"content": textContent,
			})
		}
	}

	return messages
}

// ConvertFromOpenAI 将 OpenAI 响应转换为 Anthropic 格式
func ConvertFromOpenAI(openAIResp map[string]interface{}) (*anthropic.MessagesResponse, error) {
	resp := &anthropic.MessagesResponse{
		ID:      fmt.Sprintf("msg_%d", time.Now().UnixNano()),
		Type:    "message",
		Role:    "assistant",
		Content: []anthropic.ContentBlock{},
	}

	// 解析 choices
	if choices, ok := openAIResp["choices"].([]interface{}); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			if message, ok := choice["message"].(map[string]interface{}); ok {
				if content, ok := message["content"].(string); ok {
					resp.Content = append(resp.Content, anthropic.ContentBlock{
						Type: "text",
						Text: content,
					})
				}
			}
			if finishReason, ok := choice["finish_reason"].(string); ok {
				resp.StopReason = convertFinishReason(finishReason)
			}
		}
	}

	// 解析 usage
	if usage, ok := openAIResp["usage"].(map[string]interface{}); ok {
		resp.Usage = &anthropic.Usage{}
		if promptTokens, ok := usage["prompt_tokens"].(float64); ok {
			resp.Usage.InputTokens = int(promptTokens)
		}
		if completionTokens, ok := usage["completion_tokens"].(float64); ok {
			resp.Usage.OutputTokens = int(completionTokens)
		}
	}

	// 解析 model
	if model, ok := openAIResp["model"].(string); ok {
		resp.Model = model
	}

	return resp, nil
}

// convertFinishReason 转换结束原因
func convertFinishReason(reason string) string {
	switch reason {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	default:
		return reason
	}
}
