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
	apiPath    string // API 路径，默认 /v1/messages
	betaHeader string // anthropic-beta 头（透传客户端的 beta 功能标记）
}

// AnthropicConfig Anthropic 客户端配置
type AnthropicConfig struct {
	BaseURL    string
	APIKey     string
	Timeout    time.Duration
	HTTPClient *http.Client
	Version    string // API 版本，默认 2023-06-01
	APIPath    string // API 路径，默认 /v1/messages
	BetaHeader string // anthropic-beta 头（透传客户端的 beta 功能标记）
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

	apiPath := cfg.APIPath
	if apiPath == "" {
		apiPath = "/v1/messages"
	}

	return &AnthropicClient{
		httpClient: httpClient,
		baseURL:    baseURL,
		apiKey:     cfg.APIKey,
		version:    version,
		apiPath:    apiPath,
		betaHeader: cfg.BetaHeader,
	}
}

// Messages 发送 Messages 请求
func (c *AnthropicClient) Messages(ctx context.Context, req anthropic.MessagesRequest) (*anthropic.MessagesResponse, error) {
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	// 使用配置的 apiPath
	apiURL := c.baseURL + c.apiPath
	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(bodyBytes))
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

	// 使用配置的 apiPath
	apiURL := c.baseURL + c.apiPath
	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(bodyBytes))
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
	if c.betaHeader != "" {
		req.Header.Set("anthropic-beta", c.betaHeader)
	}
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
