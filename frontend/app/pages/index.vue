<script setup lang="ts">
// 主入口：按 URL 参数分发三种模式（与原 app.js 路由决策等价）
// 1. 单文件分享落地页 (?s={token})  2. 目录短链沙盒 (?d={token})  3. 全盘浏览
const route = useRoute()
const shareToken = computed(() => (route.query.s as string) || "")
const dirToken = computed(() => (route.query.d as string) || "")
</script>

<template>
  <main>
    <ShareLanding v-if="shareToken" :token="shareToken" />
    <DirBrowser v-else :dir-token="dirToken" />
  </main>
</template>
