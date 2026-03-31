<script setup lang="ts">
import { ref, onMounted, computed, nextTick, watch } from 'vue'
import { marked, type Tokens } from 'marked'
import hljs from 'highlight.js/lib/core'
import 'highlight.js/styles/github-dark.css'
// 按需注册常用语言（避免全量导入 190+ 语言导致 chunk 过大）
import javascript from 'highlight.js/lib/languages/javascript'
import typescript from 'highlight.js/lib/languages/typescript'
import python from 'highlight.js/lib/languages/python'
import go from 'highlight.js/lib/languages/go'
import json from 'highlight.js/lib/languages/json'
import bash from 'highlight.js/lib/languages/bash'
import css from 'highlight.js/lib/languages/css'
import xml from 'highlight.js/lib/languages/xml'
import markdown from 'highlight.js/lib/languages/markdown'
import sql from 'highlight.js/lib/languages/sql'
import java from 'highlight.js/lib/languages/java'
import cpp from 'highlight.js/lib/languages/cpp'
import rust from 'highlight.js/lib/languages/rust'
import yaml from 'highlight.js/lib/languages/yaml'
import shell from 'highlight.js/lib/languages/shell'

hljs.registerLanguage('javascript', javascript)
hljs.registerLanguage('typescript', typescript)
hljs.registerLanguage('python', python)
hljs.registerLanguage('go', go)
hljs.registerLanguage('json', json)
hljs.registerLanguage('bash', bash)
hljs.registerLanguage('css', css)
hljs.registerLanguage('html', xml)
hljs.registerLanguage('xml', xml)
hljs.registerLanguage('markdown', markdown)
hljs.registerLanguage('sql', sql)
hljs.registerLanguage('java', java)
hljs.registerLanguage('cpp', cpp)
hljs.registerLanguage('c', cpp)
hljs.registerLanguage('rust', rust)
hljs.registerLanguage('yaml', yaml)
hljs.registerLanguage('shell', shell)
import { modelApi } from '@/api/model'
import { userApi } from '@/api/user'
import { chatStream, anthropicStream, getChatSessions, deleteChatSession, saveChatSession, generateSessionName } from '@/api/chat'
import type { Model, ChatMessage, UserKey } from '@/api/types'
import { useChatStore, setChatAbortController, getChatAbortController } from '@/stores/chat'
import { sanitizeHtml } from '@/utils/sanitize'

// 配置 marked 使用 highlight.js 语法高亮
const renderer = {
  code(token: Tokens.Code) {
    const code = token.text
    const language = token.lang
    if (language && hljs.getLanguage(language)) {
      try {
        const highlighted = hljs.highlight(code, { language }).value
        return `<pre><code class="hljs language-${language}">${highlighted}</code></pre>`
      } catch {
        // 忽略错误
      }
    }
    const highlighted = hljs.highlightAuto(code).value
    return `<pre><code class="hljs">${highlighted}</code></pre>`
  }
}
marked.use({ renderer })
marked.setOptions({
  breaks: true,
  gfm: true
})

// 使用 chat store
const chatStore = useChatStore()

// 状态
const models = ref<Model[]>([])
const currentModelId = ref('') // 当前选中的模型 ID
const userKeys = ref<UserKey[]>([])
const selectedKeyId = ref('')
const messages = ref<ChatMessage[]>([])
const inputMessage = ref('')
const isLoading = ref(false)
const streamingContent = ref('')
const streamingReasoning = ref('') // 流式思考内容
const messagesContainer = ref<HTMLElement | null>(null)
const textarea = ref<HTMLTextAreaElement | null>(null)

// 终止控制器
const abortController = ref<AbortController | null>(null)

// 用户是否在底部（用于控制自动滚动）
const isUserAtBottom = ref(true)
// 是否正在程序控制滚动（避免与用户滚动混淆）
const isAutoScrolling = ref(false)

// 折叠状态
const expandedReasoning = ref<Set<number>>(new Set())

// 会话管理
const sessions = ref(getChatSessions())
const currentSessionId = ref<string | null>(null)

// 设置参数
const temperature = ref(0.7)
const maxTokens = ref(4096)
const showSettings = ref(false)

// 系统提示词
const systemPrompt = ref('你是一个有帮助的 AI 助手。')

// 计算当前选中的模型
const currentModel = computed(() => {
  return models.value.find(m => m.id === currentModelId.value)
})

// 计算当前模型的供应商类型
const currentProviderType = computed(() => {
  return currentModel.value?.provider_type || 'openai'
})

// 计算当前显示的消息
const displayMessages = computed(() => {
  const result: ChatMessage[] = []
  if (systemPrompt.value) {
    result.push({ role: 'system', content: systemPrompt.value })
  }
  result.push(...messages.value)
  return result
})

// 加载模型列表
async function loadModels() {
  try {
    const response = await modelApi.adminList()
    models.value = response.data.filter((m: Model) => m.enabled)
    if (models.value.length > 0 && !currentModelId.value) {
      currentModelId.value = models.value[0].id
    }
  } catch (error) {
    console.error('加载模型失败:', error)
  }
}

// 加载用户密钥列表
async function loadUserKeys() {
  try {
    const response = await userApi.getMyKeys()
    userKeys.value = response.data.filter((k: UserKey) => k.status === 'active')
    if (userKeys.value.length > 0 && !selectedKeyId.value) {
      selectedKeyId.value = userKeys.value[0].id
    }
  } catch (error) {
    console.error('加载密钥失败:', error)
  }
}

// 发送消息
async function sendMessage() {
  const content = inputMessage.value.trim()
  if (!content || isLoading.value || !currentModelId.value || !selectedKeyId.value) return

  const model = currentModel.value
  if (!model) return

  // 创建会话 ID（如果还没有）
  if (!currentSessionId.value) {
    currentSessionId.value = `chat_${Date.now()}_${Math.random().toString(36).slice(2, 9)}`
  }

  // 添加用户消息（携带模型信息）
  const userMessage: ChatMessage = {
    role: 'user',
    content,
    modelName: model.name,
    providerType: currentProviderType.value
  }
  messages.value.push(userMessage)
  inputMessage.value = ''
  isLoading.value = true
  streamingContent.value = ''
  streamingReasoning.value = ''
  // 发送新消息时重置到底部
  isUserAtBottom.value = true

  // 立即保存会话（会话名称从第一条用户消息生成）
  saveChatSession({
    id: currentSessionId.value,
    name: generateSessionName(messages.value),
    model: model.name,
    modelId: currentModelId.value,
    keyId: selectedKeyId.value,
    messages: [...messages.value],
    createdAt: Date.now(),
    updatedAt: Date.now()
  })
  refreshSessions()

  // 创建终止控制器
  const controller = new AbortController()
  abortController.value = controller
  setChatAbortController(controller)

  // 保存后台会话状态
  chatStore.setBackgroundSession({
    sessionId: currentSessionId.value,
    modelId: currentModelId.value,
    modelName: model.name,
    keyId: selectedKeyId.value,
    messages: [...messages.value],
    streamingContent: '',
    streamingReasoning: '',
    systemPrompt: systemPrompt.value,
    temperature: temperature.value,
    maxTokens: maxTokens.value,
    providerType: currentProviderType.value
  })

  // 自动调整输入框高度
  if (textarea.value) {
    textarea.value.style.height = 'auto'
  }

  // 回调函数 - 更新 store 中的状态
  const onChunk = (text: string) => {
    if (chatStore.backgroundSession) {
      chatStore.backgroundSession.streamingContent += text
      // 如果当前显示的是这个会话，同步更新组件状态
      if (currentSessionId.value === chatStore.backgroundSession.sessionId) {
        streamingContent.value = chatStore.backgroundSession.streamingContent
        scrollToBottom()
      }
    }
  }

  const onError = (error: Error) => {
    // 先记录会话 ID，因为 addErrorAndSave 会清除 backgroundSession
    const bgSessionId = chatStore.backgroundSession?.sessionId
    chatStore.addErrorAndSave(error.message)
    // 如果当前显示的是这个会话，更新组件状态
    if (bgSessionId === currentSessionId.value) {
      messages.value.push({ role: 'assistant', content: `错误: ${error.message}` })
      isLoading.value = false
      streamingContent.value = ''
      streamingReasoning.value = ''
    }
    refreshSessions()
  }

  const onComplete = () => {
    // 先获取助手回复内容
    const bg = chatStore.backgroundSession
    if (bg && (bg.streamingContent || bg.streamingReasoning)) {
      // 添加助手消息到本地消息列表
      const assistantMsg: ChatMessage = {
        role: 'assistant',
        content: bg.streamingContent,
        reasoning_content: bg.streamingReasoning || undefined,
        modelName: bg.modelName,
        providerType: bg.providerType
      }
      messages.value.push(assistantMsg)
    }
    // 保存并清除后台会话
    chatStore.completeAndSaveBackgroundSession()
    // 更新组件状态
    isLoading.value = false
    streamingContent.value = ''
    streamingReasoning.value = ''
    abortController.value = null
    refreshSessions()
  }

  const onReasoning = (text: string) => {
    if (chatStore.backgroundSession) {
      chatStore.backgroundSession.streamingReasoning += text
      // 如果当前显示的是这个会话，同步更新组件状态
      if (currentSessionId.value === chatStore.backgroundSession.sessionId) {
        streamingReasoning.value = chatStore.backgroundSession.streamingReasoning
        scrollToBottom()
      }
    }
  }

  try {
    // 根据供应商类型选择协议
    if (currentProviderType.value === 'anthropic') {
      // 使用 Anthropic 协议
      // 转换消息格式：过滤掉 system 消息，单独传递
      const anthropicMessages = displayMessages.value
        .filter(m => m.role !== 'system')
        .map(m => ({
          role: m.role as 'user' | 'assistant',
          content: m.content
        }))

      await anthropicStream(
        {
          model: model.name,
          messages: anthropicMessages,
          max_tokens: maxTokens.value,
          system: systemPrompt.value || undefined,
          temperature: temperature.value
        },
        selectedKeyId.value,
        onChunk,
        onError,
        onComplete,
        onReasoning,
        controller.signal
      )
    } else {
      // 使用 OpenAI 协议
      await chatStream(
        {
          model: model.name,
          messages: displayMessages.value,
          temperature: temperature.value,
          max_tokens: maxTokens.value
        },
        selectedKeyId.value,
        onChunk,
        onError,
        onComplete,
        onReasoning,
        controller.signal
      )
    }
  } catch (error) {
    isLoading.value = false
    abortController.value = null
    chatStore.clearBackgroundSession()
    console.error('发送消息失败:', error)
  }
}

// 终止响应
function abortResponse() {
  chatStore.abortAndSave()
  isLoading.value = false
  streamingContent.value = ''
  streamingReasoning.value = ''
  abortController.value = null
  refreshSessions()
}

// 滚动到底部（仅当用户在底部时）
function scrollToBottom() {
  if (!messagesContainer.value || !isUserAtBottom.value) return
  // 标记为自动滚动，避免 handleScroll 误判
  isAutoScrolling.value = true
  messagesContainer.value.scrollTop = messagesContainer.value.scrollHeight
  // 延迟重置，确保滚动事件已处理
  setTimeout(() => {
    isAutoScrolling.value = false
  }, 50)
}

// 处理滚动事件
function handleScroll() {
  // 如果是程序控制的滚动，忽略
  if (isAutoScrolling.value) return
  if (!messagesContainer.value) return
  const { scrollTop, scrollHeight, clientHeight } = messagesContainer.value
  // 距离底部 50px 以内视为在底部
  const isAtBottom = scrollHeight - scrollTop - clientHeight < 50
  isUserAtBottom.value = isAtBottom
}

// 渲染 Markdown
function renderMarkdown(content: string): string {
  return sanitizeHtml(marked(content) as string)
}

// 刷新会话列表（按最近更新时间排序）
function refreshSessions() {
  sessions.value = getChatSessions().sort((a, b) => b.updatedAt - a.updatedAt)
}

// 新建会话
function newSession() {
  // 如果当前会话有正在进行的请求，让它继续在后台处理
  // 只需切换到新会话
  messages.value = []
  currentSessionId.value = null
  streamingContent.value = ''
  streamingReasoning.value = ''
  isLoading.value = false
}

// 加载会话
function loadSession(sessionId: string) {
  // 如果后台会话属于要加载的会话，恢复状态
  if (chatStore.hasBackgroundSession && chatStore.backgroundSession?.sessionId === sessionId) {
    restoreBackgroundSession()
    return
  }

  // 否则从存储中加载
  const session = sessions.value.find(s => s.id === sessionId)
  if (session) {
    currentSessionId.value = session.id
    messages.value = [...session.messages]
    streamingContent.value = ''
    streamingReasoning.value = ''
    isLoading.value = false
    // 恢复模型选择（优先使用保存的 modelId，否则按名称查找）
    if (session.modelId) {
      // 验证模型是否仍然存在
      const model = models.value.find(m => m.id === session.modelId)
      if (model) {
        currentModelId.value = session.modelId
      }
    } else {
      // 兼容旧数据：根据模型名称查找模型 ID
      const model = models.value.find(m => m.name === session.model)
      if (model) {
        currentModelId.value = model.id
      }
    }
    // 恢复密钥选择（验证密钥是否仍然存在）
    if (session.keyId) {
      const key = userKeys.value.find(k => k.id === session.keyId)
      if (key) {
        selectedKeyId.value = session.keyId
      }
    }
  }
}

// 恢复后台会话
function restoreBackgroundSession() {
  if (!chatStore.hasBackgroundSession || !chatStore.backgroundSession) return

  const bg = chatStore.backgroundSession
  currentSessionId.value = bg.sessionId
  currentModelId.value = bg.modelId
  selectedKeyId.value = bg.keyId
  messages.value = [...bg.messages]
  streamingContent.value = bg.streamingContent
  streamingReasoning.value = bg.streamingReasoning
  systemPrompt.value = bg.systemPrompt
  temperature.value = bg.temperature
  maxTokens.value = bg.maxTokens
  isLoading.value = true

  // 恢复 AbortController
  const storedController = getChatAbortController()
  if (storedController) {
    abortController.value = storedController
  }

  // 滚动到底部
  nextTick(() => {
    scrollToBottom()
  })
}

// 删除会话
function handleDeleteSession(sessionId: string) {
  // 如果删除的是后台正在进行的会话，终止请求
  if (chatStore.hasBackgroundSession && chatStore.backgroundSession?.sessionId === sessionId) {
    chatStore.abortAndSave()
    isLoading.value = false
    streamingContent.value = ''
    streamingReasoning.value = ''
    abortController.value = null
  }

  deleteChatSession(sessionId)
  refreshSessions()
  if (currentSessionId.value === sessionId) {
    newSession()
  }
}

// 清空消息
function clearMessages() {
  // 如果当前会话有正在进行的请求，终止它
  if (chatStore.hasBackgroundSession && chatStore.backgroundSession?.sessionId === currentSessionId.value) {
    chatStore.abortAndSave()
    isLoading.value = false
    streamingContent.value = ''
    streamingReasoning.value = ''
    abortController.value = null
  }

  messages.value = []
  streamingContent.value = ''
  streamingReasoning.value = ''

  if (currentSessionId.value) {
    deleteChatSession(currentSessionId.value)
    refreshSessions()
    currentSessionId.value = null
  }
}

// 自动调整输入框高度
function adjustTextareaHeight() {
  if (textarea.value) {
    textarea.value.style.height = 'auto'
    textarea.value.style.height = Math.min(textarea.value.scrollHeight, 200) + 'px'
  }
}

// 处理键盘事件
function handleKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    sendMessage()
  }
}

// 切换思考内容显示
function toggleReasoning(index: number) {
  if (expandedReasoning.value.has(index)) {
    expandedReasoning.value.delete(index)
  } else {
    expandedReasoning.value.add(index)
  }
}

// 监听后台会话状态变化，同步到当前会话
watch(
  () => chatStore.backgroundSession,
  (bgSession) => {
    // 如果当前会话是后台会话，同步状态
    if (bgSession && currentSessionId.value === bgSession.sessionId) {
      messages.value = [...bgSession.messages]
      streamingContent.value = bgSession.streamingContent
      streamingReasoning.value = bgSession.streamingReasoning
    }
  },
  { deep: true }
)

// 监听后台会话完成
watch(
  () => chatStore.hasBackgroundSession,
  (hasSession) => {
    // 后台会话完成时，刷新会话列表
    if (!hasSession) {
      refreshSessions()
      isLoading.value = false
    }
  }
)

onMounted(async () => {
  await Promise.all([loadModels(), loadUserKeys()])
  refreshSessions()

  // 检查是否有后台会话需要恢复
  if (chatStore.hasBackgroundSession && chatStore.backgroundSession) {
    restoreBackgroundSession()
    return
  }

  // 默认选择最新的会话
  if (sessions.value.length > 0) {
    loadSession(sessions.value[0].id)
  }
})
</script>

<template>
  <div class="flex h-full">
    <!-- 左侧会话列表 -->
    <div class="w-64 bg-gray-50 dark:bg-gray-900 border-r dark:border-gray-700 flex flex-col">
      <div class="p-4 border-b dark:border-gray-700">
        <button
          @click="newSession"
          class="w-full px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 transition-colors"
        >
          新建会话
        </button>
      </div>
      <div class="flex-1 overflow-y-auto p-2">
        <div
          v-for="session in sessions"
          :key="session.id"
          :class="[
            'p-3 mb-1 rounded-lg cursor-pointer group flex items-center justify-between',
            currentSessionId === session.id
              ? 'bg-blue-50 dark:bg-blue-900/20'
              : 'hover:bg-gray-100 dark:hover:bg-gray-700'
          ]"
          @click="loadSession(session.id)"
        >
          <div class="flex-1 min-w-0">
            <div class="flex items-center gap-2">
              <!-- 显示会话名称（如无则显示模型名称） -->
              <div class="text-sm font-medium truncate">{{ session.name || session.model }}</div>
              <!-- 后台进行中标记 -->
              <span
                v-if="chatStore.hasBackgroundSession && chatStore.backgroundSession?.sessionId === session.id"
                class="w-2 h-2 bg-green-500 rounded-full animate-pulse"
                title="后台处理中"
              ></span>
            </div>
            <div class="text-xs text-gray-500 dark:text-gray-400">
              {{ session.messages.length }} 条消息 · {{ session.model }}
            </div>
          </div>
          <button
            @click.stop="handleDeleteSession(session.id)"
            class="opacity-0 group-hover:opacity-100 p-1 text-gray-400 hover:text-red-500"
          >
            <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>
        <div v-if="sessions.length === 0" class="text-center text-gray-400 dark:text-gray-500 py-8 text-sm">
          暂无会话记录
        </div>
      </div>
    </div>

    <!-- 右侧聊天区域 -->
    <div class="flex-1 flex flex-col">
      <!-- 顶部工具栏 -->
      <div class="p-4 bg-white dark:bg-gray-800 border-b dark:border-gray-700 flex items-center gap-4">
        <!-- 密钥选择 -->
        <div class="flex items-center gap-2">
          <label class="text-sm text-gray-600 dark:text-gray-300 whitespace-nowrap">密钥</label>
          <select
            v-model="selectedKeyId"
            class="px-3 py-2 border dark:border-gray-600 rounded-lg bg-white dark:bg-gray-700 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
          >
            <option value="">选择密钥</option>
            <option v-for="key in userKeys" :key="key.id" :value="key.id">
              {{ key.name }}
            </option>
          </select>
        </div>

        <!-- 模型选择 -->
        <div class="flex items-center gap-2">
          <label class="text-sm text-gray-600 dark:text-gray-300 whitespace-nowrap">模型</label>
          <select
            v-model="currentModelId"
            class="px-3 py-2 border dark:border-gray-600 rounded-lg bg-white dark:bg-gray-700 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
          >
            <option value="">选择模型</option>
            <option v-for="model in models" :key="model.id" :value="model.id">
              {{ model.name }} ({{ model.provider_type === 'anthropic' ? 'Anthropic' : model.provider_type === 'openai' ? 'OpenAI' : '兼容' }})
            </option>
          </select>
        </div>

        <!-- 模型类型标签 -->
        <span
          v-if="currentProviderType"
          :class="{
            'bg-purple-100 text-purple-800 dark:bg-purple-900 dark:text-purple-200': currentProviderType === 'anthropic',
            'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200': currentProviderType === 'openai',
            'bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-200': currentProviderType === 'openai_compatible'
          }"
          class="px-2 py-1 text-xs rounded-full"
        >
          {{ currentProviderType === 'anthropic' ? 'Anthropic' : currentProviderType === 'openai' ? 'OpenAI' : '兼容' }}
        </span>

        <button
          @click="showSettings = !showSettings"
          class="px-3 py-2 text-gray-600 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-lg"
        >
          <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" />
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
          </svg>
        </button>

        <button
          @click="clearMessages"
          :disabled="messages.length === 0"
          class="px-3 py-2 text-gray-600 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-lg disabled:opacity-50 disabled:cursor-not-allowed"
        >
          清空
        </button>
      </div>

      <!-- 密钥提示 -->
      <div v-if="userKeys.length === 0" class="px-4 py-2 bg-yellow-50 dark:bg-yellow-900/20 border-b dark:border-gray-700">
        <p class="text-sm text-yellow-700 dark:text-yellow-400">
          您还没有可用的密钥，请先在「密钥管理」中创建密钥后再使用聊天功能。
        </p>
      </div>

      <!-- 设置面板 -->
      <div v-if="showSettings" class="p-4 bg-gray-50 dark:bg-gray-900 border-b dark:border-gray-700">
        <div class="grid grid-cols-3 gap-4">
          <div>
            <label class="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Temperature</label>
            <input
              v-model.number="temperature"
              type="number"
              min="0"
              max="2"
              step="0.1"
              class="w-full px-3 py-2 border dark:border-gray-600 rounded-lg bg-white dark:bg-gray-700 dark:text-white"
            />
          </div>
          <div>
            <label class="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">最大 Token</label>
            <input
              v-model.number="maxTokens"
              type="number"
              min="1"
              class="w-full px-3 py-2 border dark:border-gray-600 rounded-lg bg-white dark:bg-gray-700 dark:text-white"
            />
          </div>
          <div>
            <label class="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">系统提示词</label>
            <input
              v-model="systemPrompt"
              type="text"
              class="w-full px-3 py-2 border dark:border-gray-600 rounded-lg bg-white dark:bg-gray-700 dark:text-white"
              placeholder="可选"
            />
          </div>
        </div>
      </div>

      <!-- 消息列表 -->
      <div
        ref="messagesContainer"
        @scroll="handleScroll"
        class="flex-1 overflow-y-auto p-4 space-y-4 bg-gray-50 dark:bg-gray-900"
      >
        <div v-if="messages.length === 0 && !streamingContent" class="text-center text-gray-400 dark:text-gray-500 py-16">
          <svg class="w-16 h-16 mx-auto mb-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z" />
          </svg>
          <p class="text-lg">开始新对话</p>
          <p class="text-sm mt-2">选择密钥和模型后输入消息开始聊天</p>
        </div>

        <div
          v-for="(message, index) in messages"
          :key="index"
          :class="[
            'flex',
            message.role === 'user' ? 'justify-end' : 'justify-start'
          ]"
        >
          <div
            :class="[
              'max-w-[80%] rounded-lg px-4 py-3',
              message.role === 'user'
                ? 'bg-blue-600 text-white'
                : 'bg-white dark:bg-gray-800 shadow'
            ]"
          >
            <!-- 模型信息标签（仅助手消息显示） -->
            <div
              v-if="message.role === 'assistant' && message.modelName"
              class="text-xs mb-1 flex items-center gap-1 text-gray-500 dark:text-gray-400"
            >
              <span>{{ message.modelName }}</span>
              <span
                v-if="message.providerType"
                :class="{
                  'text-purple-400': message.providerType === 'anthropic',
                  'text-blue-400': message.providerType === 'openai',
                  'text-gray-400': message.providerType === 'openai_compatible'
                }"
              >
                · {{ message.providerType === 'anthropic' ? 'Anthropic' : message.providerType === 'openai' ? 'OpenAI' : '兼容' }}
              </span>
            </div>
            <!-- 思考内容（可折叠） -->
            <div
              v-if="message.role === 'assistant' && message.reasoning_content"
              class="mb-3 border-l-2 border-purple-400 pl-3"
            >
              <button
                @click="toggleReasoning(index)"
                class="flex items-center gap-1 text-sm text-purple-600 dark:text-purple-400 hover:text-purple-800 dark:hover:text-purple-300 mb-1"
              >
                <svg
                  :class="['w-4 h-4 transition-transform', expandedReasoning.has(index) ? 'rotate-90' : '']"
                  fill="none"
                  stroke="currentColor"
                  viewBox="0 0 24 24"
                >
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" />
                </svg>
                <span>思考过程</span>
              </button>
              <div
                v-if="expandedReasoning.has(index)"
                class="text-sm text-gray-600 dark:text-gray-400 bg-gray-50 dark:bg-gray-900 rounded p-2 prose prose-sm dark:prose-invert max-w-none"
                v-html="renderMarkdown(message.reasoning_content)"
              ></div>
            </div>
            <!-- 正常内容 -->
            <div
              v-if="message.role === 'assistant'"
              class="prose prose-sm dark:prose-invert max-w-none"
              v-html="renderMarkdown(message.content)"
            ></div>
            <div v-else class="whitespace-pre-wrap">{{ message.content }}</div>
          </div>
        </div>

        <!-- 流式响应 -->
        <div v-if="streamingContent || streamingReasoning" class="flex justify-start">
          <div class="max-w-[80%] rounded-lg px-4 py-3 bg-white dark:bg-gray-800 shadow">
            <!-- 流式思考内容 -->
            <div
              v-if="streamingReasoning"
              class="mb-3 border-l-2 border-purple-400 pl-3"
            >
              <div class="flex items-center gap-1 text-sm text-purple-600 dark:text-purple-400 mb-1">
                <svg class="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24">
                  <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                  <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                </svg>
                <span>正在思考...</span>
              </div>
              <div
                class="text-sm text-gray-600 dark:text-gray-400 bg-gray-50 dark:bg-gray-900 rounded p-2 prose prose-sm dark:prose-invert max-w-none"
                v-html="renderMarkdown(streamingReasoning)"
              ></div>
            </div>
            <!-- 流式正常内容 -->
            <div
              v-if="streamingContent"
              class="prose prose-sm dark:prose-invert max-w-none"
              v-html="renderMarkdown(streamingContent)"
            ></div>
            <span v-if="streamingContent" class="inline-block w-2 h-4 bg-blue-500 animate-pulse ml-1"></span>
          </div>
        </div>

        <!-- 等待响应 loading 状态 -->
        <div v-else-if="isLoading" class="flex justify-start">
          <div class="max-w-[80%] rounded-lg px-4 py-3 bg-white dark:bg-gray-800 shadow">
            <div class="flex items-center gap-2 text-gray-500 dark:text-gray-400">
              <svg class="animate-spin w-4 h-4" fill="none" viewBox="0 0 24 24">
                <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
              </svg>
              <span>等待响应...</span>
            </div>
          </div>
        </div>
      </div>

      <!-- 输入区域 -->
      <div class="p-4 bg-white dark:bg-gray-800 border-t dark:border-gray-700">
        <div class="flex gap-4">
          <textarea
            ref="textarea"
            v-model="inputMessage"
            @input="adjustTextareaHeight"
            @keydown="handleKeydown"
            :disabled="!currentModelId || !selectedKeyId || isLoading"
            placeholder="输入消息... (Shift+Enter 换行)"
            rows="1"
            class="flex-1 px-4 py-3 border dark:border-gray-600 rounded-lg bg-white dark:bg-gray-700 dark:text-white resize-none focus:outline-none focus:ring-2 focus:ring-blue-500 disabled:opacity-50"
          ></textarea>
          <!-- 终止按钮 -->
          <button
            v-if="isLoading"
            @click="abortResponse"
            class="px-6 py-3 bg-red-600 text-white rounded-lg hover:bg-red-700 transition-colors"
          >
            <span class="flex items-center">
              <svg class="w-5 h-5 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
              </svg>
              终止
            </span>
          </button>
          <!-- 发送按钮 -->
          <button
            v-else
            @click="sendMessage"
            :disabled="!inputMessage.trim() || !currentModelId || !selectedKeyId"
            class="px-6 py-3 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
          >
            发送
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<style>
.prose pre {
  @apply bg-gray-900 dark:bg-gray-950 rounded-lg p-4 overflow-x-auto;
}

.prose code {
  @apply bg-gray-100 dark:bg-gray-800 px-1.5 py-0.5 rounded text-sm;
}

.prose pre code {
  @apply bg-transparent p-0;
}
</style>
