import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { ChatMessage } from '@/api/types'
import { generateSessionName, saveChatSession } from '@/api/chat'

// 后台会话状态
export interface BackgroundSession {
  sessionId: string | null
  modelId: string
  modelName: string // 模型名称，用于 API 调用
  keyId: string
  messages: ChatMessage[]
  streamingContent: string
  streamingReasoning: string
  systemPrompt: string
  temperature: number
  maxTokens: number
  providerType: string // 供应商类型
}

// 模块级 AbortController（避免 Pinia Proxy 问题）
let _abortController: AbortController | null = null

export const useChatStore = defineStore('chat', () => {
  // 后台正在进行的会话
  const backgroundSession = ref<BackgroundSession | null>(null)

  // 是否有后台会话正在进行
  const hasBackgroundSession = ref(false)

  // 设置后台会话
  function setBackgroundSession(session: BackgroundSession) {
    backgroundSession.value = session
    hasBackgroundSession.value = true
  }

  // 更新后台会话的流式内容
  function updateStreamingContent(content: string, reasoning: string) {
    if (backgroundSession.value) {
      backgroundSession.value.streamingContent = content
      backgroundSession.value.streamingReasoning = reasoning
    }
  }

  // 添加消息到后台会话
  function addMessageToBackgroundSession(message: ChatMessage) {
    if (backgroundSession.value) {
      backgroundSession.value.messages.push(message)
    }
  }

  // 完成后台会话并保存到 sessionStorage
  function completeAndSaveBackgroundSession() {
    if (!backgroundSession.value) return

    const bg = backgroundSession.value

    // 保存助手消息（携带模型信息）
    if (bg.streamingContent || bg.streamingReasoning) {
      const assistantMsg: ChatMessage = {
        role: 'assistant',
        content: bg.streamingContent,
        reasoning_content: bg.streamingReasoning || undefined,
        modelName: bg.modelName,
        providerType: bg.providerType
      }
      bg.messages.push(assistantMsg)
    }

    // 保存到 sessionStorage（复用 chat.ts 的存储函数）
    if (bg.messages.length > 0) {
      const sessionName = generateSessionName(bg.messages)
      saveChatSession({
        id: bg.sessionId || `chat_${Date.now()}_${Math.random().toString(36).slice(2, 9)}`,
        name: sessionName,
        model: bg.modelName,
        modelId: bg.modelId,
        keyId: bg.keyId,
        messages: bg.messages,
        createdAt: Date.now(),
        updatedAt: Date.now()
      })
    }

    // 清除后台会话
    backgroundSession.value = null
    hasBackgroundSession.value = false
    _abortController = null
  }

  // 添加错误消息并保存
  function addErrorAndSave(errorMsg: string) {
    if (backgroundSession.value) {
      backgroundSession.value.messages.push({
        role: 'assistant',
        content: `错误: ${errorMsg}`
      })
      completeAndSaveBackgroundSession()
    }
  }

  // 清除后台会话（不保存）
  function clearBackgroundSession() {
    backgroundSession.value = null
    hasBackgroundSession.value = false
    _abortController = null
  }

  // 终止后台请求并保存已接收的内容
  function abortAndSave() {
    if (_abortController) {
      _abortController.abort()
      _abortController = null
    }

    if (backgroundSession.value) {
      const bg = backgroundSession.value
      if (bg.streamingContent || bg.streamingReasoning) {
        bg.messages.push({
          role: 'assistant',
          content: bg.streamingContent || '(已终止)',
          reasoning_content: bg.streamingReasoning || undefined,
          modelName: bg.modelName,
          providerType: bg.providerType
        })
      }
      completeAndSaveBackgroundSession()
    }
  }

  return {
    backgroundSession,
    hasBackgroundSession,
    setBackgroundSession,
    updateStreamingContent,
    addMessageToBackgroundSession,
    completeAndSaveBackgroundSession,
    addErrorAndSave,
    clearBackgroundSession,
    abortAndSave
  }
})

// 模块级函数：设置 AbortController
export function setChatAbortController(controller: AbortController) {
  _abortController = controller
}

// 模块级函数：获取 AbortController
export function getChatAbortController() {
  return _abortController
}

// 模块级函数：终止后台请求
export function abortChatRequest() {
  if (_abortController) {
    _abortController.abort()
    _abortController = null
  }
}
