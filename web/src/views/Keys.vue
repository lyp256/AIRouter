<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { userApi } from '@/api/user'
import { useUserStore } from '@/stores/user'
import type { User, UserKey } from '@/api/types'

const userStore = useUserStore()
const users = ref<User[]>([])
const selectedUserId = ref('')
const userKeys = ref<UserKey[]>([])
const loading = ref(false)

// 创建密钥相关
const showCreateModal = ref(false)
const creating = ref(false)
const newKeyName = ref('')
const newKeyRateLimit = ref(60)
const newKeyQuotaLimit = ref<number | null>(null)
const newKeyExpiredAt = ref('')
const createdRawKey = ref('')
const showRawKeyModal = ref(false)

// 编辑密钥相关
const showEditModal = ref(false)
const editing = ref(false)
const editingKey = ref<UserKey | null>(null)
const editRateLimit = ref(60)
const editQuotaLimit = ref<number | null>(null)
const editExpiredAt = ref('')

async function loadUsers() {
  try {
    const res = await userApi.list()
    users.value = res.data
    // 默认选择当前用户
    if (userStore.user && !selectedUserId.value) {
      selectedUserId.value = userStore.user.id
      await loadKeys()
    }
  } catch (e) {
    // ignore
  }
}

async function loadKeys() {
  if (!selectedUserId.value) {
    userKeys.value = []
    return
  }
  loading.value = true
  try {
    const res = await userApi.listKeys(selectedUserId.value)
    userKeys.value = res.data
  } finally {
    loading.value = false
  }
}

function openCreateModal() {
  newKeyName.value = ''
  newKeyRateLimit.value = 60
  newKeyQuotaLimit.value = null
  newKeyExpiredAt.value = ''
  showCreateModal.value = true
}

async function createKey() {
  if (!newKeyName.value.trim()) {
    alert('请输入密钥名称')
    return
  }

  creating.value = true
  try {
    const data: Partial<UserKey> & { user_id: string; name: string } = {
      user_id: selectedUserId.value,
      name: newKeyName.value,
      rate_limit: newKeyRateLimit.value
    }
    if (newKeyQuotaLimit.value) {
      data.quota_limit = newKeyQuotaLimit.value
    }
    if (newKeyExpiredAt.value) {
      data.expired_at = newKeyExpiredAt.value
    }

    const res = await userApi.createKey(data)
    createdRawKey.value = res.raw_key
    showCreateModal.value = false
    showRawKeyModal.value = true
    await loadKeys()
  } catch (e: unknown) {
    const err = e as { response?: { data?: { error?: string } } }
    alert(err.response?.data?.error || '创建失败')
  } finally {
    creating.value = false
  }
}

function openEditModal(key: UserKey) {
  editingKey.value = key
  editRateLimit.value = key.rate_limit
  editQuotaLimit.value = key.quota_limit || null
  editExpiredAt.value = key.expired_at ? key.expired_at.split('T')[0] : ''
  showEditModal.value = true
}

async function updateKey() {
  if (!editingKey.value) return

  editing.value = true
  try {
    const data: Partial<UserKey> = {
      rate_limit: editRateLimit.value
    }
    if (editQuotaLimit.value !== null) {
      data.quota_limit = editQuotaLimit.value
    }
    if (editExpiredAt.value) {
      data.expired_at = editExpiredAt.value
    }

    await userApi.updateKey(editingKey.value.id, data)
    showEditModal.value = false
    await loadKeys()
  } catch (e: unknown) {
    const err = e as { response?: { data?: { error?: string } } }
    alert(err.response?.data?.error || '更新失败')
  } finally {
    editing.value = false
  }
}

async function toggleKeyStatus(key: UserKey) {
  const newStatus = key.status === 'active' ? 'disabled' : 'active'
  const action = newStatus === 'disabled' ? '禁用' : '启用'

  if (!confirm(`确定要${action}密钥「${key.name}」吗？`)) {
    return
  }

  try {
    await userApi.updateKey(key.id, { status: newStatus })
    await loadKeys()
  } catch (e: unknown) {
    const err = e as { response?: { data?: { error?: string } } }
    alert(err.response?.data?.error || '操作失败')
  }
}

async function regenerateKey(key: UserKey) {
  if (!confirm(`确定要刷新密钥「${key.name}」吗？刷新后原密钥将失效。`)) {
    return
  }

  try {
    const res = await userApi.regenerateKey(key.id)
    createdRawKey.value = res.raw_key
    showRawKeyModal.value = true
    await loadKeys()
  } catch (e: unknown) {
    const err = e as { response?: { data?: { error?: string } } }
    alert(err.response?.data?.error || '刷新失败')
  }
}

async function deleteKey(key: UserKey) {
  if (!confirm(`确定要删除密钥「${key.name}」吗？此操作不可恢复。`)) {
    return
  }

  try {
    await userApi.deleteKey(key.id)
    await loadKeys()
  } catch (e: unknown) {
    const err = e as { response?: { data?: { error?: string } } }
    alert(err.response?.data?.error || '删除失败')
  }
}

function copyRawKey() {
  navigator.clipboard.writeText(createdRawKey.value)
  alert('已复制到剪贴板')
}

function closeRawKeyModal() {
  showRawKeyModal.value = false
  createdRawKey.value = ''
}

onMounted(loadUsers)
</script>

<template>
  <div class="p-6">
    <h1 class="text-2xl font-bold text-gray-800 dark:text-white mb-6">密钥管理</h1>

    <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-6">
      <div class="mb-4 flex flex-col md:flex-row md:items-end gap-4">
        <div class="flex-1">
          <label class="block text-sm font-medium mb-2 dark:text-gray-200">选择用户</label>
          <select
            v-model="selectedUserId"
            @change="loadKeys"
            class="w-full md:w-64 px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white"
          >
            <option value="">请选择用户</option>
            <option v-for="u in users" :key="u.id" :value="u.id">{{ u.username }}</option>
          </select>
        </div>
        <button
          v-if="selectedUserId"
          @click="openCreateModal"
          class="px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 transition"
        >
          创建密钥
        </button>
      </div>

      <div v-if="selectedUserId">
        <!-- 加载动画 -->
        <div v-if="loading" class="flex items-center justify-center py-16">
          <svg class="animate-spin w-6 h-6 text-blue-500 mr-3" fill="none" viewBox="0 0 24 24">
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
          </svg>
          <span class="text-gray-500 dark:text-gray-400">加载中...</span>
        </div>
        <template v-else>
        <table class="w-full" v-if="userKeys.length > 0">
          <thead class="bg-gray-50 dark:bg-gray-700">
            <tr>
              <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300">名称</th>
              <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300">限流</th>
              <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300">配额使用</th>
              <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300">状态</th>
              <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300">过期时间</th>
              <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300">操作</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-200 dark:divide-gray-700">
            <tr v-for="k in userKeys" :key="k.id">
              <td class="px-4 py-3 text-sm dark:text-white">{{ k.name }}</td>
              <td class="px-4 py-3 text-sm dark:text-gray-300">{{ k.rate_limit }}/min</td>
              <td class="px-4 py-3 text-sm dark:text-gray-300">{{ k.quota_used }} / {{ k.quota_limit || '∞' }}</td>
              <td class="px-4 py-3">
                <span :class="k.status === 'active' ? 'text-green-600' : 'text-red-600'" class="text-sm">
                  {{ k.status === 'active' ? '正常' : '禁用' }}
                </span>
              </td>
              <td class="px-4 py-3 text-sm dark:text-gray-300">
                {{ k.expired_at ? new Date(k.expired_at).toLocaleDateString() : '永久' }}
              </td>
              <td class="px-4 py-3">
                <div class="flex items-center gap-2">
                  <button
                    @click="openEditModal(k)"
                    class="text-blue-600 hover:text-blue-800 text-sm"
                    title="编辑"
                  >
                    编辑
                  </button>
                  <button
                    @click="toggleKeyStatus(k)"
                    :class="k.status === 'active' ? 'text-yellow-600 hover:text-yellow-800' : 'text-green-600 hover:text-green-800'"
                    class="text-sm"
                    :title="k.status === 'active' ? '禁用' : '启用'"
                  >
                    {{ k.status === 'active' ? '禁用' : '启用' }}
                  </button>
                  <button
                    @click="regenerateKey(k)"
                    class="text-purple-600 hover:text-purple-800 text-sm"
                    title="刷新密钥"
                  >
                    刷新
                  </button>
                  <button
                    @click="deleteKey(k)"
                    class="text-red-600 hover:text-red-800 text-sm"
                    title="删除"
                  >
                    删除
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
        <p v-else class="text-center text-gray-400 py-8">该用户暂无密钥</p>
        </template>
      </div>
    </div>

    <!-- 创建密钥模态框 -->
    <div v-if="showCreateModal" class="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
      <div class="bg-white dark:bg-gray-800 rounded-lg shadow-lg p-6 w-full max-w-md">
        <h2 class="text-xl font-bold mb-4 dark:text-white">创建密钥</h2>
        <div class="space-y-4">
          <div>
            <label class="block text-sm font-medium mb-1 dark:text-gray-200">密钥名称 *</label>
            <input
              v-model="newKeyName"
              type="text"
              class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white"
              placeholder="例如: 生产环境密钥"
            />
          </div>
          <div>
            <label class="block text-sm font-medium mb-1 dark:text-gray-200">限流 (次/分钟)</label>
            <input
              v-model.number="newKeyRateLimit"
              type="number"
              min="1"
              class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white"
            />
          </div>
          <div>
            <label class="block text-sm font-medium mb-1 dark:text-gray-200">配额限制 (可选)</label>
            <input
              v-model.number="newKeyQuotaLimit"
              type="number"
              min="0"
              class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white"
              placeholder="留空表示无限制"
            />
          </div>
          <div>
            <label class="block text-sm font-medium mb-1 dark:text-gray-200">过期时间 (可选)</label>
            <input
              v-model="newKeyExpiredAt"
              type="date"
              class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white"
            />
          </div>
        </div>
        <div class="mt-6 flex justify-end gap-3">
          <button
            @click="showCreateModal = false"
            class="px-4 py-2 text-gray-600 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-lg"
          >
            取消
          </button>
          <button
            @click="createKey"
            :disabled="creating"
            class="px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50"
          >
            {{ creating ? '创建中...' : '创建' }}
          </button>
        </div>
      </div>
    </div>

    <!-- 编辑密钥模态框 -->
    <div v-if="showEditModal" class="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
      <div class="bg-white dark:bg-gray-800 rounded-lg shadow-lg p-6 w-full max-w-md">
        <h2 class="text-xl font-bold mb-4 dark:text-white">编辑密钥</h2>
        <div class="space-y-4">
          <div>
            <label class="block text-sm font-medium mb-1 dark:text-gray-200">密钥名称</label>
            <input
              :value="editingKey?.name"
              disabled
              class="w-full px-3 py-2 border rounded-lg bg-gray-100 dark:bg-gray-600 dark:text-gray-300"
            />
          </div>
          <div>
            <label class="block text-sm font-medium mb-1 dark:text-gray-200">限流 (次/分钟)</label>
            <input
              v-model.number="editRateLimit"
              type="number"
              min="1"
              class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white"
            />
          </div>
          <div>
            <label class="block text-sm font-medium mb-1 dark:text-gray-200">配额限制</label>
            <input
              v-model.number="editQuotaLimit"
              type="number"
              min="0"
              class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white"
              placeholder="留空表示无限制"
            />
          </div>
          <div>
            <label class="block text-sm font-medium mb-1 dark:text-gray-200">过期时间</label>
            <input
              v-model="editExpiredAt"
              type="date"
              class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white"
            />
          </div>
        </div>
        <div class="mt-6 flex justify-end gap-3">
          <button
            @click="showEditModal = false"
            class="px-4 py-2 text-gray-600 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-lg"
          >
            取消
          </button>
          <button
            @click="updateKey"
            :disabled="editing"
            class="px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50"
          >
            {{ editing ? '保存中...' : '保存' }}
          </button>
        </div>
      </div>
    </div>

    <!-- 显示明文密钥模态框 -->
    <div v-if="showRawKeyModal" class="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
      <div class="bg-white dark:bg-gray-800 rounded-lg shadow-lg p-6 w-full max-w-md">
        <h2 class="text-xl font-bold mb-4 dark:text-white text-green-600">密钥已生成</h2>
        <p class="text-red-600 dark:text-red-400 mb-4 font-medium">请立即保存此密钥，系统不会再次显示！</p>
        <div class="bg-gray-100 dark:bg-gray-700 p-3 rounded-lg mb-4 break-all">
          <code class="text-sm dark:text-white">{{ createdRawKey }}</code>
        </div>
        <div class="flex justify-end gap-3">
          <button
            @click="copyRawKey"
            class="px-4 py-2 bg-gray-200 dark:bg-gray-600 text-gray-700 dark:text-white rounded-lg hover:bg-gray-300 dark:hover:bg-gray-500"
          >
            复制密钥
          </button>
          <button
            @click="closeRawKeyModal"
            class="px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700"
          >
            我已保存
          </button>
        </div>
      </div>
    </div>
  </div>
</template>