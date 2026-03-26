<script setup lang="ts">
import { ref, onMounted, nextTick, computed } from 'vue'
import { marked } from 'marked'
import { modelApi } from '@/api/model'
import { userApi } from '@/api/user'
import { chatStream, getChatSessions, saveChatSession, deleteChatSession, createChatSession } from '@/api/chat'
import type { Model, ChatMessage, UserKey } from '@/api/types'

// 配置 marked
marked.setOptions({
  breaks: true,
  gfm: true
})

// 状态
const models = ref<Model[]>([])
const currentModel = ref('')
const userKeys = ref<UserKey[]>([])
const selectedKeyId = ref('')
const messages = ref<ChatMessage[]>([])
const inputMessage = ref('')
const isLoading = ref(false)
const streamingContent = ref('')
const streamingReasoning = ref('') // 流式思考内容
const messagesContainer = ref<HTMLElement | null>(null)
const textarea = ref<HTMLTextAreaElement | null>(null)

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
    const response = await modelApi.list()
    models.value = response.data.filter((m: Model) => m.enabled)
    if (models.value.length > 0 && !currentModel.value) {
      currentModel.value = models.value[0].name
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
  if (!content || isLoading.value || !currentModel.value || !selectedKeyId.value) return

  // 添加用户消息
  messages.value.push({ role: 'user', content })
  inputMessage.value = ''
  isLoading.value = true
  streamingContent.value = ''
  streamingReasoning.value = ''

  // 自动调整输入框高度
  if (textarea.value) {
    textarea.value.style.height = 'auto'
  }

  try {
    await chatStream(
      {
        model: currentModel.value,
        messages: displayMessages.value,
        temperature: temperature.value,
        max_tokens: maxTokens.value
      },
      selectedKeyId.value,  // 传入 keyId
      (text: string) => {
        streamingContent.value += text
        scrollToBottom()
      },
      (error: Error) => {
        messages.value.push({ role: 'assistant', content: `错误: ${error.message}` })
      },
      () => {
        if (streamingContent.value || streamingReasoning.value) {
          messages.value.push({
            role: 'assistant',
            content: streamingContent.value,
            reasoning_content: streamingReasoning.value || undefined
          })
        }
        streamingContent.value = ''
        streamingReasoning.value = ''
        isLoading.value = false
        saveCurrentSession()
      },
      (text: string) => {
        // 思考内容回调
        streamingReasoning.value += text
        scrollToBottom()
      }
    )
  } catch (error) {
    isLoading.value = false
    console.error('发送消息失败:', error)
  }
}

// 滚动到底部
function scrollToBottom() {
  nextTick(() => {
    if (messagesContainer.value) {
      messagesContainer.value.scrollTop = messagesContainer.value.scrollHeight
    }
  })
}

// 渲染 Markdown
function renderMarkdown(content: string): string {
  return marked(content) as string
}

// 新建会话
function newSession() {
  messages.value = []
  currentSessionId.value = null
  streamingContent.value = ''
  streamingReasoning.value = ''
}

// 加载会话
function loadSession(sessionId: string) {
  const session = sessions.value.find(s => s.id === sessionId)
  if (session) {
    currentSessionId.value = session.id
    messages.value = [...session.messages]
    currentModel.value = session.model
  }
}

// 保存当前会话
function saveCurrentSession() {
  if (messages.value.length === 0) return

  if (!currentSessionId.value) {
    const session = createChatSession(currentModel.value)
    currentSessionId.value = session.id
  }

  saveChatSession({
    id: currentSessionId.value,
    model: currentModel.value,
    messages: [...messages.value],
    createdAt: Date.now(),
    updatedAt: Date.now()
  })
  sessions.value = getChatSessions()
}

// 删除会话
function handleDeleteSession(sessionId: string) {
  deleteChatSession(sessionId)
  sessions.value = getChatSessions()
  if (currentSessionId.value === sessionId) {
    newSession()
  }
}

// 清空消息
function clearMessages() {
  messages.value = []
  streamingContent.value = ''
  streamingReasoning.value = ''
  if (currentSessionId.value) {
    deleteChatSession(currentSessionId.value)
    sessions.value = getChatSessions()
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

onMounted(() => {
  loadModels()
  loadUserKeys()
})
</script>

<template>
  <div class="flex h-full">
    <!-- 左侧会话列表 -->
    <div class="w-64 bg-white dark:bg-gray-800 border-r dark:border-gray-700 flex flex-col">
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
            <div class="text-sm font-medium truncate">{{ session.model }}</div>
            <div class="text-xs text-gray-500 dark:text-gray-400">
              {{ session.messages.length }} 条消息
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
        <select
          v-model="selectedKeyId"
          class="px-3 py-2 border dark:border-gray-600 rounded-lg bg-white dark:bg-gray-700 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
        >
          <option value="">选择密钥</option>
          <option v-for="key in userKeys" :key="key.id" :value="key.id">
            {{ key.name }}
          </option>
        </select>

        <!-- 模型选择 -->
        <select
          v-model="currentModel"
          class="px-3 py-2 border dark:border-gray-600 rounded-lg bg-white dark:bg-gray-700 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
        >
          <option value="">选择模型</option>
          <option v-for="model in models" :key="model.id" :value="model.name">
            {{ model.name }}
          </option>
        </select>

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
      </div>

      <!-- 输入区域 -->
      <div class="p-4 bg-white dark:bg-gray-800 border-t dark:border-gray-700">
        <div class="flex gap-4">
          <textarea
            ref="textarea"
            v-model="inputMessage"
            @input="adjustTextareaHeight"
            @keydown="handleKeydown"
            :disabled="!currentModel || !selectedKeyId || isLoading"
            placeholder="输入消息... (Shift+Enter 换行)"
            rows="1"
            class="flex-1 px-4 py-3 border dark:border-gray-600 rounded-lg bg-white dark:bg-gray-700 dark:text-white resize-none focus:outline-none focus:ring-2 focus:ring-blue-500 disabled:opacity-50"
          ></textarea>
          <button
            @click="sendMessage"
            :disabled="!inputMessage.trim() || !currentModel || !selectedKeyId || isLoading"
            class="px-6 py-3 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
          >
            <span v-if="isLoading" class="flex items-center">
              <svg class="animate-spin w-5 h-5 mr-2" fill="none" viewBox="0 0 24 24">
                <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
              </svg>
              发送中
            </span>
            <span v-else>发送</span>
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