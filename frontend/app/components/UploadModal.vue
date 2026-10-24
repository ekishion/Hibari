<script setup lang="ts">
// 上传弹窗：等价迁移自 upload_modal.html + upload.js
// （剩余容量条、拖拽区、文件清单、单文件进度、顺序上传、关闭取消）

const props = defineProps<{ dir: string }>()
const emit = defineEmits<{ close: [] }>()

const { state, pendingCount, quotaFree, addFiles, removeItem, resetQueue, startUpload, abortCurrent } = useUpload()
const { uiConfig } = useAuth()
const { showToast } = useToast()
const { currentFiles } = useFiles()
const reloadFiles = inject<() => void>("reloadFiles", () => {})

const visible = ref(true)
const quotaText = ref("正在获取网盘容量…")
const quotaPercent = ref(0)
const quotaWarn = ref(false)
const dropzoneActive = ref(false)
const fileInput = ref<HTMLInputElement | null>(null)
const listBox = ref<HTMLElement | null>(null)

const summaryText = computed(() => {
  const done = state.items.filter((it) => it.status === "done").length
  const failed = state.items.filter((it) => it.status === "failed").length
  const bits: string[] = []
  if (pendingCount.value) bits.push(`待上传 ${pendingCount.value}`)
  if (done) bits.push(`已完成 ${done}`)
  if (failed) bits.push(`失败 ${failed}`)
  return bits.join(" · ")
})

const canStart = computed(() => !state.uploading && pendingCount.value > 0)

function statusText(item: { status: string; message: string; progress: number }): string {
  if (item.status === "uploading") return "上传中 " + Math.round(item.progress) + "%"
  if (item.status === "saving") return "保存中…"
  if (item.status === "done") return "已完成"
  if (item.status === "failed") return item.message
  return "排队中"
}

async function loadQuota() {
  try {
    const resp = await apiFetch("/api/quota", { method: "GET" })
    if (!resp.ok) {
      const text = await resp.text()
      throw new Error(await parseAPIError(resp, text))
    }
    const data = await resp.json()
    const total = Number(data.total) || 0
    const used = Number(data.used) || 0
    const free = Number(data.free) || 0
    if (total <= 0) {
      quotaText.value = "容量信息不可用"
      return
    }
    const percent = Math.min(100, Math.round((used / total) * 100))
    quotaText.value = `已用 ${formatSize(used)} / 共 ${formatSize(total)} · 剩余 ${formatSize(free)}`
    quotaPercent.value = percent
    quotaWarn.value = percent >= 90
    quotaFree.value = free
  } catch (err) {
    quotaText.value = "容量获取失败：" + ((err as Error).message || "未知错误")
  }
}

function pickFiles() {
  fileInput.value?.click()
}

function onFileChosen(e: Event) {
  const input = e.target as HTMLInputElement
  if (input.files?.length) addFiles(input.files)
  input.value = ""
}

function onDropFiles(e: DragEvent) {
  dropzoneActive.value = false
  if (e.dataTransfer?.files?.length) addFiles(e.dataTransfer.files)
}

function closeModal() {
  if (state.uploading) {
    abortCurrent()
    showToast("已取消上传", "info")
  }
  visible.value = false
  emit("close")
}

async function beginUpload() {
  await startUpload(props.dir, (succeeded) => {
    if (succeeded > 0) {
      loadQuota()
      reloadFiles()
    }
  })
}

onMounted(() => {
  loadQuota()
})

onBeforeUnmount(() => {
  if (state.uploading) abortCurrent()
})
</script>

<template>
  <Teleport to="body">
    <div id="upload-overlay" class="modal-overlay" :hidden="!visible" :aria-hidden="!visible" @click.self="closeModal">
      <div class="upload-card" role="dialog" aria-modal="true" aria-labelledby="upload-dialog-title">
        <div class="upload-card-header">
          <div class="upload-card-title-group">
            <span class="upload-mark"><AppIcon name="upload" :size="17" /></span>
            <div>
              <h2 id="upload-dialog-title">上传文件</h2>
              <p class="upload-target" :title="dir">目标目录：{{ dir }}</p>
            </div>
          </div>
          <button type="button" id="upload-close" class="modal-close" aria-label="关闭上传窗口" @click="closeModal">
            <AppIcon name="x" :size="16" />
          </button>
        </div>

        <div id="upload-quota" class="upload-quota" aria-live="polite">
          <div class="upload-quota-text">{{ quotaText }}</div>
          <div class="upload-quota-bar" aria-hidden="true">
            <div class="upload-quota-fill" :class="{ 'is-warning': quotaWarn }" :style="{ width: quotaPercent + '%' }" />
          </div>
        </div>

        <div
          id="upload-dropzone"
          class="upload-dropzone"
          :class="{ 'is-dragover': dropzoneActive }"
          tabindex="0"
          role="button"
          aria-label="选择要上传的文件，或把文件拖到这里"
          @click="pickFiles"
          @keydown.enter.prevent="pickFiles"
          @keydown.space.prevent="pickFiles"
          @dragenter.prevent="dropzoneActive = true"
          @dragover.prevent="dropzoneActive = true"
          @dragleave="dropzoneActive = false"
          @drop.prevent="onDropFiles"
        >
          <span class="upload-dropzone-icon"><AppIcon name="upload" :size="18" /></span>
          <p class="upload-dropzone-title">点击选择文件，或把文件拖到这里</p>
          <p class="upload-dropzone-hint">
            {{ uiConfig.uploadMaxMB > 0 ? `单文件上限 ${uiConfig.uploadMaxMB >= 1024 ? uiConfig.uploadMaxMB / 1024 + ' GB' : uiConfig.uploadMaxMB + ' MB'}` : "不限单文件大小" }}
          </p>
          <input ref="fileInput" type="file" multiple hidden @change="onFileChosen">
        </div>

        <ul v-show="state.items.length" id="upload-file-list" ref="listBox" class="upload-file-list" aria-label="待上传文件清单">
          <li
            v-for="item in state.items"
            :key="item.id"
            class="upload-item"
            :class="{ 'is-done': item.status === 'done', 'is-failed': item.status === 'failed' }"
          >
            <span class="upload-item-icon"><AppIcon :name="fileIconMeta(item.name, false).name" :size="17" /></span>
            <div class="upload-item-main">
              <div class="upload-item-row">
                <span class="upload-item-name" :title="item.name">{{ item.name }}</span>
                <span class="upload-item-size">{{ formatSize(item.size) }}</span>
              </div>
              <div class="upload-progress" aria-hidden="true">
                <div class="upload-progress-fill" :class="{ 'is-failed': item.status === 'failed' }" :style="{ width: item.progress + '%' }" />
              </div>
              <div class="upload-item-status">{{ statusText(item) }}</div>
            </div>
            <button
              type="button"
              class="upload-item-remove"
              :aria-label="'移除 ' + item.name"
              :disabled="item.status === 'uploading' || item.status === 'saving'"
              @click="removeItem(item)"
            >
              <AppIcon name="trash-2" :size="15" />
            </button>
          </li>
        </ul>

        <div class="upload-actions">
          <span class="upload-actions-hint" id="upload-summary">{{ summaryText }}</span>
          <button type="button" class="btn btn-primary" :disabled="!canStart" @click="beginUpload">
            <span class="btn-spinner"><AppIcon name="loader" :size="16" /></span>
            <span class="btn-label">{{ state.uploading ? "上传中…" : "开始上传" }}</span>
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>
