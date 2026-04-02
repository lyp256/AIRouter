package anthropic

import "encoding/json"

// SystemContent 系统提示词（支持字符串或内容块数组）
type SystemContent []SystemBlock

// UnmarshalJSON 实现自定义反序列化，支持 string 和 array 两种格式
func (s *SystemContent) UnmarshalJSON(data []byte) error {
	// 尝试解析为字符串
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		*s = SystemContent{{Type: "text", Text: str}}
		return nil
	}

	// 解析为数组
	var blocks []SystemBlock
	if err := json.Unmarshal(data, &blocks); err != nil {
		return err
	}
	*s = blocks
	return nil
}

// MarshalJSON 实现自定义序列化，空值输出 null
func (s SystemContent) MarshalJSON() ([]byte, error) {
	if len(s) == 0 {
		return []byte("null"), nil
	}
	return json.Marshal([]SystemBlock(s))
}

// SystemBlock 系统内容块
type SystemBlock struct {
	Type         string                 `json:"type"`                    // text
	Text         string                 `json:"text,omitempty"`          // 文本内容
	CacheControl map[string]interface{} `json:"cache_control,omitempty"` // 缓存控制
}

// ThinkingConfig Extended Thinking 配置
type ThinkingConfig struct {
	Type         string `json:"type"`                    // enabled
	BudgetTokens int    `json:"budget_tokens,omitempty"` // 思考 token 预算
}

// MessagesRequest Anthropic Messages API 请求
type MessagesRequest struct {
	Model         string                 `json:"model"`
	Messages      []Message              `json:"messages"`
	MaxTokens     int                    `json:"max_tokens"`
	System        SystemContent          `json:"system,omitempty"`         // 系统提示词（支持字符串或数组）
	Temperature   *float64               `json:"temperature,omitempty"`    // 采样温度 0-1
	TopP          *float64               `json:"top_p,omitempty"`          // nucleus sampling
	TopK          *int                   `json:"top_k,omitempty"`          // top-k sampling
	StopSequences []string               `json:"stop_sequences,omitempty"` // 停止序列
	Stream        bool                   `json:"stream,omitempty"`
	Metadata      *Metadata              `json:"metadata,omitempty"`
	Tools         []Tool                 `json:"tools,omitempty"`
	ToolChoice    *ToolChoice            `json:"tool_choice,omitempty"`
	Thinking      *ThinkingConfig        `json:"thinking,omitempty"` // Extended Thinking 配置
	Extra         map[string]interface{} `json:"-"`
}

// Message 消息
type Message struct {
	Role    string         `json:"role"` // user 或 assistant
	Content MessageContent `json:"content"`
}

// MessageContent 消息内容（可以是字符串或内容块数组）
type MessageContent interface{}

// ContentBlock 内容块
type ContentBlock struct {
	ID        string       `json:"id,omitempty"` // tool_use 内容块的唯一标识
	Type      string       `json:"type"`         // text, thinking, redacted_thinking, image, tool_use, tool_result
	Text      string       `json:"text,omitempty"`
	Thinking  string       `json:"thinking,omitempty"`  // thinking 类型的思考内容
	Signature string       `json:"signature,omitempty"` // thinking/redacted_thinking 内容块的签名（多轮对话必须回传）
	Data      string       `json:"data,omitempty"`      // redacted_thinking 类型的 base64 编码数据
	Source    *ImageSource `json:"source,omitempty"`
	ToolUseID string       `json:"tool_use_id,omitempty"`
	Content   interface{}  `json:"content,omitempty"` // tool_result 的内容
	Name      string       `json:"name,omitempty"`    // tool_use 的名称
	Input     interface{}  `json:"input,omitempty"`   // tool_use 的输入
}

// ImageSource 图片源
type ImageSource struct {
	Type      string `json:"type"`       // base64
	MediaType string `json:"media_type"` // image/jpeg, image/png, image/gif, image/webp
	Data      string `json:"data"`       // base64 编码的图片数据
}

// Metadata 元数据
type Metadata struct {
	UserID string `json:"user_id,omitempty"`
}

// Tool 工具定义
type Tool struct {
	Name         string                 `json:"name"`
	Type         string                 `json:"type,omitempty"` // 自定义工具默认 custom
	Description  string                 `json:"description,omitempty"`
	InputSchema  map[string]interface{} `json:"input_schema"`
	CacheControl map[string]interface{} `json:"cache_control,omitempty"` // 缓存控制
}

// ToolChoice 工具选择
type ToolChoice struct {
	Type string `json:"type"`           // auto, any, tool
	Name string `json:"name,omitempty"` // 指定工具名称时使用
}

// MessagesResponse Anthropic Messages API 响应
type MessagesResponse struct {
	ID           string                 `json:"id"`
	Type         string                 `json:"type"` // message
	Role         string                 `json:"role"` // assistant
	Content      []ContentBlock         `json:"content"`
	Model        string                 `json:"model"`
	StopReason   string                 `json:"stop_reason,omitempty"` // end_turn, max_tokens, stop_sequence, tool_use, pause_turn, refusal
	StopSequence string                 `json:"stop_sequence,omitempty"`
	Usage        *Usage                 `json:"usage"`
	Error        *ErrorResponse         `json:"error,omitempty"`
	Extra        map[string]interface{} `json:"-"`
}

// Usage 使用量
type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"` // 缓存创建消耗的 token
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`     // 缓存命中节省的 token
}

// ErrorResponse 错误响应
type ErrorResponse struct {
	Type    string `json:"type"` // invalid_request_error, authentication_error, etc.
	Message string `json:"message"`
}

// StreamEvent 流式事件
type StreamEvent struct {
	Type         string            `json:"type"`                    // message_start, content_block_start, ping, content_block_delta, content_block_stop, message_delta, message_stop
	Message      *MessagesResponse `json:"message,omitempty"`       // message_start
	Index        int               `json:"index,omitempty"`         // content_block_start, content_block_delta, content_block_stop
	ContentBlock *ContentBlock     `json:"content_block,omitempty"` // content_block_start
	Delta        *StreamDelta      `json:"delta,omitempty"`         // content_block_delta
	DeltaUsage   *DeltaUsage       `json:"usage,omitempty"`         // message_delta
}

// StreamDelta 流式增量
type StreamDelta struct {
	Type        string `json:"type"` // text_delta, thinking_delta, input_json_delta, signature_delta
	Text        string `json:"text,omitempty"`
	Thinking    string `json:"thinking,omitempty"`  // thinking_delta 类型的思考内容
	Signature   string `json:"signature,omitempty"` // signature_delta 类型的签名增量
	StopReason  string `json:"stop_reason,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
}

// DeltaUsage 增量使用量（message_delta 事件的 usage 字段）
type DeltaUsage struct {
	InputTokens  int `json:"input_tokens"` // 智谱等 API 在 message_delta 返回
	OutputTokens int `json:"output_tokens"`
}

// MessageStartEvent 消息开始事件
type MessageStartEvent struct {
	Type    string           `json:"type"`
	Message MessagesResponse `json:"message"`
}

// ContentBlockStartEvent 内容块开始事件
type ContentBlockStartEvent struct {
	Type         string       `json:"type"`
	Index        int          `json:"index"`
	ContentBlock ContentBlock `json:"content_block"`
}

// ContentBlockDeltaEvent 内容块增量事件
type ContentBlockDeltaEvent struct {
	Type  string      `json:"type"`
	Index int         `json:"index"`
	Delta StreamDelta `json:"delta"`
}

// MessageDeltaEvent 消息增量事件
type MessageDeltaEvent struct {
	Type  string      `json:"type"`
	Delta StreamDelta `json:"delta"`
	Usage DeltaUsage  `json:"usage"`
}

// MessageStopEvent 消息停止事件
type MessageStopEvent struct {
	Type string `json:"type"`
}

// ContentBlockStopEvent 内容块停止事件
type ContentBlockStopEvent struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
}
