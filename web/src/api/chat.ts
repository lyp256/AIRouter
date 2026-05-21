import { useUserStore } from '@/stores/user'
import type { ChatMessage, ChatRequest, ChatResponse, AnthropicRequest, AnthropicStreamEvent } from './types'

export type ChatProtocol = 'openai-chat' | 'openai-responses' | 'anthropic-messages'

export const chatProtocols: { value: ChatProtocol; label: string }[] = [
  { value: 'openai-chat', label: 'OpenAI Chat' },
  { value: 'openai-responses', label: 'OpenAI Responses' },
  { value: 'anthropic-messages', label: 'Anthropic Messages' },
]

export function getChatProtocolLabel(protocol?: ChatProtocol): string {
  return chatProtocols.find(p => p.value === protocol)?.label || chatProtocols[0].label
}

export function isErrorChatMessage(message: ChatMessage): boolean {
  return message.isError === true || (message.role === 'assistant' && message.content.trimStart().startsWith('错误:'))
}

export function getContextMessages(messages: ChatMessage[]): ChatMessage[] {
  return messages.filter(message => !isErrorChatMessage(message))
}

function getChatProtocolEndpoint(protocol: ChatProtocol): string {
  switch (protocol) {
    case 'openai-responses':
      return '/v1/responses'
    case 'anthropic-messages':
      return '/v1/messages'
    default:
      return '/v1/chat/completions'
  }
}

function buildChatProtocolRequest(request: ChatRequest, protocol: ChatProtocol): Record<string, unknown> {
  const contextMessages = getContextMessages(request.messages)

  switch (protocol) {
    case 'openai-responses':
      const systemMessages = contextMessages.filter(message => message.role === 'system').map(message => message.content)
      return {
        model: request.model,
        input: contextMessages.filter(message => message.role !== 'system').map(message => ({
          role: message.role,
          content: message.content
        })),
        instructions: systemMessages.join('\n\n') || undefined,
        temperature: request.temperature,
        top_p: request.top_p,
        max_output_tokens: request.max_tokens,
        stream: true
      }
    case 'anthropic-messages': {
      const systemMessages = contextMessages.filter(message => message.role === 'system').map(message => message.content)
      const messages = contextMessages
        .filter(message => message.role !== 'system')
        .map(message => ({
          role: message.role === 'assistant' ? 'assistant' : 'user',
          content: message.content
        }))

      return {
        model: request.model,
        messages,
        system: systemMessages.join('\n\n') || undefined,
        temperature: request.temperature,
        top_p: request.top_p,
        max_tokens: request.max_tokens || 4096,
        stream: true
      }
    }
    default:
      return {
        ...request,
        messages: contextMessages,
        stream: true
      }
  }
}

function extractStreamDelta(data: string, protocol: ChatProtocol): { content?: string; reasoning?: string; complete?: boolean } {
  const payload = JSON.parse(data)

  if (protocol === 'anthropic-messages') {
    const event = payload as AnthropicStreamEvent
    if (event.type === 'content_block_delta' && event.delta) {
      const deltaType = event.delta.type || 'text_delta'
      return {
        content: deltaType === 'text_delta' || !event.delta.type ? event.delta.text : undefined,
        reasoning: deltaType === 'thinking_delta' ? event.delta.thinking : undefined
      }
    }
    return { complete: event.type === 'message_stop' || !!event.delta?.stop_reason }
  }

  if (protocol === 'openai-responses') {
    return {
      content: payload.delta || payload.text || payload.response?.output_text,
      reasoning: payload.reasoning_delta || payload.reasoning,
      complete: payload.type === 'response.completed' || payload.type === 'response.done'
    }
  }

  const delta = (payload as ChatResponse).choices?.[0]?.delta
  return {
    content: delta?.content,
    reasoning: delta?.reasoning_content
  }
}

/**
 * 发送聊天请求（流式响应）
 * 使用 JWT + KeyID 认证调用 /v1/chat/completions
 */
export async function chatStream(
  request: ChatRequest,
  keyId: string,  // 用户密钥 ID
  onChunk: (text: string) => void,
  onError: (error: Error) => void,
  onComplete: () => void,
  onReasoning?: (text: string) => void, // 思考内容回调
  signal?: AbortSignal, // 终止信号
  protocol: ChatProtocol = 'openai-chat'
): Promise<void> {
  const userStore = useUserStore()
  const token = userStore.token

  if (!token) {
    onError(new Error('未登录'))
    return
  }

  if (!keyId) {
    onError(new Error('请选择密钥'))
    return
  }

  try {
    const response = await fetch(getChatProtocolEndpoint(protocol), {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${token}`,  // JWT token
        'X-Key-ID': keyId                     // Key ID
      },
      body: JSON.stringify(buildChatProtocolRequest(request, protocol)),
      signal // 支持终止请求
    })

    if (!response.ok) {
      const errorData = await response.json().catch(() => ({}))
      throw new Error(errorData.error?.message || errorData.message || `请求失败: ${response.status}`)
    }

    if (!response.body) {
      throw new Error('响应体为空')
    }

    const reader = response.body.getReader()
    const decoder = new TextDecoder()
    let buffer = ''

    while (true) {
      // 检查是否被中止
      if (signal?.aborted) {
        reader.cancel()
        break
      }
      const { done, value } = await reader.read()
      if (done) break

      buffer += decoder.decode(value, { stream: true })
      const lines = buffer.split('\n')
      buffer = lines.pop() || ''

      for (const line of lines) {
        if (line.startsWith('data: ')) {
          const data = line.slice(6).trim()
          if (data === '[DONE]') {
            onComplete()
            return
          }
          try {
            const delta = extractStreamDelta(data, protocol)
            if (delta.reasoning && onReasoning) {
              onReasoning(delta.reasoning)
            }
            if (delta.content) {
              onChunk(delta.content)
            }
            if (delta.complete) {
              onComplete()
              return
            }
          } catch {
            // 忽略解析错误
          }
        }
      }
    }

    onComplete()
  } catch (error) {
    // 如果是用户主动中止，不报错
    if (error instanceof Error && error.name === 'AbortError') {
      onComplete()
      return
    }
    onError(error instanceof Error ? error : new Error(String(error)))
  }
}

/**
 * 发送 Anthropic 消息请求（流式响应）
 * 使用 JWT + KeyID 认证调用 /v1/messages
 */
export async function anthropicStream(
  request: AnthropicRequest,
  keyId: string,
  onChunk: (text: string) => void,
  onError: (error: Error) => void,
  onComplete: () => void,
  onReasoning?: (text: string) => void, // 思考内容回调
  signal?: AbortSignal // 终止信号
): Promise<void> {
  const userStore = useUserStore()
  const token = userStore.token

  if (!token) {
    onError(new Error('未登录'))
    return
  }

  if (!keyId) {
    onError(new Error('请选择密钥'))
    return
  }

  try {
    const response = await fetch('/v1/messages', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${token}`,
        'X-Key-ID': keyId
      },
      body: JSON.stringify({
        ...request,
        stream: true
      }),
      signal // 支持终止请求
    })

    if (!response.ok) {
      const errorData = await response.json().catch(() => ({}))
      throw new Error(errorData.message || errorData.error?.message || `请求失败: ${response.status}`)
    }

    if (!response.body) {
      throw new Error('响应体为空')
    }

    const reader = response.body.getReader()
    const decoder = new TextDecoder()
    let buffer = ''

    while (true) {
      // 检查是否被中止
      if (signal?.aborted) {
        reader.cancel()
        break
      }
      const { done, value } = await reader.read()
      if (done) break

      buffer += decoder.decode(value, { stream: true })
      const lines = buffer.split('\n')
      buffer = lines.pop() || ''

      for (const line of lines) {
        if (line.startsWith('data: ')) {
          const data = line.slice(6).trim()
          if (data === '[DONE]' || data === '') {
            continue
          }
          try {
            const event: AnthropicStreamEvent = JSON.parse(data)
            // 处理不同类型的事件
            if (event.type === 'content_block_delta' && event.delta) {
              const deltaType = event.delta.type || 'text_delta' // 默认为 text_delta，兼容无 type 字段的响应
              // 处理思考内容
              if (deltaType === 'thinking_delta' && event.delta.thinking && onReasoning) {
                onReasoning(event.delta.thinking)
              }
              // 处理正常文本内容（兼容无 type 字段或 type 为 text_delta 的情况）
              if ((deltaType === 'text_delta' || !event.delta.type) && event.delta.text) {
                onChunk(event.delta.text)
              }
            } else if (event.type === 'message_delta') {
              // 消息增量事件，包含停止原因
              if (event.delta?.stop_reason) {
                onComplete()
                return
              }
            } else if (event.type === 'message_stop') {
              onComplete()
              return
            }
          } catch {
            // 忽略解析错误
          }
        }
      }
    }

    onComplete()
  } catch (error) {
    // 如果是用户主动中止，不报错
    if (error instanceof Error && error.name === 'AbortError') {
      onComplete()
      return
    }
    onError(error instanceof Error ? error : new Error(String(error)))
  }
}

/**
 * 发送聊天请求（非流式响应）
 * 使用 JWT + KeyID 认证调用 /v1/chat/completions
 */
export async function chat(request: ChatRequest, keyId: string): Promise<ChatResponse> {
  const userStore = useUserStore()
  const token = userStore.token

  if (!token) {
    throw new Error('未登录')
  }

  if (!keyId) {
    throw new Error('请选择密钥')
  }

  const response = await fetch('/v1/chat/completions', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${token}`,
      'X-Key-ID': keyId
    },
    body: JSON.stringify({
      ...request,
      stream: false
    })
  })

  if (!response.ok) {
    const errorData = await response.json().catch(() => ({}))
    throw new Error(errorData.error?.message || `请求失败: ${response.status}`)
  }

  return response.json()
}

/**
 * 聊天会话管理
 */
export interface ChatSession {
  id: string
  name?: string       // 会话名称（来自第一条用户消息）
  model: string       // 模型名称
  modelId?: string    // 模型 ID（用于恢复选择）
  keyId?: string      // 密钥 ID（用于恢复选择）
  protocol?: ChatProtocol // API 协议（兼容旧会话，默认 OpenAI Chat）
  messages: ChatMessage[]
  createdAt: number
  updatedAt: number
}

/**
 * 从第一条用户消息生成会话名称
 */
export function generateSessionName(messages: ChatMessage[], maxLength: number = 30): string {
  const firstUserMessage = messages.find(m => m.role === 'user')
  if (!firstUserMessage) {
    return '新会话'
  }

  const content = firstUserMessage.content.trim()
  if (content.length <= maxLength) {
    return content
  }

  return content.slice(0, maxLength) + '...'
}

const SESSION_STORAGE_KEY = 'airouter_chat_sessions'

/**
 * 获取所有聊天会话
 */
export function getChatSessions(): ChatSession[] {
  const data = sessionStorage.getItem(SESSION_STORAGE_KEY)
  return data ? JSON.parse(data) : []
}

/**
 * 保存聊天会话
 */
export function saveChatSession(session: ChatSession): void {
  const sessions = getChatSessions()
  const index = sessions.findIndex(s => s.id === session.id)
  if (index >= 0) {
    sessions[index] = session
  } else {
    sessions.unshift(session)
  }
  sessionStorage.setItem(SESSION_STORAGE_KEY, JSON.stringify(sessions))
}

/**
 * 删除聊天会话
 */
export function deleteChatSession(sessionId: string): void {
  const sessions = getChatSessions().filter(s => s.id !== sessionId)
  sessionStorage.setItem(SESSION_STORAGE_KEY, JSON.stringify(sessions))
}

/**
 * 创建新会话
 */
export function createChatSession(model: string): ChatSession {
  return {
    id: `chat_${Date.now()}_${Math.random().toString(36).slice(2, 9)}`,
    model,
    messages: [],
    createdAt: Date.now(),
    updatedAt: Date.now()
  }
}
