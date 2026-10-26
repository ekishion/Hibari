<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { session } from '@/composables/useSession'
import { theme } from '@/composables/useTheme'
import { api } from '@/api/client'
import { toast } from '@/ui/toast'
import Toast from '@/ui/Toast.vue'
import { Menu, Moon, Sun, X } from 'lucide-vue-next'

const router = useRouter()
const route = useRoute()
const authed = computed(() => session.authenticated)

/** 导航条目。顶栏与抽屉共用一份，避免两处漏改。 */
const links = [
  { to: '/', label: '文件' },
  { to: '/overview', label: '概览' },
  { to: '/shares', label: '分享' },
  { to: '/audit', label: '审计' },
  { to: '/settings', label: '设置' },
]

/** 移动端抽屉。桌面端不出现，导航直接在顶栏横排。 */
const navOpen = ref(false)
const drawer = ref<HTMLDialogElement | null>(null)

// 原生 dialog 的 showModal/close 由状态驱动，Esc 关闭再经 close 事件同步回来。
watch(navOpen, (open) => {
  const el = drawer.value
  if (!el) return
  if (open && !el.open) el.showModal()
  if (!open && el.open) el.close()
})

// 点了任意链接就收起，否则抽屉会盖住刚打开的新页面。
watch(
  () => route.fullPath,
  () => {
    navOpen.value = false
  },
)

async function logout() {
  try {
    await api.logout()
  } catch {
    // 会话可能已在服务端过期，本地状态照常清理。
  }
  session.setUser(null)
  toast.info('已退出登录')
  router.push({ name: 'login' })
}
</script>

<template>
  <div class="shell">
    <header class="topbar">
      <RouterLink to="/" class="brand">
        <span class="mark" aria-hidden="true">雀</span>
        <span class="name">云雀</span>
      </RouterLink>

      <nav v-if="authed" class="bar-nav">
        <RouterLink v-for="l in links" :key="l.to" :to="l.to">{{ l.label }}</RouterLink>
      </nav>

      <div class="right" :class="{ authed }">
        <span v-if="authed && session.state.user" class="who">{{ session.state.user.username }}</span>
        <!-- 访客（分享页）没有抽屉，主题切换必须留在顶栏；登录后它和退出都搬进抽屉。 -->
        <button
          type="button"
          class="icon-btn theme-btn"
          :aria-label="theme.state.theme === 'dark' ? '切换到浅色主题' : '切换到深色主题'"
          :title="theme.state.theme === 'dark' ? '浅色' : '深色'"
          @click="theme.toggle()"
        >
          <Sun v-if="theme.state.theme === 'dark'" :size="16" />
          <Moon v-else :size="16" />
        </button>
        <button v-if="authed" type="button" class="link" @click="logout">退出</button>
        <button v-if="authed" type="button" class="icon-btn menu" aria-label="打开菜单" @click="navOpen = true">
          <Menu :size="18" />
        </button>
      </div>
    </header>

    <!-- 移动端抽屉：原生 dialog 提供焦点陷阱、Esc 关闭与遮罩，这里只负责左侧排版。 -->
    <dialog ref="drawer" class="drawer" aria-label="导航菜单" @close="navOpen = false" @cancel="navOpen = false">
      <div class="drawer-top">
        <span class="mark" aria-hidden="true">雀</span>
        <span class="drawer-name">云雀</span>
        <button type="button" class="icon-btn drawer-close" aria-label="关闭菜单" @click="navOpen = false">
          <X :size="18" />
        </button>
      </div>

      <nav class="drawer-nav">
        <RouterLink v-for="l in links" :key="l.to" :to="l.to">{{ l.label }}</RouterLink>
      </nav>

      <div class="drawer-foot">
        <span v-if="session.state.user" class="drawer-who">{{ session.state.user.username }}</span>
        <button type="button" class="drawer-btn" @click="theme.toggle()">
          <Sun v-if="theme.state.theme === 'dark'" :size="16" />
          <Moon v-else :size="16" />
          {{ theme.state.theme === 'dark' ? '浅色主题' : '深色主题' }}
        </button>
        <button type="button" class="drawer-btn" @click="logout">退出登录</button>
      </div>
    </dialog>

    <main>
      <RouterView />
    </main>

    <Toast />
  </div>
</template>

<style scoped>
.shell {
  display: flex;
  flex-direction: column;
  min-height: 100dvh;
}

.topbar {
  position: sticky;
  top: 0;
  z-index: 40;
  /* 三栏：品牌贴左、导航居中、账号贴右。两侧等宽的 1fr 让中间栏真正落在视口中线。 */
  display: grid;
  grid-template-columns: 1fr auto 1fr;
  align-items: center;
  gap: var(--s-4);
  height: var(--header-h);
  padding: 0 var(--s-4);
  /* 用 surface 而不是 bg：顶栏和页面底色同色时会糊成一片，
     与表格用同一层色阶才立得起来。 */
  background: color-mix(in oklab, var(--surface) 88%, transparent);
  border-bottom: 1px solid var(--border);
  backdrop-filter: blur(12px);
}

.brand {
  grid-column: 1;
  display: flex;
  align-items: center;
  gap: var(--s-2);
  font-weight: 600;
  color: var(--text);
  text-decoration: none;
}

/* 品牌块就是一块实心标牌，不再叠纹理——百叶窗的语言交给导航的叶片结构。 */
.mark {
  display: grid;
  place-items: center;
  width: 26px;
  height: 26px;
  background: var(--text);
  color: var(--bg);
  border-radius: var(--r-sm);
  font-size: var(--t-sm);
}

/* 桌面端导航：一片片独立的叶片，缝隙内缩不触上下边缘。 */
.bar-nav {
  grid-column: 2;
  display: flex;
  align-self: stretch;
  /* 空间不够时导航自己横向滚动，不挤压右侧的账号与退出。 */
  min-width: 0;
  overflow-x: auto;
  scrollbar-width: none;
}

.bar-nav::-webkit-scrollbar {
  display: none;
}

.bar-nav a {
  position: relative;
  display: flex;
  align-items: center;
  padding: 0 var(--s-4);
  color: var(--text-2);
  font-size: var(--t-sm);
  text-decoration: none;
  white-space: nowrap;
  transition: color var(--dur-fast) var(--ease);
}

/* 叶片之间留内缩的缝：细线不触上下边缘，才是一片片独立的叶片。 */
.bar-nav a + a::before {
  content: '';
  position: absolute;
  inset: 32% auto 32% 0;
  width: 1px;
  background: var(--border);
}

.bar-nav a:hover {
  color: var(--text);
}

/* 当前页：文字最亮、字重加粗，不再画竖线——顶栏里那道蓝边像多余的描边。 */
.bar-nav a.router-link-active {
  color: var(--text);
  font-weight: 500;
}

/* 汉堡按钮只在移动端出现，桌面端导航本来就横在顶栏里。
   选择器带上 .icon-btn 才压得住下面同优先级的 display: grid。 */
.icon-btn.menu {
  display: none;
}

.right {
  grid-column: 3;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: var(--s-3);
}

.who {
  color: var(--text-3);
  font-size: var(--t-sm);
}

.link {
  color: var(--text-2);
  font-size: var(--t-sm);
  white-space: nowrap;
}

.link:hover {
  color: var(--text);
}

.icon-btn {
  display: grid;
  place-items: center;
  width: 32px;
  height: 32px;
  border-radius: var(--r-sm);
  color: var(--text-2);
}

.icon-btn:hover {
  background: var(--surface-2);
  color: var(--text);
}

main {
  flex: 1;
}

/* ---- 移动端抽屉 ---- */

.drawer {
  /* 覆盖原生 dialog 的居中与尺寸：贴左侧、通高、只有右侧圆角。 */
  inset: 0 auto 0 0;
  margin: 0;
  width: min(300px, 82vw);
  max-width: none;
  height: 100dvh;
  max-height: none;
  padding: 0;
  border: none;
  border-right: 1px solid var(--border);
  border-radius: 0 var(--r-lg) var(--r-lg) 0;
  background: var(--surface);
  color: var(--text);
  overflow: hidden;
}

.drawer::backdrop {
  background: light-dark(rgb(0 0 0 / 0.4), rgb(0 0 0 / 0.7));
  backdrop-filter: blur(2px);
}

/* 从左侧滑入。opacity 一并走，避免遮罩先于面板出现。 */
.drawer[open] {
  display: flex;
  flex-direction: column;
  animation: slide-in var(--dur) var(--ease);
}

@keyframes slide-in {
  from {
    opacity: 0;
    transform: translateX(-100%);
  }
}

.drawer-top {
  display: flex;
  align-items: center;
  gap: var(--s-2);
  padding: var(--s-3) var(--s-4);
  border-bottom: 1px solid var(--border);
}

.drawer-name {
  font-weight: 600;
}

.drawer-close {
  margin-left: auto;
}

/* 抽屉里的菜单项：每项一片，项与项之间是一条通栏细线。 */
.drawer-nav {
  display: flex;
  flex-direction: column;
  padding: var(--s-2) 0;
  overflow-y: auto;
}

.drawer-nav a {
  position: relative;
  display: flex;
  align-items: center;
  min-height: 48px;
  padding: 0 var(--s-5);
  color: var(--text-2);
  font-size: var(--t-sm);
  text-decoration: none;
}

.drawer-nav a + a {
  border-top: 1px solid var(--border);
}

.drawer-nav a:hover {
  background: var(--surface-2);
  color: var(--text);
}

/* 当前页用左侧强调线与加亮文字标出，不做整行填色——与顶栏语言一致。 */
.drawer-nav a.router-link-active {
  color: var(--text);
  font-weight: 500;
}

.drawer-nav a.router-link-active::before {
  content: '';
  position: absolute;
  inset: 0 auto 0 0;
  width: 3px;
  background: var(--accent);
}

.drawer-foot {
  display: flex;
  flex-direction: column;
  margin-top: auto;
  border-top: 1px solid var(--border);
}

.drawer-who {
  padding: var(--s-3) var(--s-5) 0;
  color: var(--text-3);
  font-size: var(--t-xs);
}

.drawer-btn {
  display: flex;
  align-items: center;
  gap: var(--s-2);
  min-height: 48px;
  padding: 0 var(--s-5);
  color: var(--text-2);
  font-size: var(--t-sm);
  text-align: left;
}

.drawer-btn + .drawer-btn {
  border-top: 1px solid var(--border);
}

.drawer-btn:hover {
  background: var(--surface-2);
  color: var(--text);
}

@media (max-width: 640px) {
  .topbar {
    gap: var(--s-2);
    padding: 0 var(--s-3);
  }

  /* 移动端导航搬进抽屉，顶栏只留品牌和右侧的汉堡。 */
  .bar-nav {
    display: none;
  }

  .icon-btn.menu {
    display: grid;
  }

  /* 已登录时主题切换与退出都在抽屉里，顶栏不再重复；
     访客没有抽屉，主题切换照常留在顶栏。 */
  .right.authed .theme-btn,
  .right.authed .link {
    display: none;
  }

  /* 窄屏只留图标。 */
  .name {
    display: none;
  }

  .who {
    display: none;
  }

  .icon-btn {
    width: 44px;
    height: 44px;
  }
}

/* 桌面端不需要抽屉；万一在打开状态下被拉宽，也不让它留在屏幕上。 */
@media (min-width: 641px) {
  .drawer[open] {
    display: none;
  }
}
</style>
