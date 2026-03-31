<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { statsApi } from '@/api/stats'
import type { UsageLog, FilterOptions } from '@/api/types'
import { formatBU } from '@/utils/format'

const logs = ref<UsageLog[]>([])
const loading = ref(false)
const total = ref(0)
const currentPage = ref(1)
const pageSize = 20

const filterOptions = ref<FilterOptions>({
  models: [],
  provider_types: [],
  provider_names: [],
  provider_keys: [],
  statuses: []
})

const filters = ref({
  model: '',
  provider_type: '',
  provider_name: '',
  provider_key_id: '',
  status: ''
})

// tooltip 动态定位
const tooltipStyle = ref({})
function showTooltip(event: MouseEvent) {
  const el = event.currentTarget as HTMLElement
  const rect = el.getBoundingClientRect()
  const tipW = 300
  const tipMaxH = window.innerHeight - 16
  let left = rect.left + rect.width / 2 - tipW / 2
  if (left < 8) left = 8
  if (left + tipW > window.innerWidth - 8) left = window.innerWidth - 8 - tipW
  let top = rect.top - 4
  const above = rect.top > 60
  if (!above) {
    top = rect.bottom + 4
  }
  tooltipStyle.value = {
    left: left + 'px',
    top: (above ? 'auto' : top + 'px'),
    bottom: (above ? (window.innerHeight - rect.top + 4) + 'px' : 'auto'),
    maxHeight: (above ? Math.min(rect.top - 16, 200) : Math.min(tipMaxH - top, 200)) + 'px',
    overflowY: 'auto' as const,
    position: 'fixed' as const,
  }
}

async function loadFilterOptions() {
  try {
    const res = await statsApi.filterOptions()
    filterOptions.value = res.data || { models: [], provider_types: [], provider_names: [], provider_keys: [], statuses: [] }
  } catch (e) {
    console.error('加载筛选选项失败', e)
  }
}

async function loadLogs() {
  loading.value = true
  try {
    const params: Record<string, string | number> = { page: currentPage.value, page_size: pageSize }
    if (filters.value.model) params.model = filters.value.model
    if (filters.value.provider_type) params.provider_type = filters.value.provider_type
    if (filters.value.provider_name) params.provider_name = filters.value.provider_name
    if (filters.value.provider_key_id) params.provider_key_id = filters.value.provider_key_id
    if (filters.value.status) params.status = filters.value.status

    const res = await statsApi.logs(params)
    logs.value = res.data || []
    total.value = res.total || 0
  } finally {
    loading.value = false
  }
}

function formatTime(date: string): string {
  return new Date(date).toLocaleString()
}

// 格式化延迟显示（人类可读）
function formatLatency(ms: number | undefined | null): string {
  if (!ms) return '-'
  if (ms < 1) return '<1ms'
  if (ms < 1000) return Math.round(ms) + 'ms'
  const sec = ms / 1000
  if (sec < 60) return sec.toFixed(1) + 's'
  const min = Math.floor(sec / 60)
  const remainSec = Math.round(sec % 60)
  return `${min}m${remainSec}s`
}

// 计算整体 token 速率（token/s）
function calcTotalRate(log: UsageLog): number | null {
  const duration = log.total_duration || log.latency
  if (duration <= 0 || log.output_tokens <= 0) return null
  return (log.output_tokens / duration) * 1000
}

// 计算响应 token 速率（token/s）
function calcResponseRate(log: UsageLog): number | null {
  const duration = log.total_duration || log.latency
  const responseTime = duration - (log.first_token_latency || 0)
  if (responseTime <= 0 || log.output_tokens <= 0) return null
  return (log.output_tokens / responseTime) * 1000
}

// 格式化速率显示
function formatRate(rate: number | null): string {
  if (rate === null) return '-'
  if (rate >= 100) return rate.toFixed(0) + ' t/s'
  if (rate >= 10) return rate.toFixed(1) + ' t/s'
  return rate.toFixed(2) + ' t/s'
}

function goToPage(page: number) {
  currentPage.value = page
  loadLogs()
}

onMounted(() => {
  loadFilterOptions()
  loadLogs()
})
</script>

<template>
  <div class="p-6">
    <h1 class="text-2xl font-bold text-gray-800 dark:text-white mb-6">使用日志</h1>

    <!-- 筛选 -->
    <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-4 mb-6">
      <div class="flex gap-4 flex-wrap">
        <select v-model="filters.model" class="px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white min-w-[150px]">
          <option value="">全部模型</option>
          <option v-for="m in filterOptions.models" :key="m" :value="m">{{ m }}</option>
        </select>
        <select v-model="filters.provider_type" class="px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white min-w-[150px]">
          <option value="">全部协议</option>
          <option v-for="t in filterOptions.provider_types" :key="t" :value="t">{{ t }}</option>
        </select>
        <select v-model="filters.provider_name" class="px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white min-w-[150px]">
          <option value="">全部厂商</option>
          <option v-for="p in filterOptions.provider_names" :key="p" :value="p">{{ p }}</option>
        </select>
        <select v-model="filters.provider_key_id" class="px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white min-w-[150px]">
          <option value="">全部供应商Key</option>
          <option v-for="pk in filterOptions.provider_keys" :key="pk.id" :value="pk.id">{{ pk.name }}</option>
        </select>
        <select v-model="filters.status" class="px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white min-w-[120px]">
          <option value="">全部状态</option>
          <option value="success">成功</option>
          <option value="error">失败</option>
        </select>
        <button @click="currentPage = 1; loadLogs()" class="px-4 py-2 bg-blue-600 text-white rounded-lg">筛选</button>
        <button @click="filters = { model: '', provider_type: '', provider_name: '', provider_key_id: '', status: '' }; currentPage = 1; loadLogs()" class="px-4 py-2 bg-gray-400 text-white rounded-lg hover:bg-gray-500">清空</button>
      </div>
    </div>

    <!-- 加载动画 -->
    <div v-if="loading" class="flex items-center justify-center py-16 bg-white dark:bg-gray-800 rounded-lg shadow">
      <svg class="animate-spin w-6 h-6 text-blue-500 mr-3" fill="none" viewBox="0 0 24 24">
        <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
        <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
      </svg>
      <span class="text-gray-500 dark:text-gray-400">加载中...</span>
    </div>

    <!-- 日志列表 -->
    <div v-else class="bg-white dark:bg-gray-800 rounded-lg shadow overflow-auto">
      <table class="w-full">
        <thead class="bg-gray-50 dark:bg-gray-700">
          <tr>
            <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300">时间</th>
            <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300">模型</th>
            <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300">协议</th>
            <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300">厂商</th>
            <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300">上游模型</th>
            <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300">输入 Token</th>
            <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300">输出 Token</th>
            <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300">成本</th>
            <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300">延迟</th>
            <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300">首Token延迟</th>
            <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300">总耗时</th>
            <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300">整体速率</th>
            <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300">响应速率</th>
            <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300">状态</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-200 dark:divide-gray-700">
          <tr v-for="log in logs" :key="log.id">
            <td class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400 whitespace-nowrap">{{ formatTime(log.created_at) }}</td>
            <td class="px-4 py-3 text-sm text-gray-900 dark:text-white">{{ log.model }}</td>
            <td class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400">{{ log.provider_type || '-' }}</td>
            <td class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400">{{ log.provider_name || '-' }}</td>
            <td class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400">{{ log.provider_model || '-' }}</td>
            <td class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400">{{ log.input_tokens }}</td>
            <td class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400">{{ log.output_tokens }}</td>
            <td class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400">{{ formatBU(log.cost) }}</td>
            <td class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400">{{ formatLatency(log.latency) }}</td>
            <td class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400">{{ formatLatency(log.first_token_latency) }}</td>
            <td class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400">{{ formatLatency(log.total_duration) }}</td>
            <td class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400">{{ formatRate(calcTotalRate(log)) }}</td>
            <td class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400">{{ formatRate(calcResponseRate(log)) }}</td>
            <td class="px-4 py-3">
              <span class="inline-flex items-center gap-1">
                <span
                  :class="log.status === 'success' ? 'bg-green-100 text-green-800' : 'bg-red-100 text-red-800'"
                  class="px-2 py-1 text-xs rounded-full"
                >
                  {{ log.status === 'success' ? '成功' : '失败' }}
                </span>
                <span v-if="log.status === 'error' && log.error_message" class="relative cursor-help text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 group" @mouseenter="showTooltip">
                  &#9432;
                  <span class="fixed px-2 py-1 text-xs text-white bg-gray-800 rounded opacity-0 group-hover:opacity-100 transition-opacity pointer-events-none z-50 max-w-[300px] whitespace-normal break-all" :style="tooltipStyle">{{ log.error_message }}</span>
                </span>
              </span>
            </td>
          </tr>
        </tbody>
      </table>
      <p v-if="logs.length === 0" class="text-center text-gray-400 py-8">暂无数据</p>
      <!-- 分页 -->
      <div v-if="total > 0" class="flex items-center justify-between px-6 py-3 border-t dark:border-gray-700">
        <span class="text-sm text-gray-500 dark:text-gray-400">共 {{ total }} 条，第 {{ currentPage }} / {{ Math.ceil(total / pageSize) }} 页</span>
        <div class="flex gap-2">
          <button @click="goToPage(currentPage - 1)" :disabled="currentPage <= 1" class="px-3 py-1 text-sm border rounded-lg dark:border-gray-600 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700 disabled:opacity-50 disabled:cursor-not-allowed">上一页</button>
          <button @click="goToPage(currentPage + 1)" :disabled="currentPage >= Math.ceil(total / pageSize)" class="px-3 py-1 text-sm border rounded-lg dark:border-gray-600 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700 disabled:opacity-50 disabled:cursor-not-allowed">下一页</button>
        </div>
      </div>
    </div>
  </div>
</template>