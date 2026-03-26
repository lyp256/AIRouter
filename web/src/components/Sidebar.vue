<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { useUserStore } from '@/stores/user'

const router = useRouter()
const userStore = useUserStore()

// 菜单项配置（管理员菜单需要 requiresAdmin: true）
const allMenuItems = [
  { name: '仪表盘', icon: 'dashboard', path: '/' },
  { name: 'AI 聊天', icon: 'chat', path: '/chat' },
  { name: '供应商管理', icon: 'provider', path: '/providers', requiresAdmin: true },
  { name: '模型管理', icon: 'model', path: '/models', requiresAdmin: true },
  { name: '用户管理', icon: 'user', path: '/users', requiresAdmin: true },
  { name: '密钥管理', icon: 'key', path: '/keys' },
  { name: '统计分析', icon: 'chart', path: '/statistics', requiresAdmin: true },
  { name: '系统设置', icon: 'settings', path: '/settings' },
]

// 根据用户角色过滤菜单
const menuItems = computed(() => {
  if (userStore.isAdmin) {
    return allMenuItems
  }
  return allMenuItems.filter(item => !item.requiresAdmin)
})

function handleLogout() {
  userStore.logout()
  router.push('/login')
}
</script>

<template>
  <aside class="w-64 bg-white dark:bg-gray-800 shadow-lg flex flex-col">
    <!-- Logo -->
    <div class="h-16 flex items-center justify-center border-b dark:border-gray-700">
      <h1 class="text-xl font-bold text-gray-800 dark:text-white">AIRouter</h1>
    </div>

    <!-- 导航菜单 -->
    <nav class="flex-1 py-4">
      <ul class="space-y-1">
        <li v-for="item in menuItems" :key="item.path">
          <RouterLink
            :to="item.path"
            class="flex items-center px-6 py-3 text-gray-700 dark:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors"
            active-class="bg-blue-50 dark:bg-blue-900/20 text-blue-600 dark:text-blue-400 border-r-2 border-blue-600"
          >
            <span class="mr-3">{{ item.name }}</span>
          </RouterLink>
        </li>
      </ul>
    </nav>

    <!-- 用户信息 -->
    <div class="p-4 border-t dark:border-gray-700">
      <div class="flex items-center justify-between">
        <div>
          <p class="text-sm font-medium text-gray-800 dark:text-white">
            {{ userStore.user?.username || '管理员' }}
          </p>
          <p class="text-xs text-gray-500 dark:text-gray-400">
            {{ userStore.user?.role || 'admin' }}
          </p>
        </div>
        <button
          @click="handleLogout"
          class="text-sm text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
        >
          退出
        </button>
      </div>
    </div>
  </aside>
</template>