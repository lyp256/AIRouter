<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { userApi } from '@/api/user'
import type { User, UserKey } from '@/api/types'

const users = ref<User[]>([])
const loading = ref(false)
const showModal = ref(false)
const showKeyModal = ref(false)
const selectedUser = ref<User | null>(null)
const userKeys = ref<UserKey[]>([])
const newRawKey = ref('')

const newUser = ref({
  username: '',
  email: '',
  password: '',
  role: 'user'
})

const newKey = ref({
  name: '',
  user_id: '',
  rate_limit: 60,
  quota_limit: 0
})

async function loadUsers() {
  loading.value = true
  try {
    const res = await userApi.list()
    users.value = res.data
  } finally {
    loading.value = false
  }
}

async function createUser() {
  try {
    await userApi.create(newUser.value)
    showModal.value = false
    newUser.value = { username: '', email: '', password: '', role: 'user' }
    loadUsers()
  } catch (e) {
    alert('创建失败')
  }
}

async function deleteUser(id: string) {
  if (!confirm('确定删除此用户？')) return
  try {
    await userApi.delete(id)
    loadUsers()
  } catch (e) {
    alert('删除失败')
  }
}

async function toggleStatus(user: User) {
  const newStatus = user.status === 'active' ? 'disabled' : 'active'
  try {
    await userApi.update(user.id, { status: newStatus })
    loadUsers()
  } catch (e) {
    alert('操作失败')
  }
}

async function showKeys(user: User) {
  selectedUser.value = user
  newRawKey.value = ''
  const res = await userApi.get(user.id)
  userKeys.value = res.keys
  showKeyModal.value = true
}

async function createKey() {
  if (!selectedUser.value) return
  try {
    newKey.value.user_id = selectedUser.value.id
    const res = await userApi.createKey(newKey.value)
    newRawKey.value = res.raw_key
    newKey.value = { name: '', user_id: '', rate_limit: 60, quota_limit: 0 }
    const userRes = await userApi.get(selectedUser.value.id)
    userKeys.value = userRes.keys
  } catch (e) {
    alert('创建密钥失败')
  }
}

async function deleteKey(keyId: string) {
  if (!confirm('确定删除此密钥？')) return
  try {
    await userApi.deleteKey(keyId)
    if (selectedUser.value) {
      const res = await userApi.get(selectedUser.value.id)
      userKeys.value = res.keys
    }
  } catch (e) {
    alert('删除失败')
  }
}

async function toggleKeyStatus(key: UserKey) {
  const newStatus = key.status === 'active' ? 'disabled' : 'active'
  try {
    await userApi.updateKey(key.id, { status: newStatus })
    if (selectedUser.value) {
      const res = await userApi.get(selectedUser.value.id)
      userKeys.value = res.keys
    }
  } catch (e) {
    alert('操作失败')
  }
}

function copyKey() {
  if (newRawKey.value) {
    navigator.clipboard.writeText(newRawKey.value)
    alert('已复制到剪贴板')
  }
}

onMounted(loadUsers)
</script>

<template>
  <div class="p-6">
    <div class="flex justify-between items-center mb-6">
      <h1 class="text-2xl font-bold text-gray-800 dark:text-white">用户管理</h1>
      <button @click="showModal = true" class="px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700">
        添加用户
      </button>
    </div>

    <div class="bg-white dark:bg-gray-800 rounded-lg shadow">
      <table class="w-full">
        <thead class="bg-gray-50 dark:bg-gray-700">
          <tr>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300 uppercase">用户名</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300 uppercase">邮箱</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300 uppercase">角色</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300 uppercase">状态</th>
            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300 uppercase">创建时间</th>
            <th class="px-6 py-3 text-right text-xs font-medium text-gray-500 dark:text-gray-300 uppercase">操作</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-200 dark:divide-gray-700">
          <tr v-for="u in users" :key="u.id">
            <td class="px-6 py-4 text-sm font-medium text-gray-900 dark:text-white">{{ u.username }}</td>
            <td class="px-6 py-4 text-sm text-gray-500 dark:text-gray-400">{{ u.email }}</td>
            <td class="px-6 py-4 text-sm text-gray-500 dark:text-gray-400">{{ u.role }}</td>
            <td class="px-6 py-4">
              <span
                :class="u.status === 'active' ? 'bg-green-100 text-green-800' : 'bg-red-100 text-red-800'"
                class="px-2 py-1 text-xs rounded-full"
              >
                {{ u.status === 'active' ? '正常' : '禁用' }}
              </span>
            </td>
            <td class="px-6 py-4 text-sm text-gray-500 dark:text-gray-400">{{ new Date(u.created_at).toLocaleDateString() }}</td>
            <td class="px-6 py-4 text-right space-x-2">
              <button @click="toggleStatus(u)" class="text-blue-600 hover:text-blue-800 text-sm">
                {{ u.status === 'active' ? '禁用' : '启用' }}
              </button>
              <button @click="showKeys(u)" class="text-green-600 hover:text-green-800 text-sm">密钥</button>
              <button @click="deleteUser(u.id)" class="text-red-600 hover:text-red-800 text-sm">删除</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- 创建用户弹窗 -->
    <div v-if="showModal" class="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div class="bg-white dark:bg-gray-800 rounded-lg p-6 w-full max-w-md">
        <h2 class="text-lg font-bold mb-4 dark:text-white">添加用户</h2>
        <form @submit.prevent="createUser">
          <div class="space-y-4">
            <div>
              <label class="block text-sm font-medium mb-1 dark:text-gray-200">用户名</label>
              <input v-model="newUser.username" class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white" required />
            </div>
            <div>
              <label class="block text-sm font-medium mb-1 dark:text-gray-200">邮箱</label>
              <input v-model="newUser.email" type="email" class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white" required />
            </div>
            <div>
              <label class="block text-sm font-medium mb-1 dark:text-gray-200">密码</label>
              <input v-model="newUser.password" type="password" class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white" required />
            </div>
            <div>
              <label class="block text-sm font-medium mb-1 dark:text-gray-200">角色</label>
              <select v-model="newUser.role" class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white">
                <option value="user">普通用户</option>
                <option value="admin">管理员</option>
              </select>
            </div>
          </div>
          <div class="flex justify-end gap-2 mt-6">
            <button type="button" @click="showModal = false" class="px-4 py-2 border rounded-lg dark:text-white">取消</button>
            <button type="submit" class="px-4 py-2 bg-blue-600 text-white rounded-lg">创建</button>
          </div>
        </form>
      </div>
    </div>

    <!-- 密钥管理弹窗 -->
    <div v-if="showKeyModal" class="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div class="bg-white dark:bg-gray-800 rounded-lg p-6 w-full max-w-2xl max-h-[80vh] overflow-auto">
        <div class="flex justify-between items-center mb-4">
          <h2 class="text-lg font-bold dark:text-white">{{ selectedUser?.username }} - 密钥管理</h2>
          <button @click="showKeyModal = false" class="text-gray-500">&times;</button>
        </div>

        <!-- 新密钥提示 -->
        <div v-if="newRawKey" class="mb-4 p-4 bg-green-50 dark:bg-green-900/20 border border-green-200 rounded-lg">
          <p class="text-sm text-green-800 dark:text-green-200 mb-2">新密钥已创建，请妥善保存：</p>
          <div class="flex items-center gap-2">
            <code class="flex-1 p-2 bg-white dark:bg-gray-700 rounded text-sm">{{ newRawKey }}</code>
            <button @click="copyKey" class="px-3 py-1 bg-green-600 text-white rounded text-sm">复制</button>
          </div>
        </div>

        <!-- 添加密钥表单 -->
        <div class="mb-4 p-4 bg-gray-50 dark:bg-gray-700 rounded-lg">
          <div class="grid grid-cols-3 gap-4">
            <input v-model="newKey.name" placeholder="密钥名称" class="px-3 py-2 border rounded-lg dark:bg-gray-600 dark:text-white" />
            <input v-model.number="newKey.rate_limit" type="number" placeholder="请求限制/分钟" class="px-3 py-2 border rounded-lg dark:bg-gray-600 dark:text-white" />
            <input v-model.number="newKey.quota_limit" type="number" placeholder="配额限制" class="px-3 py-2 border rounded-lg dark:bg-gray-600 dark:text-white" />
          </div>
          <button @click="createKey" class="mt-2 px-4 py-2 bg-green-600 text-white rounded-lg text-sm">创建密钥</button>
        </div>

        <!-- 密钥列表 -->
        <table class="w-full">
          <thead class="bg-gray-50 dark:bg-gray-700">
            <tr>
              <th class="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-300">名称</th>
              <th class="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-300">限流</th>
              <th class="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-300">配额</th>
              <th class="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-300">状态</th>
              <th class="px-4 py-2 text-right text-xs font-medium text-gray-500 dark:text-gray-300">操作</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-200 dark:divide-gray-700">
            <tr v-for="k in userKeys" :key="k.id">
              <td class="px-4 py-2 text-sm dark:text-white">{{ k.name }}</td>
              <td class="px-4 py-2 text-sm dark:text-gray-300">{{ k.rate_limit }}/min</td>
              <td class="px-4 py-2 text-sm dark:text-gray-300">{{ k.quota_used }} / {{ k.quota_limit || '∞' }}</td>
              <td class="px-4 py-2">
                <span :class="k.status === 'active' ? 'text-green-600' : 'text-red-600'" class="text-sm">{{ k.status === 'active' ? '正常' : '禁用' }}</span>
              </td>
              <td class="px-4 py-2 text-right space-x-2">
                <button @click="toggleKeyStatus(k)" class="text-blue-600 text-sm">{{ k.status === 'active' ? '禁用' : '启用' }}</button>
                <button @click="deleteKey(k.id)" class="text-red-600 text-sm">删除</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>