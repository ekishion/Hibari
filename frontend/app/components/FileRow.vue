<script setup lang="ts">
// 文件列表行：等价迁移自 buildFileItem / buildParentItem / downloadFileDirect
import { fileNameOf, fileTimeOf, type BaiduFile } from '~/composables/useFiles'

const props = defineProps<{
  // 返回上一级行不携带 file（以 parentPath 区分），仅在非父行时存在
  file?: BaiduFile
  currentDir: string
  parentPath?: string | null
}>()

const { enterDir, currentDirToken, currentDirPassword } = useFiles()
const { openPreview } = usePreview()
const { openShare } = useShareModals()
const { showToast } = useToast()

// 目录短链沙盒模式：访客不该看到"创建分享"入口
const inSandbox = computed(() => !!currentDirToken.value)

const isDir = computed(() => Number(props.file?.isdir) === 1)
const name = computed(() => (props.parentPath ? "返回上级" : fileNameOf(props.file)))
const metaText = computed(() => {
  if (props.parentPath) return "返回上一层目录"
  const timeStr = formatTime(fileTimeOf(props.file))
  if (isDir.value) return "文件夹" + (timeStr !== "-" ? " · " + timeStr : "")
  return formatSize(Number(props.file.size) || 0) + (timeStr !== "-" ? " · " + timeStr : "")
})
const filePath = computed(() => {
  if (props.parentPath) return props.parentPath
  return props.file.path || (props.currentDir.replace(/\/+$/, "") + "/" + name.value)
})
const iconMeta = computed(() =>
  props.parentPath ? { name: "corner-left-up", category: "folder" } : fileIconMeta(name.value, isDir.value),
)
const previewKindValue = computed(() => (props.parentPath || isDir.value ? null : previewKind(name.value)))
const downloading = ref(false)

async function downloadFileDirect() {
  if (downloading.value) return
  downloading.value = true
  // 沙盒模式凭目录短链 token + 提取码换流媒体访问
  const streamUrl =
    "/api/stream?path=" + encodeURIComponent(filePath.value) +
    (currentDirToken.value
      ? "&token=" + encodeURIComponent(currentDirToken.value) + "&pwd=" + encodeURIComponent(currentDirPassword.value)
      : "")
  try {
    const resp = await apiFetch(streamUrl, { headers: { "X-Stream-Probe": "1" } })
    if (!resp.ok) {
      const text = await resp.text()
      throw new Error(await parseAPIError(resp, text))
    }
    await resp.json()
    triggerDownloadNavigation(streamUrl)
  } catch (err) {
    showToast("下载失败：" + ((err as Error).message || "未知错误"), "error")
  } finally {
    downloading.value = false
  }
}

function onRowClick() {
  if (props.parentPath) enterDir(props.parentPath)
  else if (isDir.value) enterDir(filePath.value)
}

function onRowKeydown(e: KeyboardEvent) {
  if (e.key === "Enter" || e.key === " ") {
    e.preventDefault()
    onRowClick()
  }
}

function onRowDblclick(e: MouseEvent) {
  if (isDir.value || props.parentPath) return
  if ((e.target as HTMLElement).closest?.(".file-action")) return
  downloadFileDirect()
}
</script>

<template>
  <div
    class="file-item"
    :class="{ 'is-dir': isDir || !!parentPath }"
    role="button"
    tabindex="0"
    :data-filename="name"
    @click="onRowClick"
    @keydown="onRowKeydown"
    @dblclick="onRowDblclick"
  >
    <div class="file-icon" :data-cat="iconMeta.category">
      <AppIcon :name="iconMeta.name" :size="20" />
    </div>
    <div class="file-info">
      <div class="file-name" :title="name">{{ name }}</div>
      <div class="file-meta">{{ metaText }}</div>
    </div>
    <div class="file-actions">
      <template v-if="!parentPath && isDir && !inSandbox">
        <button type="button" class="file-action" :aria-label="'分享文件夹 ' + name" @click.stop="openShare({ type: 'dir', path: filePath, name })">
          <AppIcon name="share-2" :size="14" />
          <span>分享</span>
        </button>
      </template>
      <template v-else-if="!parentPath && !isDir">
        <button v-if="previewKindValue" type="button" class="file-action" :aria-label="'预览 ' + name" @click.stop="openPreview(filePath, name)">
          <AppIcon name="eye" :size="14" />
          <span>预览</span>
        </button>
        <button v-if="!inSandbox" type="button" class="file-action" :aria-label="'分享 ' + name" @click.stop="openShare({ type: 'file', path: filePath, name })">
          <AppIcon name="share-2" :size="14" />
          <span>分享</span>
        </button>
        <button
          type="button"
          class="file-action"
          :aria-label="'下载 ' + name"
          :disabled="downloading"
          @click.stop.prevent="downloadFileDirect"
          @dblclick.stop.prevent
        >
          <AppIcon name="download" :size="14" />
          <span>下载</span>
        </button>
      </template>
    </div>
  </div>
</template>
