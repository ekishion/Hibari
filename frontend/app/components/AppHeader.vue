<script setup lang="ts">
// 顶栏：品牌 + 主题切换 + 分享管理 + 登录/退出
const { authRequired, authenticated, doLogout } = useAuth()
const { isDark, toggle } = useTheme()
const { showToast } = useToast()
const { openSharesManage, sharesManageOpen } = useShareModals()

const props = defineProps<{ sandboxRoot?: string }>()

const showShareManage = computed(() => !props.sandboxRoot && !(authRequired.value && !authenticated.value))

async function onLogout() {
  await doLogout()
  showToast("已退出登录", "info")
}
</script>

<template>
  <header class="header">
    <div class="brand">
      <span class="brand-mark"><AppIcon name="cloud" /></span>
      <div class="brand-text">
        <h1 class="wordmark">即刻轻盘</h1>
        <p class="subtitle">百度网盘青春版 · 安全中转</p>
      </div>
    </div>
    <div class="header-actions">
      <button type="button" class="btn btn-ghost" :aria-label="isDark ? '切换到浅色模式' : '切换到深色模式'" @click="toggle()">
        <span class="btn-icon"><AppIcon :name="isDark ? 'sun' : 'moon'" /></span>
      </button>
      <button v-if="showShareManage" type="button" class="btn btn-ghost" aria-label="活跃分享管理" @click="openSharesManage">
        <span class="btn-icon"><AppIcon name="share-2" /></span>
        <span class="btn-label">分享管理</span>
      </button>
      <button v-if="authRequired && !authenticated" type="button" class="btn btn-ghost" aria-label="登录" @click="useAuth().requireLogin('')">
        <span class="btn-icon"><AppIcon name="log-in" /></span>
        <span class="btn-label">登录</span>
      </button>
      <button v-else-if="authRequired && authenticated" type="button" class="btn btn-ghost" aria-label="退出" @click="onLogout">
        <span class="btn-icon"><AppIcon name="log-out" /></span>
        <span class="btn-label">退出</span>
      </button>
    </div>
  </header>
</template>
