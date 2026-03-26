import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { User } from '@/api/types'

export const useUserStore = defineStore('user', () => {
  const token = ref<string | null>(localStorage.getItem('token'))
  const user = ref<User | null>(null)

  // 初始化时从 localStorage 恢复用户信息
  const storedUser = localStorage.getItem('user')
  if (storedUser) {
    try {
      user.value = JSON.parse(storedUser)
    } catch {
      // 解析失败，忽略
    }
  }

  function setToken(newToken: string) {
    token.value = newToken
    localStorage.setItem('token', newToken)
  }

  function setUser(newUser: User) {
    user.value = newUser
    // 存储到 localStorage 以便路由守卫使用
    localStorage.setItem('user', JSON.stringify(newUser))
  }

  function logout() {
    token.value = null
    user.value = null
    localStorage.removeItem('token')
    localStorage.removeItem('user')
  }

  function isLoggedIn() {
    return !!token.value
  }

  // 是否为管理员
  const isAdmin = computed(() => user.value?.role === 'admin')

  // 检查是否有权限访问需要管理员权限的页面
  function hasPermission(requiredRole: string) {
    if (!user.value) return false
    if (requiredRole === 'admin') {
      return user.value.role === 'admin'
    }
    return true
  }

  return {
    token,
    user,
    setToken,
    setUser,
    logout,
    isLoggedIn,
    isAdmin,
    hasPermission
  }
})