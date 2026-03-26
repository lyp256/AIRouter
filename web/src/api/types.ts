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

// Model 对外大模型
// 移除了 provider_id, provider_model, api_path 字段（通过 Upstream 关联）
export interface Model {
  id: string
  name: string
  description: string
  input_price: number
  output_price: number
  context_window: number
  enabled: boolean
  created_at: string
  updated_at: string
}

// ModelWithUpstreams 模型及其上游模型
export interface ModelWithUpstreams extends Model {
  upstreams: Upstream[]
}

export interface UsageLog {
  id: string
  user_id: string
  user_key_id: string
  upstream_id: string
  provider_key_id: string
  model: string
  provider_model: string
  provider_name: string
  input_tokens: number
  output_tokens: number
  cost: number
  latency: number
  status: string
  error_message: string
  request_id: string
  created_at: string
}

export interface ApiResponse<T> {
  data: T
  message?: string
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