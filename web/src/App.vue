<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { session } from '@/composables/useSession'
import { api } from '@/api/client'
import { toast } from '@/ui/toast'
import Toast from '@/ui/Toast.vue'

const router = useRouter()
const authed = computed(() => session.authenticated)

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
        <span>云雀</span>
      </RouterLink>

      <nav v-if="authed">
        <RouterLink to="/">文件</RouterLink>
        <RouterLink to="/overview">概览</RouterLink>
        <RouterLink to="/shares">分享</RouterLink>
        <RouterLink to="/audit">审计</RouterLink>
        <RouterLink to="/settings">设置</RouterLink>
      </nav>

      <div class="right">
        <span v-if="authed && session.state.user" class="who">{{ session.state.user.username }}</span>
        <button v-if="authed" type="button" class="link" @click="logout">退出</button>
      </div>
    </header>

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
  display: flex;
  align-items: center;
  gap: var(--s-6);
  height: var(--header-h);
  padding: 0 var(--s-4);
  background: color-mix(in oklab, var(--bg) 88%, transparent);
  border-bottom: 1px solid var(--border);
  backdrop-filter: blur(12px);
}

.brand {
  display: flex;
  align-items: center;
  gap: var(--s-2);
  font-weight: 600;
  color: var(--text);
  text-decoration: none;
}

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

nav {
  display: flex;
  gap: var(--s-1);
  overflow-x: auto;
}

nav a {
  padding: var(--s-2) var(--s-3);
  border-radius: var(--r-sm);
  color: var(--text-2);
  font-size: var(--t-sm);
  text-decoration: none;
  white-space: nowrap;
}

nav a:hover {
  background: var(--surface-2);
  color: var(--text);
}

nav a.router-link-active {
  color: var(--text);
  font-weight: 500;
}

.right {
  display: flex;
  align-items: center;
  gap: var(--s-3);
  margin-left: auto;
}

.who {
  color: var(--text-3);
  font-size: var(--t-sm);
}

.link {
  color: var(--text-2);
  font-size: var(--t-sm);
}

.link:hover {
  color: var(--text);
}

main {
  flex: 1;
}

@media (max-width: 640px) {
  .topbar {
    gap: var(--s-2);
    padding: 0 var(--s-3);
  }

  /* 窄屏只留图标，把宽度让给导航。 */
  .brand span:last-child {
    display: none;
  }

  .who {
    display: none;
  }

  nav a {
    padding: var(--s-2);
    font-size: var(--t-xs);
  }
}
</style>
