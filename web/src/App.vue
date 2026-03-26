<script setup lang="ts">
import { RouterView } from 'vue-router'
import Sidebar from '@/components/Sidebar.vue'
import { useUserStore } from '@/stores/user'
import { computed } from 'vue'
import { useRoute } from 'vue-router'

const userStore = useUserStore()
const route = useRoute()

const isLoginPage = computed(() => route.name === 'Login')
</script>

<template>
  <div class="flex h-screen bg-gray-100 dark:bg-gray-900">
    <!-- 侧边栏 -->
    <Sidebar v-if="!isLoginPage && userStore.isLoggedIn()" />

    <!-- 主内容区 -->
    <main class="flex-1 overflow-auto">
      <RouterView />
    </main>
  </div>
</template>