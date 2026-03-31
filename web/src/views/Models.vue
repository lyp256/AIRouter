<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { modelApi, upstreamApi } from '@/api/model'
import { providerApi } from '@/api/provider'
import type { Model, ModelWithUpstreams, Upstream, Provider, ProviderKey, UpstreamTestResult } from '@/api/types'
import { storageToDisplay, displayToStorage, formatBU } from '@/utils/format'

const models = ref<Model[]>([])
const modelDetails = ref<Map<string, ModelWithUpstreams>>(new Map())
const providers = ref<Provider[]>([])
const providerKeys = ref<Map<string, ProviderKey[]>>(new Map())
const loading = ref(false)
const showModelModal = ref(false)
const showUpstreamModal = ref(false)
const editingModel = ref<Model | null>(null)
const editingUpstream = ref<Upstream | null>(null)
const currentModelId = ref('')
const expandedModels = ref<Set<string>>(new Set())

// 上游模型测试状态
const testingUpstreamId = ref<string | null>(null)
const testResults = ref<Map<string, UpstreamTestResult>>(new Map())
const testingModelId = ref<string | null>(null) // 批量测试中的模型 ID

// 表单使用 BU/M tokens 单位（用户友好）
const modelForm = ref({
  name: '',
  provider_type: 'anthropic', // 默认类型
  description: '',
  input_price: 0,  // BU/M tokens
  output_price: 0, // BU/M tokens
  context_window: 4096
})

const upstreamForm = ref({
  provider_id: '',
  provider_key_id: '',
  provider_model: '',
  weight: 1,
  priority: 0
})

async function loadData() {
  loading.value = true
  try {
    const [modelRes, providerRes] = await Promise.all([
      modelApi.adminList(),
      providerApi.list()
    ])
    models.value = modelRes.data
    providers.value = providerRes.data

    // 加载所有供应商的密钥
    for (const p of providerRes.data) {
      const keyRes = await providerApi.listKeys(p.id)
      providerKeys.value.set(p.id, keyRes.data)
    }
  } finally {
    loading.value = false
  }
}

async function toggleExpand(modelId: string) {
  if (expandedModels.value.has(modelId)) {
    expandedModels.value.delete(modelId)
  } else {
    expandedModels.value.add(modelId)
    // 加载模型详情（包含上游模型）
    if (!modelDetails.value.has(modelId)) {
      try {
        const res = await modelApi.get(modelId)
        modelDetails.value.set(modelId, res.data)
      } catch (e) {
        console.error('加载模型详情失败', e)
      }
    }
  }
}

function openCreateModelModal() {
  editingModel.value = null
  modelForm.value = {
    name: '',
    provider_type: 'anthropic',
    description: '',
    input_price: 0,
    output_price: 0,
    context_window: 4096
  }
  showModelModal.value = true
}

function openEditModelModal(model: Model) {
  editingModel.value = model
  modelForm.value = {
    name: model.name,
    provider_type: model.provider_type,
    description: model.description || '',
    input_price: storageToDisplay(model.input_price),  // 转换为 BU/M 显示
    output_price: storageToDisplay(model.output_price), // 转换为 BU/M 显示
    context_window: model.context_window
  }
  showModelModal.value = true
}

async function saveModel() {
  try {
    // 提交时将 BU/M 转换为存储格式（纳 BU/K）
    const payload = {
      ...modelForm.value,
      input_price: displayToStorage(modelForm.value.input_price),
      output_price: displayToStorage(modelForm.value.output_price)
    }
    if (editingModel.value) {
      await modelApi.update(editingModel.value.id, payload)
    } else {
      await modelApi.create(payload)
    }
    showModelModal.value = false
    loadData()
  } catch (e) {
    alert('保存失败')
  }
}

async function deleteModel(id: string) {
  if (!confirm('确定删除此模型？关联的上游模型也会被删除。')) return
  try {
    await modelApi.delete(id)
    modelDetails.value.delete(id)
    expandedModels.value.delete(id)
    loadData()
  } catch (e) {
    alert('删除失败')
  }
}

async function toggleModel(model: Model) {
  try {
    await modelApi.toggle(model.id)
    loadData()
  } catch (e) {
    alert('操作失败')
  }
}

// 上游模型操作
function openCreateUpstreamModal(modelId: string) {
  currentModelId.value = modelId
  editingUpstream.value = null
  upstreamForm.value = {
    provider_id: '',
    provider_key_id: '',
    provider_model: '',
    weight: 1,
    priority: 0
  }
  showUpstreamModal.value = true
}

function openEditUpstreamModal(upstream: Upstream, modelId: string) {
  currentModelId.value = modelId
  editingUpstream.value = upstream
  upstreamForm.value = {
    provider_id: upstream.provider_id,
    provider_key_id: upstream.provider_key_id,
    provider_model: upstream.provider_model,
    weight: upstream.weight,
    priority: upstream.priority
  }
  showUpstreamModal.value = true
}

async function saveUpstream() {
  try {
    if (editingUpstream.value) {
      await upstreamApi.update(editingUpstream.value.id, upstreamForm.value)
    } else {
      await upstreamApi.create(currentModelId.value, upstreamForm.value)
    }
    showUpstreamModal.value = false
    // 重新加载模型详情
    modelDetails.value.delete(currentModelId.value)
    const res = await modelApi.get(currentModelId.value)
    modelDetails.value.set(currentModelId.value, res.data)
  } catch (e) {
    alert('保存失败')
  }
}

async function deleteUpstream(upstreamId: string, modelId: string) {
  if (!confirm('确定删除此上游模型？')) return
  try {
    await upstreamApi.delete(upstreamId)
    modelDetails.value.delete(modelId)
    const res = await modelApi.get(modelId)
    modelDetails.value.set(modelId, res.data)
  } catch (e) {
    alert('删除失败')
  }
}

async function toggleUpstream(upstream: Upstream, modelId: string) {
  try {
    await upstreamApi.toggle(upstream.id)
    modelDetails.value.delete(modelId)
    const res = await modelApi.get(modelId)
    modelDetails.value.set(modelId, res.data)
  } catch (e) {
    alert('操作失败')
  }
}

async function resetUpstreamStatus(upstream: Upstream, modelId: string) {
  try {
    await upstreamApi.resetStatus(upstream.id)
    modelDetails.value.delete(modelId)
    const res = await modelApi.get(modelId)
    modelDetails.value.set(modelId, res.data)
  } catch (e) {
    alert('重置失败')
  }
}

async function testUpstream(upstream: Upstream) {
  testingUpstreamId.value = upstream.id
  testResults.value.delete(upstream.id)
  try {
    const res = await upstreamApi.test(upstream.id)
    testResults.value.set(upstream.id, res.data)
  } catch (e: any) {
    testResults.value.set(upstream.id, {
      success: false,
      latency_ms: 0,
      first_token_latency_ms: 0,
      message: e.response?.data?.error || '测试请求失败',
      upstream_id: upstream.id,
      provider_name: '',
      provider_model: '',
    })
  } finally {
    testingUpstreamId.value = null
  }
}

async function testAllUpstreams(modelId: string) {
  testingModelId.value = modelId
  // 清除该模型下所有上游的旧测试结果
  const upstreams = getUpstreams(modelId)
  for (const u of upstreams) {
    testResults.value.delete(u.id)
  }
  try {
    const res = await modelApi.testUpstreams(modelId)
    for (const r of res.data) {
      testResults.value.set(r.upstream_id, r)
    }
  } catch (e) {
    alert('批量测试失败')
  } finally {
    testingModelId.value = null
  }
}

function getProviderName(providerId: string) {
  const p = providers.value.find(x => x.id === providerId)
  return p?.name || providerId
}

function getProviderKeys(providerId: string) {
  return providerKeys.value.get(providerId) || []
}

function getKeyProviderName(keyId: string) {
  for (const keys of providerKeys.value.values()) {
    const key = keys.find(k => k.id === keyId)
    if (key) return key.name
  }
  return keyId
}

function getUpstreams(modelId: string): Upstream[] {
  const detail = modelDetails.value.get(modelId)
  return detail?.upstreams || []
}

// 获取当前模型的供应商类型
function getCurrentModelType(): string {
  const model = models.value.find(m => m.id === currentModelId.value)
  return model?.provider_type || 'openai_compatible'
}

// 获取过滤后的供应商列表（只显示与模型类型匹配的供应商）
function getFilteredProviders(): Provider[] {
  const modelType = getCurrentModelType()
  return providers.value.filter(p => p.type === modelType)
}

onMounted(loadData)
</script>

<template>
  <div class="p-6">
    <div class="flex justify-between items-center mb-6">
      <h1 class="text-2xl font-bold text-gray-800 dark:text-white">模型管理</h1>
      <button @click="openCreateModelModal" class="px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700">
        添加模型
      </button>
    </div>

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
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300 uppercase w-8"></th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300 uppercase">模型名称</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300 uppercase">类型</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300 uppercase">描述</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300 uppercase">输入价格</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300 uppercase">输出价格</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300 uppercase">状态</th>
            <th class="px-6 py-3 text-right text-xs font-medium text-gray-500 dark:text-gray-300 uppercase">操作</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-200 dark:divide-gray-700">
          <template v-for="m in models" :key="m.id">
            <tr class="hover:bg-gray-50 dark:hover:bg-gray-700">
              <td class="px-6 py-4">
                <button @click="toggleExpand(m.id)" class="text-gray-400 hover:text-gray-600">
                  <svg :class="['w-4 h-4 transition-transform', expandedModels.has(m.id) ? 'rotate-90' : '']" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" />
                  </svg>
                </button>
              </td>
              <td class="px-6 py-4 text-sm font-medium text-gray-900 dark:text-white">{{ m.name }}</td>
              <td class="px-6 py-4">
                <span
                  :class="{
                    'bg-purple-100 text-purple-800 dark:bg-purple-900 dark:text-purple-200': m.provider_type === 'anthropic',
                    'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200': m.provider_type === 'openai',
                    'bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-200': !m.provider_type || m.provider_type === 'openai_compatible'
                  }"
                  class="px-2 py-1 text-xs rounded-full"
                >
                  {{ m.provider_type === 'anthropic' ? 'Anthropic' : m.provider_type === 'openai' ? 'OpenAI' : m.provider_type || '兼容' }}
                </span>
              </td>
              <td class="px-6 py-4 text-sm text-gray-500 dark:text-gray-400">{{ m.description || '-' }}</td>
              <td class="px-6 py-4 text-sm text-gray-500 dark:text-gray-400">{{ formatBU(m.input_price) }}</td>
              <td class="px-6 py-4 text-sm text-gray-500 dark:text-gray-400">{{ formatBU(m.output_price) }}</td>
              <td class="px-6 py-4">
                <span
                  :class="m.enabled ? 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200' : 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200'"
                  class="px-2 py-1 text-xs rounded-full"
                >
                  {{ m.enabled ? '启用' : '禁用' }}
                </span>
              </td>
              <td class="px-6 py-4 text-right space-x-2">
                <button @click="toggleModel(m)" class="text-blue-600 hover:text-blue-800 text-sm">
                  {{ m.enabled ? '禁用' : '启用' }}
                </button>
                <button @click="openEditModelModal(m)" class="text-yellow-600 hover:text-yellow-800 text-sm">编辑</button>
                <button @click="deleteModel(m.id)" class="text-red-600 hover:text-red-800 text-sm">删除</button>
              </td>
            </tr>
            <!-- 上游模型展开行 -->
            <tr v-if="expandedModels.has(m.id)" class="bg-gray-50 dark:bg-gray-900">
              <td colspan="8" class="px-6 py-4">
                <div class="flex justify-between items-center mb-3">
                  <h4 class="text-sm font-medium text-gray-700 dark:text-gray-300">上游模型配置</h4>
                  <div class="flex gap-2">
                    <button
                      @click="testAllUpstreams(m.id)"
                      :disabled="testingModelId === m.id || getUpstreams(m.id).length === 0"
                      class="px-3 py-1 bg-teal-600 text-white text-sm rounded hover:bg-teal-700 disabled:opacity-50 disabled:cursor-not-allowed"
                    >
                      {{ testingModelId === m.id ? '测试中...' : '测试全部' }}
                    </button>
                    <button @click="openCreateUpstreamModal(m.id)" class="px-3 py-1 bg-green-600 text-white text-sm rounded hover:bg-green-700">
                      添加上游模型
                    </button>
                  </div>
                </div>
                <div v-if="getUpstreams(m.id).length === 0" class="text-sm text-gray-500 dark:text-gray-400 py-2">
                  暂无上游模型配置，请添加
                </div>
                <table v-else class="w-full text-sm">
                  <thead>
                    <tr class="text-gray-500 dark:text-gray-400">
                      <th class="py-2 text-left">供应商</th>
                      <th class="py-2 text-left">密钥</th>
                      <th class="py-2 text-left">实际模型</th>
                      <th class="py-2 text-left">权重</th>
                      <th class="py-2 text-left">优先级</th>
                      <th class="py-2 text-left">启用</th>
                      <th class="py-2 text-left">健康状态</th>
                      <th class="py-2 text-right">操作</th>
                    </tr>
                  </thead>
                  <tbody class="divide-y divide-gray-200 dark:divide-gray-700">
                    <tr v-for="u in getUpstreams(m.id)" :key="u.id">
                      <td class="py-2 text-gray-700 dark:text-gray-300">{{ u.provider_name || getProviderName(u.provider_id) }}</td>
                      <td class="py-2 text-gray-700 dark:text-gray-300">{{ u.provider_key_name || getKeyProviderName(u.provider_key_id) }}</td>
                      <td class="py-2 text-gray-700 dark:text-gray-300">{{ u.provider_model }}</td>
                      <td class="py-2 text-gray-700 dark:text-gray-300">{{ u.weight }}</td>
                      <td class="py-2 text-gray-700 dark:text-gray-300">{{ u.priority }}</td>
                      <td class="py-2">
                        <span
                          :class="u.enabled ? 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200' : 'bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-300'"
                          class="px-2 py-0.5 text-xs rounded-full"
                        >
                          {{ u.enabled ? '启用' : '禁用' }}
                        </span>
                      </td>
                      <td class="py-2">
                        <span
                          :class="{
                            'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200': u.status === 'active',
                            'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200': u.status === 'error',
                            'bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-300': u.status !== 'active' && u.status !== 'error'
                          }"
                          class="px-2 py-0.5 text-xs rounded-full"
                        >
                          {{ u.status === 'active' ? '健康' : u.status === 'error' ? '异常' : u.status }}
                        </span>
                        <button
                          v-if="u.status === 'error'"
                          @click="resetUpstreamStatus(u, m.id)"
                          class="ml-1 text-xs text-orange-600 hover:text-orange-800 underline"
                        >
                          重置
                        </button>
                        <span v-if="testResults.has(u.id)" class="ml-2">
                          <span
                            :class="testResults.get(u.id)!.success
                              ? 'text-green-600 dark:text-green-400'
                              : 'text-red-600 dark:text-red-400'"
                            class="text-xs"
                            :title="testResults.get(u.id)!.message + (testResults.get(u.id)!.response_content ? '\n响应: ' + testResults.get(u.id)!.response_content : '')"
                          >
                            {{ testResults.get(u.id)!.success
                              ? (testResults.get(u.id)!.first_token_latency_ms
                                ? testResults.get(u.id)!.latency_ms + 'ms / 首 Token ' + testResults.get(u.id)!.first_token_latency_ms + 'ms'
                                : testResults.get(u.id)!.latency_ms + 'ms')
                              : '失败' }}
                          </span>
                        </span>
                      </td>
                      <td class="py-2 text-right space-x-2">
                        <button
                          @click="testUpstream(u)"
                          :disabled="testingUpstreamId === u.id"
                          class="text-green-600 hover:text-green-800 disabled:opacity-50 disabled:cursor-not-allowed"
                        >
                          <span v-if="testingUpstreamId === u.id" class="inline-flex items-center">
                            <svg class="animate-spin w-3 h-3 mr-1" fill="none" viewBox="0 0 24 24">
                              <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                              <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                            </svg>
                            测试中
                          </span>
                          <span v-else>测试</span>
                        </button>
                        <button @click="toggleUpstream(u, m.id)" class="text-blue-600 hover:text-blue-800">
                          {{ u.enabled ? '禁用' : '启用' }}
                        </button>
                        <button @click="openEditUpstreamModal(u, m.id)" class="text-yellow-600 hover:text-yellow-800">编辑</button>
                        <button @click="deleteUpstream(u.id, m.id)" class="text-red-600 hover:text-red-800">删除</button>
                      </td>
                    </tr>
                  </tbody>
                </table>
              </td>
            </tr>
          </template>
        </tbody>
      </table>
    </div>

    <!-- 创建/编辑模型弹窗 -->
    <div v-if="showModelModal" class="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div class="bg-white dark:bg-gray-800 rounded-lg p-6 w-full max-w-md">
        <h2 class="text-lg font-bold mb-4 dark:text-white">{{ editingModel ? '编辑模型' : '添加模型' }}</h2>
        <form @submit.prevent="saveModel">
          <div class="space-y-4">
            <div>
              <label class="block text-sm font-medium mb-1 dark:text-gray-200">模型名称（对外）</label>
              <input v-model="modelForm.name" class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white" required />
            </div>
            <!-- 类型选择：仅创建时可选，编辑时不可修改 -->
            <div v-if="!editingModel">
              <label class="block text-sm font-medium mb-1 dark:text-gray-200">供应商类型 <span class="text-red-500">*</span></label>
              <select v-model="modelForm.provider_type" class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white" required>
                <option value="openai">OpenAI</option>
                <option value="anthropic">Anthropic</option>
                <option value="openai_compatible">兼容模式</option>
              </select>
              <p class="text-xs text-gray-500 mt-1">创建后不可修改</p>
            </div>
            <!-- 编辑时显示类型标签 -->
            <div v-else>
              <label class="block text-sm font-medium mb-1 dark:text-gray-200">供应商类型</label>
              <span
                :class="{
                  'bg-purple-100 text-purple-800 dark:bg-purple-900 dark:text-purple-200': editingModel.provider_type === 'anthropic',
                  'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200': editingModel.provider_type === 'openai',
                  'bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-200': editingModel.provider_type === 'openai_compatible'
                }"
                class="px-3 py-1 text-sm rounded-full"
              >
                {{ editingModel.provider_type === 'anthropic' ? 'Anthropic' : editingModel.provider_type === 'openai' ? 'OpenAI' : '兼容模式' }}
              </span>
            </div>
            <div>
              <label class="block text-sm font-medium mb-1 dark:text-gray-200">描述</label>
              <input v-model="modelForm.description" class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white" />
            </div>
            <div class="grid grid-cols-2 gap-4">
              <div>
                <label class="block text-sm font-medium mb-1 dark:text-gray-200">输入价格 BU/M tokens</label>
                <input v-model.number="modelForm.input_price" type="number" step="0.01" class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white" />
              </div>
              <div>
                <label class="block text-sm font-medium mb-1 dark:text-gray-200">输出价格 BU/M tokens</label>
                <input v-model.number="modelForm.output_price" type="number" step="0.01" class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white" />
              </div>
            </div>
            <div>
              <label class="block text-sm font-medium mb-1 dark:text-gray-200">上下文窗口</label>
              <input v-model.number="modelForm.context_window" type="number" class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white" />
            </div>
          </div>
          <div class="flex justify-end gap-2 mt-6">
            <button type="button" @click="showModelModal = false" class="px-4 py-2 border rounded-lg dark:text-white">取消</button>
            <button type="submit" class="px-4 py-2 bg-blue-600 text-white rounded-lg">保存</button>
          </div>
        </form>
      </div>
    </div>

    <!-- 创建/编辑上游模型弹窗 -->
    <div v-if="showUpstreamModal" class="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div class="bg-white dark:bg-gray-800 rounded-lg p-6 w-full max-w-md">
        <h2 class="text-lg font-bold mb-4 dark:text-white">{{ editingUpstream ? '编辑上游模型' : '添加上游模型' }}</h2>
        <form @submit.prevent="saveUpstream">
          <div class="space-y-4">
            <div>
              <label class="block text-sm font-medium mb-1 dark:text-gray-200">供应商</label>
              <select v-model="upstreamForm.provider_id" class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white" required :disabled="!!editingUpstream">
                <option value="">选择供应商</option>
                <option v-for="p in getFilteredProviders()" :key="p.id" :value="p.id">{{ p.name }} ({{ p.type }})</option>
              </select>
              <p v-if="!editingUpstream" class="text-xs text-gray-500 mt-1">仅显示与模型类型匹配的供应商</p>
            </div>
            <div>
              <label class="block text-sm font-medium mb-1 dark:text-gray-200">供应商密钥</label>
              <select v-model="upstreamForm.provider_key_id" class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white" required>
                <option value="">选择密钥</option>
                <option v-for="k in getProviderKeys(upstreamForm.provider_id)" :key="k.id" :value="k.id">{{ k.name }}</option>
              </select>
            </div>
            <div>
              <label class="block text-sm font-medium mb-1 dark:text-gray-200">实际模型名称</label>
              <input v-model="upstreamForm.provider_model" class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white" placeholder="gpt-4-turbo" required />
            </div>
            <div class="grid grid-cols-2 gap-4">
              <div>
                <label class="block text-sm font-medium mb-1 dark:text-gray-200">权重</label>
                <input v-model.number="upstreamForm.weight" type="number" min="1" class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white" />
              </div>
              <div>
                <label class="block text-sm font-medium mb-1 dark:text-gray-200">优先级</label>
                <input v-model.number="upstreamForm.priority" type="number" class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white" />
                <p class="text-xs text-gray-500 mt-1">数值越大优先级越高</p>
              </div>
            </div>
          </div>
          <div class="flex justify-end gap-2 mt-6">
            <button type="button" @click="showUpstreamModal = false" class="px-4 py-2 border rounded-lg dark:text-white">取消</button>
            <button type="submit" class="px-4 py-2 bg-blue-600 text-white rounded-lg">保存</button>
          </div>
        </form>
      </div>
    </div>
  </div>
</template>