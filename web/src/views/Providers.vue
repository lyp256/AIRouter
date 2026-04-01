<script setup lang="ts">
import { ref, onMounted, watch, computed } from 'vue'
import { providerApi } from '@/api/provider'
import type { Provider, ProviderKey } from '@/api/types'

const providers = ref<Provider[]>([])
const loading = ref(false)
const showCreateModal = ref(false)
const showEditModal = ref(false)
const showKeyModal = ref(false)
const selectedProvider = ref<Provider | null>(null)
const providerKeys = ref<ProviderKey[]>([])

const newProvider = ref({
  name: '',
  type: 'openai',
  base_url: '',
  api_path: '',
  description: ''
})

// 编辑表单（独立于创建表单）
const editProvider = ref<Provider | null>(null)

const newKey = ref({
  name: '',
  key: ''
})

// 编辑密钥表单
const editKey = ref<ProviderKey | null>(null)
const showEditKeyModal = ref(false)
const editKeyForm = ref({
  name: '',
  key: '',
  status: '',
  quota_limit: 0
})

// 供应商类型配置
const providerTypeConfigs = {
  openai: {
    label: 'OpenAI',
    defaultBaseUrl: 'https://api.openai.com',
    defaultApiPath: '/v1/chat/completions',
    baseUrlPlaceholder: 'https://api.openai.com',
    apiPathPlaceholder: '/v1/chat/completions（留空使用默认）'
  },
  anthropic: {
    label: 'Anthropic',
    defaultBaseUrl: 'https://api.anthropic.com',
    defaultApiPath: '/v1/messages',
    baseUrlPlaceholder: 'https://api.anthropic.com',
    apiPathPlaceholder: '/v1/messages（留空使用默认）'
  },
  openai_compatible: {
    label: 'OpenAI 兼容',
    defaultBaseUrl: '',
    defaultApiPath: '/v1/chat/completions',
    baseUrlPlaceholder: '例如: https://api.deepseek.com',
    apiPathPlaceholder: '例如: /v1/chat/completions'
  }
}

const providerTypes = Object.entries(providerTypeConfigs).map(([value, config]) => ({
  value,
  label: config.label
}))

// 当前类型配置（computed 确保响应式）
const currentConfig = computed(() => providerTypeConfigs[newProvider.value.type as keyof typeof providerTypeConfigs])

// 监听类型变化，自动填充默认值
watch(() => newProvider.value.type, (newType, oldType) => {
  // 类型切换时更新默认值
  const config = providerTypeConfigs[newType as keyof typeof providerTypeConfigs]
  if (config) {
    // 如果是首次打开或切换类型，更新为对应类型的默认值
    const oldConfig = oldType ? providerTypeConfigs[oldType as keyof typeof providerTypeConfigs] : null
    // 如果当前值是旧类型的默认值，或者是空的，则更新为新类型的默认值
    if (!newProvider.value.base_url || (oldConfig && newProvider.value.base_url === oldConfig.defaultBaseUrl)) {
      newProvider.value.base_url = config.defaultBaseUrl
    }
    if (!newProvider.value.api_path || (oldConfig && newProvider.value.api_path === oldConfig.defaultApiPath)) {
      newProvider.value.api_path = config.defaultApiPath
    }
  }
})

async function loadProviders() {
  loading.value = true
  try {
    const res = await providerApi.list()
    providers.value = res.data
  } finally {
    loading.value = false
  }
}

async function createProvider() {
  try {
    await providerApi.create(newProvider.value)
    showCreateModal.value = false
    resetNewProvider()
    loadProviders()
  } catch (e) {
    alert('创建失败')
  }
}

// 打开编辑弹窗
function openEditModal(provider: Provider) {
  editProvider.value = { ...provider }
  showEditModal.value = true
}

// 保存编辑
async function saveEditProvider() {
  if (!editProvider.value) return
  try {
    await providerApi.update(editProvider.value.id, {
      name: editProvider.value.name,
      base_url: editProvider.value.base_url,
      api_path: editProvider.value.api_path,
      description: editProvider.value.description
    })
    showEditModal.value = false
    editProvider.value = null
    loadProviders()
  } catch (e) {
    alert('保存失败')
  }
}

// 重置表单并设置默认值
function resetNewProvider() {
  newProvider.value = {
    name: '',
    type: 'openai',
    base_url: providerTypeConfigs.openai.defaultBaseUrl,
    api_path: providerTypeConfigs.openai.defaultApiPath,
    description: ''
  }
}

async function deleteProvider(id: string) {
  if (!confirm('确定删除此供应商？关联的密钥和上游模型也会被删除。')) return
  try {
    await providerApi.delete(id)
    loadProviders()
  } catch (e) {
    alert('删除失败')
  }
}

async function showKeys(provider: Provider) {
  selectedProvider.value = provider
  const res = await providerApi.listKeys(provider.id)
  providerKeys.value = res.data
  showKeyModal.value = true
}

async function createKey() {
  if (!selectedProvider.value) return
  try {
    await providerApi.createKey(selectedProvider.value.id, newKey.value)
    newKey.value = { name: '', key: '' }
    const res = await providerApi.listKeys(selectedProvider.value.id)
    providerKeys.value = res.data
  } catch (e) {
    alert('创建密钥失败')
  }
}

async function deleteKey(keyId: string) {
  if (!confirm('确定删除此密钥？')) return
  try {
    await providerApi.deleteKey(keyId)
    if (selectedProvider.value) {
      const res = await providerApi.listKeys(selectedProvider.value.id)
      providerKeys.value = res.data
    }
  } catch (e: any) {
    alert(e.response?.data?.error || '删除失败')
  }
}

async function toggleProvider(provider: Provider) {
  try {
    await providerApi.update(provider.id, { enabled: !provider.enabled })
    loadProviders()
  } catch (e) {
    alert('操作失败')
  }
}

// 打开编辑密钥弹窗
function openEditKeyModal(key: ProviderKey) {
  editKey.value = key
  editKeyForm.value = {
    name: key.name,
    key: '', // 密钥不回显，留空表示不修改
    status: key.status,
    quota_limit: key.quota_limit
  }
  showEditKeyModal.value = true
}

// 保存编辑密钥
async function saveEditKey() {
  if (!editKey.value) return
  try {
    const data: Partial<ProviderKey> & { key?: string } = {}
    if (editKeyForm.value.name) data.name = editKeyForm.value.name
    if (editKeyForm.value.key) data.key = editKeyForm.value.key // 只有输入了新密钥才更新
    if (editKeyForm.value.status) data.status = editKeyForm.value.status
    data.quota_limit = editKeyForm.value.quota_limit

    await providerApi.updateKey(editKey.value.id, data)
    showEditKeyModal.value = false
    editKey.value = null
    if (selectedProvider.value) {
      const res = await providerApi.listKeys(selectedProvider.value.id)
      providerKeys.value = res.data
    }
  } catch (e) {
    alert('保存失败')
  }
}

onMounted(loadProviders)
</script>

<template>
  <div class="p-6">
    <div class="flex justify-between items-center mb-6">
      <h1 class="text-2xl font-bold text-gray-800 dark:text-white">供应商管理</h1>
      <div class="flex gap-2">
        <button
          @click="loadProviders"
          :disabled="loading"
          class="px-3 py-2 border border-gray-300 dark:border-gray-600 text-gray-600 dark:text-gray-300 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-700 disabled:opacity-50"
          title="刷新"
        >
          <svg class="w-4 h-4" :class="{ 'animate-spin': loading }" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
          </svg>
        </button>
        <button
          @click="showCreateModal = true; resetNewProvider()"
          class="px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700"
        >
          添加供应商
        </button>
      </div>
    </div>

    <!-- 供应商列表 -->
    <div v-if="loading" class="flex items-center justify-center py-16 bg-white dark:bg-gray-800 rounded-lg shadow">
      <svg class="animate-spin w-6 h-6 text-blue-500 mr-3" fill="none" viewBox="0 0 24 24">
        <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
        <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
      </svg>
      <span class="text-gray-500 dark:text-gray-400">加载中...</span>
    </div>
    <div v-else class="bg-white dark:bg-gray-800 rounded-lg shadow">
      <table class="w-full">
        <thead class="bg-gray-50 dark:bg-gray-700">
          <tr>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300 uppercase">名称</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300 uppercase">类型</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300 uppercase">Base URL</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300 uppercase">API路径</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300 uppercase">状态</th>
            <th class="px-6 py-3 text-right text-xs font-medium text-gray-500 dark:text-gray-300 uppercase">操作</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-200 dark:divide-gray-700">
          <tr v-for="p in providers" :key="p.id">
            <td class="px-6 py-4 text-sm text-gray-900 dark:text-white">{{ p.name }}</td>
            <td class="px-6 py-4 text-sm text-gray-500 dark:text-gray-400">{{ p.type }}</td>
            <td class="px-6 py-4 text-sm text-gray-500 dark:text-gray-400">{{ p.base_url }}</td>
            <td class="px-6 py-4 text-sm text-gray-500 dark:text-gray-400">{{ p.api_path || '(默认)' }}</td>
            <td class="px-6 py-4">
              <span
                :class="p.enabled ? 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200' : 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200'"
                class="px-2 py-1 text-xs rounded-full"
              >
                {{ p.enabled ? '启用' : '禁用' }}
              </span>
            </td>
            <td class="px-6 py-4 text-right space-x-2">
              <button @click="openEditModal(p)" class="text-blue-600 hover:text-blue-800 text-sm">编辑</button>
              <button @click="toggleProvider(p)" class="text-blue-600 hover:text-blue-800 text-sm">
                {{ p.enabled ? '禁用' : '启用' }}
              </button>
              <button @click="showKeys(p)" class="text-green-600 hover:text-green-800 text-sm">密钥</button>
              <button @click="deleteProvider(p.id)" class="text-red-600 hover:text-red-800 text-sm">删除</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- 创建供应商弹窗 -->
    <div v-if="showCreateModal" class="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div class="bg-white dark:bg-gray-800 rounded-lg p-6 w-full max-w-md">
        <h2 class="text-lg font-bold mb-4 dark:text-white">添加供应商</h2>
        <form @submit.prevent="createProvider">
          <div class="space-y-4">
            <div>
              <label class="block text-sm font-medium mb-1 dark:text-gray-200">名称</label>
              <input v-model="newProvider.name" class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white" required />
            </div>
            <div>
              <label class="block text-sm font-medium mb-1 dark:text-gray-200">类型</label>
              <select v-model="newProvider.type" class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white">
                <option v-for="t in providerTypes" :key="t.value" :value="t.value">{{ t.label }}</option>
              </select>
            </div>
            <div>
              <label class="block text-sm font-medium mb-1 dark:text-gray-200">Base URL</label>
              <input v-model="newProvider.base_url" class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white" :placeholder="currentConfig.baseUrlPlaceholder" />
            </div>
            <div>
              <label class="block text-sm font-medium mb-1 dark:text-gray-200">API 路径</label>
              <input v-model="newProvider.api_path" class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white" :placeholder="currentConfig.apiPathPlaceholder" />
              <p class="text-xs text-gray-500 mt-1">留空则使用默认路径</p>
            </div>
            <div>
              <label class="block text-sm font-medium mb-1 dark:text-gray-200">描述</label>
              <textarea v-model="newProvider.description" class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white" rows="2"></textarea>
            </div>
          </div>
          <div class="flex justify-end gap-2 mt-6">
            <button type="button" @click="showCreateModal = false" class="px-4 py-2 border rounded-lg dark:text-white">取消</button>
            <button type="submit" class="px-4 py-2 bg-blue-600 text-white rounded-lg">创建</button>
          </div>
        </form>
      </div>
    </div>

    <!-- 编辑供应商弹窗 -->
    <div v-if="showEditModal && editProvider" class="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div class="bg-white dark:bg-gray-800 rounded-lg p-6 w-full max-w-md">
        <h2 class="text-lg font-bold mb-4 dark:text-white">编辑供应商</h2>
        <form @submit.prevent="saveEditProvider">
          <div class="space-y-4">
            <div>
              <label class="block text-sm font-medium mb-1 dark:text-gray-200">名称</label>
              <input v-model="editProvider.name" class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white" required />
            </div>
            <div>
              <label class="block text-sm font-medium mb-1 dark:text-gray-200">类型</label>
              <input :value="editProvider.type" class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white bg-gray-100 dark:bg-gray-600" disabled />
              <p class="text-xs text-gray-500 mt-1">类型创建后不可修改</p>
            </div>
            <div>
              <label class="block text-sm font-medium mb-1 dark:text-gray-200">Base URL</label>
              <input v-model="editProvider.base_url" class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white" placeholder="例如: https://api.openai.com" />
            </div>
            <div>
              <label class="block text-sm font-medium mb-1 dark:text-gray-200">API 路径</label>
              <input v-model="editProvider.api_path" class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white" placeholder="例如: /v1/chat/completions" />
              <p class="text-xs text-gray-500 mt-1">留空则使用默认路径</p>
            </div>
            <div>
              <label class="block text-sm font-medium mb-1 dark:text-gray-200">描述</label>
              <textarea v-model="editProvider.description" class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white" rows="2"></textarea>
            </div>
          </div>
          <div class="flex justify-end gap-2 mt-6">
            <button type="button" @click="showEditModal = false; editProvider = null" class="px-4 py-2 border rounded-lg dark:text-white">取消</button>
            <button type="submit" class="px-4 py-2 bg-blue-600 text-white rounded-lg">保存</button>
          </div>
        </form>
      </div>
    </div>

    <!-- 密钥管理弹窗 -->
    <div v-if="showKeyModal" class="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div class="bg-white dark:bg-gray-800 rounded-lg p-6 w-full max-w-2xl max-h-[80vh] overflow-auto">
        <div class="flex justify-between items-center mb-4">
          <h2 class="text-lg font-bold dark:text-white">{{ selectedProvider?.name }} - 密钥管理</h2>
          <button @click="showKeyModal = false" class="text-gray-500">&times;</button>
        </div>

        <!-- 添加密钥表单 -->
        <div class="mb-4 p-4 bg-gray-50 dark:bg-gray-700 rounded-lg">
          <div class="grid grid-cols-2 gap-4">
            <input v-model="newKey.name" placeholder="密钥名称" class="px-3 py-2 border rounded-lg dark:bg-gray-600 dark:text-white" />
            <input v-model="newKey.key" placeholder="API Key" type="password" class="px-3 py-2 border rounded-lg dark:bg-gray-600 dark:text-white" />
          </div>
          <button @click="createKey" class="mt-2 px-4 py-2 bg-green-600 text-white rounded-lg text-sm">添加密钥</button>
        </div>

        <!-- 密钥列表 -->
        <table class="w-full">
          <thead class="bg-gray-50 dark:bg-gray-700">
            <tr>
              <th class="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-300">名称</th>
              <th class="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-300">状态</th>
              <th class="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-300">配额</th>
              <th class="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-300">最后使用</th>
              <th class="px-4 py-2 text-right text-xs font-medium text-gray-500 dark:text-gray-300">操作</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-200 dark:divide-gray-700">
            <tr v-for="k in providerKeys" :key="k.id">
              <td class="px-4 py-2 text-sm dark:text-white">{{ k.name }}</td>
              <td class="px-4 py-2">
                <span
                  :class="k.status === 'active' ? 'text-green-600 dark:text-green-400' : 'text-red-600 dark:text-red-400'"
                  class="text-sm"
                >
                  {{ k.status }}
                </span>
              </td>
              <td class="px-4 py-2 text-sm dark:text-gray-300">
                {{ k.quota_used }} / {{ k.quota_limit > 0 ? k.quota_limit : '∞' }}
              </td>
              <td class="px-4 py-2 text-sm dark:text-gray-300">
                {{ k.last_used_at ? new Date(k.last_used_at).toLocaleString() : '-' }}
              </td>
              <td class="px-4 py-2 text-right">
                <button @click="openEditKeyModal(k)" class="text-blue-600 hover:text-blue-800 text-sm mr-2">编辑</button>
                <button @click="deleteKey(k.id)" class="text-red-600 hover:text-red-800 text-sm">删除</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- 编辑密钥弹窗 -->
    <div v-if="showEditKeyModal && editKey" class="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div class="bg-white dark:bg-gray-800 rounded-lg p-6 w-full max-w-md">
        <h2 class="text-lg font-bold mb-4 dark:text-white">编辑密钥</h2>
        <form @submit.prevent="saveEditKey">
          <div class="space-y-4">
            <div>
              <label class="block text-sm font-medium mb-1 dark:text-gray-200">名称</label>
              <input v-model="editKeyForm.name" class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white" required />
            </div>
            <div>
              <label class="block text-sm font-medium mb-1 dark:text-gray-200">新密钥</label>
              <input v-model="editKeyForm.key" type="password" class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white" placeholder="留空表示不修改" />
              <p class="text-xs text-gray-500 mt-1">留空则保持原密钥不变</p>
            </div>
            <div>
              <label class="block text-sm font-medium mb-1 dark:text-gray-200">状态</label>
              <select v-model="editKeyForm.status" class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white">
                <option value="active">启用</option>
                <option value="disabled">禁用</option>
              </select>
            </div>
            <div>
              <label class="block text-sm font-medium mb-1 dark:text-gray-200">配额限制</label>
              <input v-model.number="editKeyForm.quota_limit" type="number" min="0" class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white" />
              <p class="text-xs text-gray-500 mt-1">0 表示无限制</p>
            </div>
          </div>
          <div class="flex justify-end gap-2 mt-6">
            <button type="button" @click="showEditKeyModal = false; editKey = null" class="px-4 py-2 border rounded-lg dark:text-white">取消</button>
            <button type="submit" class="px-4 py-2 bg-blue-600 text-white rounded-lg">保存</button>
          </div>
        </form>
      </div>
    </div>
  </div>
</template>