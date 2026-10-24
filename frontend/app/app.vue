<script setup lang="ts">
// 应用壳：全局样式 + 单页路由 + 全局弹窗（主题在 AppHeader 内管理）
import '~/assets/css/app.css'
</script>

<template>
  <div>
    <NuxtPage />
    <ToastHost />
    <LoginModal />
    <ShareModal />
    <SharesManageModal />
    <Lightbox />
  </div>
</template>
