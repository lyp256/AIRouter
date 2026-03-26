package anthropic

// MessagesRequest Anthropic Messages API 请求
type MessagesRequest struct {
	Model         string                 `json:"model"`
	Messages      []Message              `json:"messages"`
	MaxTokens     int                    `json:"max_tokens"`
	System        string                 `json:"system,omitempty"`         // 系统提示词
	Temperature   *float64               `json:"temperature,omitempty"`    // 采样温度 0-1
	TopP          *float64               `json:"top_p,omitempty"`          // nucleus sampling
	TopK          *int                   `json:"top_k,omitempty"`          // top-k sampling
	StopSequences []string               `json:"stop_sequences,omitempty"` // 停止序列
	Stream        bool                   `json:"stream,omitempty"`
	Metadata      *Metadata              `json:"metadata,omitempty"`
	Tools         []Tool                 `json:"tools,omitempty"`
	ToolChoice    *ToolChoice            `json:"tool_choice,omitempty"`
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
	Type      string       `json:"type"` // text, image, tool_use, tool_result
	Text      string       `json:"text,omitempty"`
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
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"input_schema"`
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
	StopReason   string                 `json:"stop_reason,omitempty"` // end_turn, max_tokens, stop_sequence, tool_use
	StopSequence string                 `json:"stop_sequence,omitempty"`
	Usage        *Usage                 `json:"usage"`
	Error        *ErrorResponse         `json:"error,omitempty"`
	Extra        map[string]interface{} `json:"-"`
}

// Usage 使用量
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
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
	Type        string `json:"type"` // text_delta, input_json_delta
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	StopReason  string `json:"stop_reason,omitempty"`
}

// DeltaUsage 增量使用量
type DeltaUsage struct {
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
