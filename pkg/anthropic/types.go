package anthropic

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
