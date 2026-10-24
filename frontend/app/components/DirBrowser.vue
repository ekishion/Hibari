<script setup lang="ts">
// 目录浏览主视图：全盘浏览 / 目录短链沙盒两态
const props = defineProps<{ dirToken: string }>()
const { authRequired, authenticated, requireLogin, checkAuth, uiConfig } = useAuth()
const { currentDir, sandboxRoot, currentFiles, loading, loadFiles, enterDir, currentDirToken, currentDirPassword } =
  useFiles()

const route = useRoute()
const { currentFiles: fileListRef, searchQuery: currentSearch, currentDir: currentDirRef } = useFiles()
const reloadFiles = () => loadFiles(currentDirRef.value || "/", { refresh: true })
provide("reloadFiles", reloadFiles)

// 全局拖拽：拖文件到列表区直接打开上传弹窗并预填（仅已登录 + upload_enabled 时生效）
const { addFiles } = useUpload()
const dragDepth = ref(0)
const dropTargetActive = ref(false)
const draggingWithFiles = (e: DragEvent) => !!e.dataTransfer && Array.from(e.dataTransfer.types).includes("Files")
const canUploadNow = computed(() => uiConfig.uploadEnabled && !sandboxRoot.value && !(authRequired.value && !authenticated.value))

function onDocDragEnter(e: DragEvent) {
  if (!canUploadNow.value || !draggingWithFiles(e)) return
  dragDepth.value++
  dropTargetActive.value = true
}
function onDocDragOver(e: DragEvent) {
  if (!canUploadNow.value || !draggingWithFiles(e)) return
  e.preventDefault()
}
function onDocDragLeave(e: DragEvent) {
  if (!canUploadNow.value || !draggingWithFiles(e)) return
  dragDepth.value = Math.max(0, dragDepth.value - 1)
  if (dragDepth.value === 0) dropTargetActive.value = false
}
function onDocDrop(e: DragEvent) {
  dragDepth.value = 0
  dropTargetActive.value = false
  if (!canUploadNow.value || !draggingWithFiles(e)) return
  e.preventDefault()
  const files = e.dataTransfer?.files
  if (!files?.length) return
  // 弹窗已打开且事件源自弹窗内部时，交给弹窗拖放区处理，避免重复清空清单
  // （弹窗经 Teleport 挂在 body 下，用 closest 而非 ref.contains 判定）
  if (uploadOpen.value && (e.target as HTMLElement).closest?.("#upload-overlay")) return
  openUpload()
  addFiles(files)
}

onMounted(() => {
  document.addEventListener("dragenter", onDocDragEnter)
  document.addEventListener("dragover", onDocDragOver)
  document.addEventListener("dragleave", onDocDragLeave)
  document.addEventListener("drop", onDocDrop)
})
onBeforeUnmount(() => {
  document.removeEventListener("dragenter", onDocDragEnter)
  document.removeEventListener("dragover", onDocDragOver)
  document.removeEventListener("dragleave", onDocDragLeave)
  document.removeEventListener("drop", onDocDrop)
})

// 上传组件按配置动态加载：未启用时 chunk 不下载、不执行
const uploadComponent = shallowRef<Component | null>(null)
watch(
  () => uiConfig.uploadEnabled,
  (enabled) => {
    if (enabled && !uploadComponent.value) {
      uploadComponent.value = defineAsyncComponent(() => import('./UploadModal.vue'))
    }
  },
  { immediate: true },
)
const { uploadOpen, openUpload, closeUpload } = useUploadModal()
const uploadShown = computed(() => !!uploadComponent.value && uploadOpen.value)

onMounted(async () => {
  try {
    if (props.dirToken) {
      currentDirToken.value = props.dirToken
      const pwd = (route.query.pwd as string) || ""
      if (pwd) currentDirPassword.value = pwd
      await loadFiles("")
    } else {
      // 已持有会话时 checkAuth 会置位 authenticated，加载统一交给下方 watch，避免双重请求
      await checkAuth()
    }
  } catch (err) {
    requireLogin((err as Error).message || "无法确认登录状态，请刷新页面重试")
  }
})

// 登录成功（或启动时已持有会话）后加载文件列表；保留 ?dir= 深链，重新登录则停留在当前目录
watch(authenticated, (now) => {
  if (!now || props.dirToken) return
  loadFiles(currentDirRef.value || (route.query.dir as string) || "/")
})
</script>

<template>
  <div class="container" id="app-main">
    <AppHeader :sandbox-root="sandboxRoot" />

    <Breadcrumbs :current-dir="currentDir" :sandbox-root="sandboxRoot" @navigate="enterDir" />

    <AppToolbar @open-upload="openUpload" />

    <ReadmePanel :files="currentFiles" />

    <FileList :class="{ 'is-drop-target': dropTargetActive }" />

    <div id="status" :style="{ display: loading ? 'block' : 'none' }">
      <div class="spinner"><AppIcon name="loader" /></div>
      <div class="status-tip">正在拉取文件列表…</div>
    </div>

    <footer class="footer">
      <a href="https://github.com/malaohu/jikeqingpan" target="_blank" rel="noopener noreferrer">
        <span class="footer-icon"><AppIcon name="github" :size="15" /></span>GitHub
      </a>
      <a href="https://github.com/ekishion/jikeqingpan" target="_blank" rel="noopener noreferrer">
        <span class="footer-icon"><AppIcon name="github" :size="15" /></span>GitHub(二改)
      </a>
      <a href="https://n.eki.ink" target="_blank" rel="noopener noreferrer">
        <span class="footer-icon"><AppIcon name="sparkles" :size="15" /></span>梦澪の茶话会
      </a>
    </footer>

    <UnlockModal />
    <component :is="uploadComponent" v-if="uploadShown" :dir="currentDir || '/'" @close="closeUpload" />
  </div>
</template>
