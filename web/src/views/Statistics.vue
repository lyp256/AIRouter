<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { statsApi } from '@/api/stats'
import type { UsageLog } from '@/api/types'

const logs = ref<UsageLog[]>([])
const loading = ref(false)
const total = ref(0)

const filters = ref({
  user_id: '',
  model: '',
  status: ''
})

async function loadLogs() {
  loading.value = true
  try {
    const params: Record<string, string> = {}
    if (filters.value.user_id) params.user_id = filters.value.user_id
    if (filters.value.model) params.model = filters.value.model
    if (filters.value.status) params.status = filters.value.status

    const res = await statsApi.logs(params)
    logs.value = res.data
    total.value = res.total
  } finally {
    loading.value = false
  }
}

function formatTime(date: string): string {
  return new Date(date).toLocaleString()
}

function formatCost(n: number): string {
  return '$' + n.toFixed(6)
}

onMounted(loadLogs)
</script>

<template>
  <div class="p-6">
    <h1 class="text-2xl font-bold text-gray-800 dark:text-white mb-6">使用日志</h1>

    <!-- 筛选 -->
    <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-4 mb-6">
      <div class="flex gap-4">
        <input
          v-model="filters.user_id"
          placeholder="用户 ID"
          class="px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white"
        />
        <input
          v-model="filters.model"
          placeholder="模型"
          class="px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white"
        />
        <select v-model="filters.status" class="px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white">
          <option value="">全部状态</option>
          <option value="success">成功</option>
          <option value="error">失败</option>
        </select>
        <button @click="loadLogs" class="px-4 py-2 bg-blue-600 text-white rounded-lg">筛选</button>
      </div>
    </div>

    <!-- 日志列表 -->
    <div class="bg-white dark:bg-gray-800 rounded-lg shadow overflow-auto">
      <table class="w-full">
        <thead class="bg-gray-50 dark:bg-gray-700">
          <tr>
            <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300">时间</th>
            <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300">模型</th>
            <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300">输入 Token</th>
            <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300">输出 Token</th>
            <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300">成本</th>
            <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300">延迟</th>
            <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300">状态</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-200 dark:divide-gray-700">
          <tr v-for="log in logs" :key="log.id">
            <td class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400 whitespace-nowrap">{{ formatTime(log.created_at) }}</td>
            <td class="px-4 py-3 text-sm text-gray-900 dark:text-white">{{ log.model }}</td>
            <td class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400">{{ log.input_tokens }}</td>
            <td class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400">{{ log.output_tokens }}</td>
            <td class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400">{{ formatCost(log.cost) }}</td>
            <td class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400">{{ log.latency }}ms</td>
            <td class="px-4 py-3">
              <span
                :class="log.status === 'success' ? 'bg-green-100 text-green-800' : 'bg-red-100 text-red-800'"
                class="px-2 py-1 text-xs rounded-full"
              >
                {{ log.status === 'success' ? '成功' : '失败' }}
              </span>
            </td>
          </tr>
        </tbody>
      </table>
      <p v-if="logs.length === 0" class="text-center text-gray-400 py-8">暂无数据</p>
    </div>
  </div>
</template>