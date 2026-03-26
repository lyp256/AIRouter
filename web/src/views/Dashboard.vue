<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { statsApi, type DashboardStats, type UsageTrend, type ModelStats } from '@/api/stats'

const stats = ref<DashboardStats | null>(null)
const trends = ref<UsageTrend[]>([])
const modelStats = ref<ModelStats[]>([])
const loading = ref(false)

async function loadData() {
  loading.value = true
  try {
    const [dashRes, trendRes, modelRes] = await Promise.all([
      statsApi.dashboard(),
      statsApi.trend(7),
      statsApi.models(7)
    ])
    stats.value = dashRes.data
    trends.value = trendRes.data
    modelStats.value = modelRes.data
  } finally {
    loading.value = false
  }
}

function formatNumber(n: number): string {
  if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M'
  if (n >= 1000) return (n / 1000).toFixed(1) + 'K'
  return n.toString()
}

function formatCost(n: number): string {
  return '$' + n.toFixed(4)
}

// 简单的图表数据（CSS 实现）
function getMaxValue(arr: number[]): number {
  return Math.max(...arr, 1)
}

onMounted(loadData)
</script>

<template>
  <div class="p-6">
    <h1 class="text-2xl font-bold text-gray-800 dark:text-white mb-6">仪表盘</h1>

    <!-- 统计卡片 -->
    <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6 mb-8" v-if="stats">
      <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-6">
        <h3 class="text-sm font-medium text-gray-500 dark:text-gray-400">今日请求</h3>
        <p class="text-3xl font-bold text-gray-800 dark:text-white mt-2">{{ formatNumber(stats.today_requests) }}</p>
      </div>
      <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-6">
        <h3 class="text-sm font-medium text-gray-500 dark:text-gray-400">今日 Token</h3>
        <p class="text-3xl font-bold text-gray-800 dark:text-white mt-2">{{ formatNumber(stats.today_tokens) }}</p>
      </div>
      <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-6">
        <h3 class="text-sm font-medium text-gray-500 dark:text-gray-400">今日消费</h3>
        <p class="text-3xl font-bold text-gray-800 dark:text-white mt-2">{{ formatCost(stats.today_cost) }}</p>
      </div>
      <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-6">
        <h3 class="text-sm font-medium text-gray-500 dark:text-gray-400">成功率</h3>
        <p class="text-3xl font-bold text-gray-800 dark:text-white mt-2">{{ stats.success_rate.toFixed(1) }}%</p>
      </div>
    </div>

    <div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
      <!-- 使用趋势 -->
      <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-6">
        <h2 class="text-lg font-bold text-gray-800 dark:text-white mb-4">请求趋势（7天）</h2>
        <div class="h-48 flex items-end gap-2">
          <div v-for="t in trends" :key="t.date" class="flex-1 flex flex-col items-center">
            <div
              class="w-full bg-blue-500 rounded-t"
              :style="{ height: (t.requests / getMaxValue(trends.map(x => x.requests))) * 150 + 'px' }"
            ></div>
            <span class="text-xs text-gray-500 mt-1">{{ new Date(t.date).toLocaleDateString('zh', { weekday: 'short' }) }}</span>
          </div>
        </div>
      </div>

      <!-- 模型使用统计 -->
      <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-6">
        <h2 class="text-lg font-bold text-gray-800 dark:text-white mb-4">模型使用统计（7天）</h2>
        <div class="space-y-3">
          <div v-for="m in modelStats" :key="m.model" class="flex items-center">
            <span class="w-32 text-sm text-gray-600 dark:text-gray-300 truncate">{{ m.model }}</span>
            <div class="flex-1 h-4 bg-gray-200 dark:bg-gray-700 rounded overflow-hidden">
              <div
                class="h-full bg-green-500"
                :style="{ width: (m.requests / getMaxValue(modelStats.map(x => x.requests))) * 100 + '%' }"
              ></div>
            </div>
            <span class="w-20 text-right text-sm text-gray-500 dark:text-gray-400">{{ m.requests }}</span>
          </div>
          <p v-if="modelStats.length === 0" class="text-center text-gray-400 py-4">暂无数据</p>
        </div>
      </div>
    </div>

    <!-- 快速信息 -->
    <div class="grid grid-cols-1 md:grid-cols-2 gap-6 mt-6" v-if="stats">
      <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-6">
        <h2 class="text-lg font-bold text-gray-800 dark:text-white mb-4">系统状态</h2>
        <div class="space-y-2">
          <div class="flex justify-between">
            <span class="text-gray-500 dark:text-gray-400">活跃用户</span>
            <span class="font-medium dark:text-white">{{ stats.active_users }}</span>
          </div>
          <div class="flex justify-between">
            <span class="text-gray-500 dark:text-gray-400">活跃密钥</span>
            <span class="font-medium dark:text-white">{{ stats.active_keys }}</span>
          </div>
        </div>
      </div>

      <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-6">
        <h2 class="text-lg font-bold text-gray-800 dark:text-white mb-4">消费趋势（7天）</h2>
        <div class="space-y-2">
          <div v-for="t in trends.slice().reverse().slice(0, 5)" :key="t.date" class="flex justify-between">
            <span class="text-gray-500 dark:text-gray-400">{{ new Date(t.date).toLocaleDateString() }}</span>
            <span class="font-medium dark:text-white">{{ formatCost(t.cost) }}</span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>