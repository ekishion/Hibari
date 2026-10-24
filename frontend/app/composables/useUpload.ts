// 上传队列状态：等价迁移自原 upload.js
// （客户端预检、顺序 XHR 上传 + 真实进度、CSRF 过期重试、取消）

export type UploadItemStatus = "pending" | "uploading" | "saving" | "done" | "failed"

export interface UploadItem {
  id: number
  file: File
  name: string
  size: number
  status: UploadItemStatus
  message: string
  progress: number
  xhr: XMLHttpRequest | null
}

interface UploadState {
  items: UploadItem[]
  uploading: boolean
  succeeded: number
  failed: number
}

const state = reactive<UploadState>({
  items: [],
  uploading: false,
  succeeded: 0,
  failed: 0,
})

let nextId = 1
let currentXHR: XMLHttpRequest | null = null

export function useUpload() {
  const { uiConfig } = useAuth()
  const { showToast } = useToast()

  const pendingCount = computed(() => state.items.filter((it) => it.status === "pending").length)

  function maxUploadBytes(): number {
    const mb = typeof uiConfig.uploadMaxMB === "number" ? uiConfig.uploadMaxMB : 0
    return mb > 0 ? mb * 1024 * 1024 : 0
  }

  function quotaFreeBytes(): number {
    return Number((typeof document !== "undefined" ? (document.getElementById("upload-quota") as HTMLElement | null)?.dataset.free : "0") || "0") || 0
  }

  function addFiles(fileList: FileList | File[] | null) {
    if (!fileList || !fileList.length) return
    const cap = maxUploadBytes()
    const freeBytes = quotaFreeBytes()
    for (const file of Array.from(fileList)) {
      let status: UploadItemStatus = "pending"
      let message = ""
      if (file.size <= 0) {
        status = "failed"
        message = "不支持空文件"
      } else if (cap > 0 && file.size > cap) {
        status = "failed"
        message = "超出单文件上限"
      } else if (freeBytes > 0 && file.size > freeBytes) {
        status = "failed"
        message = "超出网盘剩余容量"
      }
      state.items.push({
        id: nextId++,
        file,
        name: file.name,
        size: file.size,
        status,
        message,
        progress: 0,
        xhr: null,
      })
    }
  }

  function removeItem(item: UploadItem) {
    if (item.status === "uploading" || item.status === "saving") return
    state.items = state.items.filter((it) => it.id !== item.id)
  }

  function clearFinished() {
    state.items = state.items.filter((it) => it.status === "uploading" || it.status === "saving" || it.status === "pending")
  }

  function resetQueue() {
    if (state.uploading && currentXHR) {
      currentXHR.abort()
      showToast("已取消上传", "info")
    }
    state.uploading = false
    currentXHR = null
    state.items = []
    state.succeeded = 0
    state.failed = 0
  }

  function uploadOne(item: UploadItem, dir: string): Promise<void> {
    return new Promise((resolve) => {
      item.status = "uploading"
      item.progress = 0
      const form = new FormData()
      form.append("dir", dir)
      form.append("size", String(item.file.size))
      form.append("file", item.file, item.file.name)

      const xhr = new XMLHttpRequest()
      item.xhr = xhr
      currentXHR = xhr
      let retried = false

      const finish = () => {
        item.xhr = null
        currentXHR = null
        resolve()
      }

      const send = () => {
        xhr.open("POST", "/api/upload")
        xhr.setRequestHeader("X-CSRF-Token", getCookie("csrf_token"))
        xhr.send(form)
      }

      xhr.upload.onprogress = (e) => {
        if (!e.lengthComputable) return
        item.progress = (e.loaded / e.total) * 100
        if (item.progress >= 100) {
          item.status = "saving"
        }
      }

      xhr.onload = async () => {
        const text = xhr.responseText || ""
        let data: { error?: { code?: string; message?: string } } | null = null
        try {
          data = text ? JSON.parse(text) : null
        } catch {
          // 非 JSON 响应体
        }
        if (xhr.status >= 200 && xhr.status < 300) {
          item.status = "done"
          item.progress = 100
          finish()
          return
        }
        if (!retried && xhr.status === 403 && data?.error?.code === "csrf_invalid") {
          // CSRF Cookie 过期：重新获取后重试一次（与 apiFetch 的重试策略一致）
          retried = true
          await refreshCsrfCookie()
          send()
          return
        }
        item.status = "failed"
        item.message = data?.error?.message || `上传失败 (${xhr.status})`
        finish()
      }

      xhr.onerror = () => {
        item.status = "failed"
        item.message = "网络错误，上传中断"
        finish()
      }
      xhr.onabort = () => {
        item.status = "failed"
        item.message = "已取消"
        finish()
      }

      send()
    })
  }

  async function startUpload(dir: string, onAllDone: (succeeded: number, failed: number) => void) {
    if (state.uploading) return
    const pending = state.items.filter((it) => it.status === "pending")
    if (!pending.length) return
    state.uploading = true
    state.succeeded = 0
    state.failed = 0

    for (const item of pending) {
      await uploadOne(item, dir)
      if (item.status === "done") state.succeeded++
      else state.failed++
    }

    state.uploading = false
    if (state.succeeded > 0 && state.failed === 0) {
      showToast(`上传完成，共 ${state.succeeded} 个文件`, "success")
    } else if (state.failed > 0) {
      showToast(`上传结束：成功 ${state.succeeded} 个，失败 ${state.failed} 个`, state.succeeded ? "warning" : "error")
    }
    onAllDone(state.succeeded, state.failed)
  }

  function abortCurrent() {
    if (currentXHR) currentXHR.abort()
  }

  return {
    state,
    pendingCount,
    addFiles,
    removeItem,
    clearFinished,
    resetQueue,
    startUpload,
    abortCurrent,
  }
}
