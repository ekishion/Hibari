<script setup lang="ts">
// 工具栏：文件计数 + 搜索 + 排序 + 上传（门控）+ 分享 + 刷新
const { visibleFiles, searchQuery, currentDir, loadFiles, resetFileSearch } = useFiles()
const { uiConfig, authRequired, authenticated, requireLogin } = useAuth()
const { openShare } = useShareModals()

const emit = defineEmits<{ openUpload: [] }>()

const searchInput = ref<HTMLInputElement | null>(null)

// "/" 快捷键聚焦搜索
function onKeydown(e: KeyboardEvent) {
  if (e.key === "/" && document.activeElement?.tagName !== "INPUT" && document.activeElement?.tagName !== "TEXTAREA") {
    e.preventDefault()
    searchInput.value?.focus()
  }
}

onMounted(() => document.addEventListener("keydown", onKeydown))
onBeforeUnmount(() => document.removeEventListener("keydown", onKeydown))

function onRefresh() {
  if (authRequired.value && !authenticated.value) {
    requireLogin("")
    return
  }
  loadFiles(currentDir.value || "/", { refresh: true })
}

function onShareCurrentDir() {
  const dir = currentDir.value || "/"
  openShare({ type: "dir", path: dir, name: dir.split("/").pop() || "根目录" })
}
</script>

<template>
  <div class="toolbar">
    <span class="file-count" id="file-count">
      共 {{ visibleFiles.length }} 项{{ searchQuery ? "（已筛选）" : "" }}
    </span>
    <div class="toolbar-actions">
      <label class="search-box" for="file-search">
        <span><AppIcon name="search" :size="15" /></span>
        <input
          id="file-search"
          ref="searchInput"
          v-model="searchQuery"
          type="search"
          autocomplete="off"
          placeholder="筛选当前目录"
          aria-label="筛选当前目录文件"
          @keydown.escape="resetFileSearch(); (document.activeElement as HTMLElement)?.blur()"
        >
      </label>
      <SortMenu />
      <button
        v-if="uiConfig.uploadEnabled"
        type="button"
        class="btn btn-ghost btn-upload"
        aria-label="上传文件到当前目录"
        @click="emit('openUpload')"
      >
        <span class="btn-icon"><AppIcon name="upload" :size="15" /></span>
        <span class="btn-label">上传</span>
      </button>
      <button type="button" class="btn btn-ghost btn-share" aria-label="分享当前目录" @click="onShareCurrentDir">
        <span class="btn-icon"><AppIcon name="share-2" :size="15" /></span>
        <span class="btn-label">分享</span>
      </button>
      <button type="button" class="btn btn-ghost btn-refresh" aria-label="刷新当前目录" @click="onRefresh">
        <span class="btn-icon"><AppIcon name="refresh-cw" :size="15" /></span>
        <span class="btn-label">刷新</span>
      </button>
    </div>
  </div>
</template>
