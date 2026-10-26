import { createRouter, createWebHistory } from 'vue-router'
import { session } from '@/composables/useSession'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'browse', component: () => import('@/features/browse/BrowsePage.vue') },
    { path: '/s/:token', name: 'share', component: () => import('@/features/share/SharePage.vue') },
    { path: '/login', name: 'login', component: () => import('@/features/auth/LoginPage.vue') },
    { path: '/setup', name: 'setup', component: () => import('@/features/auth/SetupPage.vue') },
    {
      path: '/overview',
      name: 'overview',
      component: () => import('@/features/admin/OverviewPage.vue'),
      meta: { requiresAuth: true },
    },
    {
      path: '/shares',
      name: 'shares',
      component: () => import('@/features/share/SharesPage.vue'),
      meta: { requiresAuth: true },
    },
    {
      path: '/audit',
      name: 'audit',
      component: () => import('@/features/admin/AuditPage.vue'),
      meta: { requiresAuth: true },
    },
    {
      path: '/settings',
      name: 'settings',
      component: () => import('@/features/admin/SettingsPage.vue'),
      meta: { requiresAuth: true },
    },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
})

// 首次导航前拉一次状态，避免登录页闪一下。
router.beforeEach(async (to) => {
  // 分享页对访客开放，不参与初始化与登录判断。
  if (to.name === 'share') return true

  if (!session.state.loaded) {
    try {
      await session.refresh()
    } catch {
      // 后端不可达时放行到页面，由页面自己报错。
      return true
    }
  }

  if (session.state.needsSetup && to.name !== 'setup') {
    return { name: 'setup' }
  }
  if (!session.state.needsSetup && to.name === 'setup') {
    return { name: session.authenticated ? 'browse' : 'login' }
  }
  if (to.meta.requiresAuth && !session.authenticated) {
    return { name: 'login', query: { next: to.fullPath } }
  }
  if (to.name === 'login' && session.authenticated) {
    return { name: 'browse' }
  }
  return true
})

export default router
