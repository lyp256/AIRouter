<script setup lang="ts">
import { ref } from 'vue'
import { useUserStore } from '@/stores/user'
import { authApi } from '@/api/auth'

const userStore = useUserStore()

const oldPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
const message = ref('')
const error = ref('')

async function changePassword() {
  error.value = ''
  message.value = ''

  if (!oldPassword.value || !newPassword.value) {
    error.value = '请填写所有字段'
    return
  }

  if (newPassword.value !== confirmPassword.value) {
    error.value = '两次输入的密码不一致'
    return
  }

  if (newPassword.value.length < 6) {
    error.value = '密码长度至少6位'
    return
  }

  try {
    await authApi.changePassword(oldPassword.value, newPassword.value)
    message.value = '密码修改成功'
    oldPassword.value = ''
    newPassword.value = ''
    confirmPassword.value = ''
  } catch (e: unknown) {
    const err = e as { response?: { data?: { error?: string } } }
    error.value = err.response?.data?.error || '修改失败'
  }
}
</script>

<template>
  <div class="p-6">
    <h1 class="text-2xl font-bold text-gray-800 dark:text-white mb-6">系统设置</h1>

    <div class="max-w-2xl">
      <!-- 当前用户信息 -->
      <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-6 mb-6">
        <h2 class="text-lg font-bold text-gray-800 dark:text-white mb-4">账户信息</h2>
        <div class="space-y-2">
          <div class="flex">
            <span class="w-24 text-gray-500 dark:text-gray-400">用户名</span>
            <span class="dark:text-white">{{ userStore.user?.username }}</span>
          </div>
          <div class="flex">
            <span class="w-24 text-gray-500 dark:text-gray-400">邮箱</span>
            <span class="dark:text-white">{{ userStore.user?.email }}</span>
          </div>
          <div class="flex">
            <span class="w-24 text-gray-500 dark:text-gray-400">角色</span>
            <span class="dark:text-white">{{ userStore.user?.role }}</span>
          </div>
        </div>
      </div>

      <!-- 修改密码 -->
      <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-6">
        <h2 class="text-lg font-bold text-gray-800 dark:text-white mb-4">修改密码</h2>
        <form @submit.prevent="changePassword">
          <div class="space-y-4">
            <div>
              <label class="block text-sm font-medium mb-1 dark:text-gray-200">当前密码</label>
              <input
                v-model="oldPassword"
                type="password"
                class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white"
              />
            </div>
            <div>
              <label class="block text-sm font-medium mb-1 dark:text-gray-200">新密码</label>
              <input
                v-model="newPassword"
                type="password"
                class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white"
              />
            </div>
            <div>
              <label class="block text-sm font-medium mb-1 dark:text-gray-200">确认新密码</label>
              <input
                v-model="confirmPassword"
                type="password"
                class="w-full px-3 py-2 border rounded-lg dark:bg-gray-700 dark:text-white"
              />
            </div>
          </div>

          <p v-if="error" class="text-red-500 text-sm mt-2">{{ error }}</p>
          <p v-if="message" class="text-green-500 text-sm mt-2">{{ message }}</p>

          <button type="submit" class="mt-4 px-4 py-2 bg-blue-600 text-white rounded-lg">
            修改密码
          </button>
        </form>
      </div>
    </div>
  </div>
</template>