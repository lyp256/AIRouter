import { createRouter, createWebHistory } from 'vue-router'
import type { RouteRecordRaw } from 'vue-router'

const routes: RouteRecordRaw[] = [
  {
    path: '/',
    name: 'Dashboard',
    component: () => import('@/views/Dashboard.vue'),
    meta: { title: '仪表盘' }
  },
  {
    path: '/chat',
    name: 'Chat',
    component: () => import('@/views/Chat.vue'),
    meta: { title: 'AI 聊天' }
  },
  {
    path: '/providers',
    name: 'Providers',
    component: () => import('@/views/Providers.vue'),
    meta: { title: '供应商管理', requiresAdmin: true }
  },
  {
    path: '/models',
    name: 'Models',
    component: () => import('@/views/Models.vue'),
    meta: { title: '模型管理', requiresAdmin: true }
  },
  {
    path: '/users',
    name: 'Users',
    component: () => import('@/views/Users.vue'),
    meta: { title: '用户管理', requiresAdmin: true }
  },
  {
    path: '/keys',
    name: 'Keys',
    component: () => import('@/views/Keys.vue'),
    meta: { title: '密钥管理' }
  },
  {
    path: '/statistics',
    name: 'Statistics',
    component: () => import('@/views/Statistics.vue'),
    meta: { title: '统计分析', requiresAdmin: true }
  },
  {
    path: '/settings',
    name: 'Settings',
    component: () => import('@/views/Settings.vue'),
    meta: { title: '系统设置' }
  },
  {
    path: '/login',
    name: 'Login',
    component: () => import('@/views/Login.vue'),
    meta: { title: '登录', requiresAuth: false }
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

// 路由守卫
router.beforeEach((to, _from, next) => {
  const token = localStorage.getItem('token')
  const userStr = localStorage.getItem('user')

  // 需要认证的页面
  if (to.meta.requiresAuth !== false && !token) {
    next({ name: 'Login', query: { redirect: to.fullPath } })
    return
  }

  // 已登录用户访问登录页
  if (to.name === 'Login' && token) {
    next({ name: 'Dashboard' })
    return
  }

  // 检查管理员权限
  if (to.meta.requiresAdmin) {
    let role = 'user'
    if (userStr) {
      try {
        const user = JSON.parse(userStr)
        role = user.role || 'user'
      } catch {
        // 解析失败，使用默认值
      }
    }
    if (role !== 'admin') {
      next({ name: 'Dashboard' })
      return
    }
  }

  next()
})

export default router