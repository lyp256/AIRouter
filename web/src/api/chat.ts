import { useUserStore } from '@/stores/user'
import type { ChatMessage, ChatRequest, ChatResponse, AnthropicRequest, AnthropicStreamEvent } from './types'

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
    const response = await fetch('/v1/chat/completions', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${token}`,  // JWT token
        'X-Key-ID': keyId                     // Key ID
      },
      body: JSON.stringify({
        ...request,
        stream: true
      }),
      signal // 支持终止请求
    })

    if (!response.ok) {
      const errorData = await response.json().catch(() => ({}))
      throw new Error(errorData.error?.message || `请求失败: ${response.status}`)
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
            const chunk: ChatResponse = JSON.parse(data)
            const delta = chunk.choices?.[0]?.delta
            if (delta) {
              // 处理思考内容
              if (delta.reasoning_content && onReasoning) {
                onReasoning(delta.reasoning_content)
              }
              // 处理正常内容
              if (delta.content) {
                onChunk(delta.content)
              }
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