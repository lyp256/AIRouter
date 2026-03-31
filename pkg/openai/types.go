package openai

// ChatCompletionRequest Chat 请求
type ChatCompletionRequest struct {
	Model            string                 `json:"model"`
	Messages         []ChatMessage          `json:"messages"`
	Temperature      *float64               `json:"temperature,omitempty"`
	TopP             *float64               `json:"top_p,omitempty"`
	N                *int                   `json:"n,omitempty"`
	Stream           bool                   `json:"stream,omitempty"`
	Stop             interface{}            `json:"stop,omitempty"`
	MaxTokens        *int                   `json:"max_tokens,omitempty"`
	PresencePenalty  *float64               `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64               `json:"frequency_penalty,omitempty"`
	LogitBias        map[string]float64     `json:"logit_bias,omitempty"`
	User             string                 `json:"user,omitempty"`
	Tools            []Tool                 `json:"tools,omitempty"`
	ToolChoice       interface{}            `json:"tool_choice,omitempty"`
	ResponseFormat   *ResponseFormat        `json:"response_format,omitempty"`
	Extra            map[string]interface{} `json:"-"` // 额外字段
}

// ChatMessage 聊天消息
type ChatMessage struct {
	Role             string     `json:"role"`
	Content          string     `json:"content,omitempty"`
	Name             string     `json:"name,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"` // 思考内容（DeepSeek R1 等）
}

// Tool 工具定义
type Tool struct {
	Type     string   `json:"type"`
	Function Function `json:"function"`
}

// Function 函数定义
type Function struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

// ToolCall 工具调用
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall 函数调用
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ResponseFormat 响应格式
type ResponseFormat struct {
	Type string `json:"type"`
}

// ChatCompletionResponse Chat 响应
type ChatCompletionResponse struct {
	ID                string                 `json:"id"`
	Object            string                 `json:"object"`
	Created           int64                  `json:"created"`
	Model             string                 `json:"model"`
	Choices           []ChatChoice           `json:"choices"`
	Usage             *Usage                 `json:"usage,omitempty"`
	SystemFingerprint string                 `json:"system_fingerprint,omitempty"`
	Error             *ErrorResponse         `json:"error,omitempty"`
	Extra             map[string]interface{} `json:"-"`
}

// ChatChoice 选择项
type ChatChoice struct {
	Index        int          `json:"index"`
	Message      *ChatMessage `json:"message,omitempty"`
	Delta        *ChatMessage `json:"delta,omitempty"`
	FinishReason string       `json:"finish_reason"`
	LogProbs     interface{}  `json:"logprobs,omitempty"`
}

// Usage 使用量
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ErrorResponse 错误响应
type ErrorResponse struct {
	Message string      `json:"message"`
	Type    string      `json:"type"`
	Param   interface{} `json:"param,omitempty"`
	Code    string      `json:"code,omitempty"`
}

// ModelsResponse 模型列表响应
type ModelsResponse struct {
	Data []ModelInfo `json:"data"`
}

// ModelInfo 模型信息
type ModelInfo struct {
	ID           string `json:"id"`
	Object       string `json:"object"`
	Created      int64  `json:"created"`
	OwnedBy      string `json:"owned_by"`
	ProviderType string `json:"provider_type,omitempty"` // 供应商类型：openai, anthropic, openai_compatible
}

// EmbeddingRequest Embedding 请求
type EmbeddingRequest struct {
	Model          string   `json:"model"`
	Input          []string `json:"input"`
	EncodingFormat string   `json:"encoding_format,omitempty"`
	Dimensions     int      `json:"dimensions,omitempty"`
	User           string   `json:"user,omitempty"`
}

// EmbeddingResponse Embedding 响应
type EmbeddingResponse struct {
	Object string          `json:"object"`
	Data   []EmbeddingData `json:"data"`
	Model  string          `json:"model"`
	Usage  *Usage          `json:"usage"`
}

// EmbeddingData Embedding 数据
type EmbeddingData struct {
	Object    string      `json:"object"`
	Index     int         `json:"index"`
	Embedding interface{} `json:"embedding"`
}

// StreamChunk 流式响应块
type StreamChunk struct {
	ID                string       `json:"id"`
	Object            string       `json:"object"`
	Created           int64        `json:"created"`
	Model             string       `json:"model"`
	Choices           []ChatChoice `json:"choices"`
	SystemFingerprint string       `json:"system_fingerprint,omitempty"`
	Usage             *Usage       `json:"usage,omitempty"` // 流式最后的 usage（需要 stream_options.include_usage: true）
}

// StreamOptions 流式选项
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage"` // 是否在流式响应最后包含 usage 信息
}

// CompletionRequest Completions API 请求（旧版文本补全）
type CompletionRequest struct {
	Model            string                 `json:"model"`
	Prompt           interface{}            `json:"prompt"` // string 或 []string
	Suffix           string                 `json:"suffix,omitempty"`
	MaxTokens        *int                   `json:"max_tokens,omitempty"`
	Temperature      *float64               `json:"temperature,omitempty"`
	TopP             *float64               `json:"top_p,omitempty"`
	N                *int                   `json:"n,omitempty"`
	Stream           bool                   `json:"stream,omitempty"`
	Logprobs         *int                   `json:"logprobs,omitempty"`
	Echo             bool                   `json:"echo,omitempty"`
	Stop             interface{}            `json:"stop,omitempty"`
	PresencePenalty  *float64               `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64               `json:"frequency_penalty,omitempty"`
	BestOf           *int                   `json:"best_of,omitempty"`
	LogitBias        map[string]float64     `json:"logit_bias,omitempty"`
	User             string                 `json:"user,omitempty"`
	Extra            map[string]interface{} `json:"-"`
}

// CompletionResponse Completions API 响应
type CompletionResponse struct {
	ID                string             `json:"id"`
	Object            string             `json:"object"`
	Created           int64              `json:"created"`
	Model             string             `json:"model"`
	Choices           []CompletionChoice `json:"choices"`
	Usage             *Usage             `json:"usage,omitempty"`
	SystemFingerprint string             `json:"system_fingerprint,omitempty"`
	Error             *ErrorResponse     `json:"error,omitempty"`
}

// CompletionChoice Completions 选择项
type CompletionChoice struct {
	Text         string    `json:"text"`
	Index        int       `json:"index"`
	Logprobs     *Logprobs `json:"logprobs,omitempty"`
	FinishReason string    `json:"finish_reason"`
}

// Logprobs 日志概率
type Logprobs struct {
	Tokens        []string             `json:"tokens,omitempty"`
	TokenLogprobs []float64            `json:"token_logprobs,omitempty"`
	TopLogprobs   []map[string]float64 `json:"top_logprobs,omitempty"`
	TextOffset    []int                `json:"text_offset,omitempty"`
}

// CompletionStreamChunk Completions 流式响应块
type CompletionStreamChunk struct {
	ID                string                   `json:"id"`
	Object            string                   `json:"object"`
	Created           int64                    `json:"created"`
	Model             string                   `json:"model"`
	Choices           []CompletionStreamChoice `json:"choices"`
	SystemFingerprint string                   `json:"system_fingerprint,omitempty"`
}

// CompletionStreamChoice Completions 流式选择项
type CompletionStreamChoice struct {
	Text         string    `json:"text"`
	Index        int       `json:"index"`
	Logprobs     *Logprobs `json:"logprobs,omitempty"`
	FinishReason string    `json:"finish_reason"`
}
