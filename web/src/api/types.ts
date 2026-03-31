export interface User {
  id: string
  username: string
  email: string
  role: string
  status: string
  created_at: string
  updated_at: string
}

export interface UserKey {
  id: string
  name: string
  user_id: string
  permissions: string
  rate_limit: number
  quota_limit: number
  quota_used: number
  expired_at: string | null
  status: string
  created_at: string
  updated_at: string
}

export interface Provider {
  id: string
  name: string
  type: string
  base_url: string
  api_path: string
  description: string
  enabled: boolean
  created_at: string
  updated_at: string
}

// ProviderKey 供应商密钥
// 移除了 weight 和 priority 字段（已迁移到 Upstream）
export interface ProviderKey {
  id: string
  provider_id: string
  name: string
  status: string
  quota_limit: number
  quota_used: number
  last_used_at: string | null
  last_error_at: string | null
  created_at: string
  updated_at: string
}

// Upstream 上游模型（新增）
// 对模型供应商模型调用的抽象，是负载均衡的基本单位
export interface Upstream {
  id: string
  model_id: string
  provider_id: string
  provider_key_id: string
  provider_model: string
  weight: number
  priority: number
  status: string
  enabled: boolean
  created_at: string
  updated_at: string
  // 关联信息（仅详情返回时包含）
  provider_name?: string
  provider_key_name?: string
}

// UpstreamTestResult 上游模型测试结果
export interface UpstreamTestResult {
  success: boolean
  latency_ms: number
  first_token_latency_ms: number
  message: string
  upstream_id: string
  provider_name: string
  provider_model: string
  response_content?: string
}

// Model 对外大模型
// 移除了 provider_id, provider_model, api_path 字段（通过 Upstream 关联）
export interface Model {
  id: string
  name: string
  provider_type: string // 供应商类型：openai, anthropic, openai_compatible（必填，创建后不可修改）
  description: string
  input_price: number  // 输入价格（纳 BU/1K token）
  output_price: number // 输出价格（纳 BU/1K token）
  context_window: number
  enabled: boolean
  created_at: string
  updated_at: string
}

// ModelWithUpstreams 模型及其上游模型
export interface ModelWithUpstreams extends Model {
  upstreams: Upstream[]
}

// UsageLog 使用日志（通过 JOIN 查询获取完整数据）
export interface UsageLog {
  id: string
  user_id: string
  username?: string // 关联查询：用户名
  user_key_id: string
  upstream_id: string
  provider_key_id: string
  model: string
  provider_type?: string // 关联查询：协议类型
  provider_model?: string // 关联查询：供应商模型
  provider_name?: string // 关联查询：供应商名称
  input_tokens: number
  output_tokens: number
  cost: number // 费用（纳 BU）
  latency: number
  first_token_latency: number // 首Token延迟(ms)，仅流式请求有效
  total_duration: number // 总耗时(ms)，从请求发起到响应完成
  status: string
  error_message: string
  request_id: string
  created_at: string
}

export interface ApiResponse<T> {
  data: T
  message?: string
}

export interface FilterOptions {
  models: string[]
  provider_types: string[]
  provider_names: string[]
  provider_keys: { id: string; name: string }[]
  statuses: string[]
}

export interface PaginatedResponse<T> {
  data: T[]
  total: number
  page: number
  page_size: number
}

// 聊天相关类型
export interface ChatMessage {
  role: 'system' | 'user' | 'assistant'
  content: string
  reasoning_content?: string // 思考内容（DeepSeek R1 等）
  modelName?: string      // 模型名称
  providerType?: string   // 协议类型：openai | anthropic | openai_compatible
}

export interface ChatRequest {
  model: string
  messages: ChatMessage[]
  stream?: boolean
  temperature?: number
  top_p?: number
  max_tokens?: number
}

export interface ChatChoice {
  index: number
  message?: ChatMessage
  delta?: { content?: string; role?: string; reasoning_content?: string }
  finish_reason?: string
}

export interface ChatUsage {
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
}

export interface ChatResponse {
  id: string
  object: string
  model: string
  choices: ChatChoice[]
  usage?: ChatUsage
  created?: number
}

// OpenAI 兼容的模型信息（/models API 返回格式）
export interface OpenAIModelInfo {
  id: string            // 模型名称
  object: string
  created: number
  owned_by: string
  provider_type?: string // 供应商类型：openai, anthropic, openai_compatible
}

export interface OpenAIModelsResponse {
  data: OpenAIModelInfo[]
}

// ========== Anthropic 协议类型 ==========

export interface AnthropicMessage {
  role: 'user' | 'assistant'
  content: string | AnthropicContentBlock[]
}

export interface AnthropicContentBlock {
  type: 'text' | 'thinking' | 'image' | 'tool_use' | 'tool_result'
  text?: string
  thinking?: string // thinking 类型的内容
  source?: {
    type: 'base64'
    media_type: string
    data: string
  }
}

export interface AnthropicRequest {
  model: string
  messages: AnthropicMessage[]
  max_tokens: number
  system?: string
  stream?: boolean
  temperature?: number
  top_p?: number
  stop_sequences?: string[]
}

export interface AnthropicResponse {
  id: string
  type: 'message'
  role: 'assistant'
  model: string
  content: AnthropicContentBlock[]
  stop_reason: string | null
  stop_sequence: string | null
  usage: {
    input_tokens: number
    output_tokens: number
  }
}

// Anthropic 流式事件类型
export interface AnthropicStreamEvent {
  type: string
  index?: number
  message?: AnthropicResponse
  delta?: {
    type: string // text_delta, thinking_delta
    text?: string
    thinking?: string // thinking_delta 类型的内容
    stop_reason?: string
  }
  content_block?: AnthropicContentBlock
  usage?: {
    input_tokens: number
    output_tokens: number
  }
}