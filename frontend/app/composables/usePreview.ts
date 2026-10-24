// 预览灯箱状态：等价迁移自原 preview.js（数据加载与导航），媒体控件交互在 Lightbox 组件内
import type { PreviewKind } from '~/utils/previewExts'

export interface PreviewNav {
  path: string
  name: string
}

interface PreviewState {
  open: boolean
  kind: PreviewKind | null
  path: string
  name: string
  truncated: boolean
  spinner: boolean
  imageUrl: string
  textContent: string
  textIsMarkdown: boolean
  mediaSrc: string
  navList: PreviewNav[]
  navIndex: number
  seq: number
}

const state = reactive<PreviewState>({
  open: false,
  kind: null,
  path: "",
  name: "",
  truncated: false,
  spinner: true,
  imageUrl: "",
  textContent: "",
  textIsMarkdown: false,
  mediaSrc: "",
  navList: [],
  navIndex: -1,
  seq: 0,
})

let objectUrl: string | null = null

function revokeObjectUrl() {
  if (objectUrl) {
    URL.revokeObjectURL(objectUrl)
    objectUrl = null
  }
}

function resetPanes() {
  state.spinner = true
  state.imageUrl = ""
  state.textContent = ""
  state.textIsMarkdown = false
  state.mediaSrc = ""
  revokeObjectUrl()
}

function closePreview() {
  if (!state.open) return
  state.seq++
  state.open = false
  state.kind = null
  state.path = ""
  state.name = ""
  state.truncated = false
  resetPanes()
}

function openPreview(filePath: string, fileName: string) {
  const { requireLogin } = useAuth()
  const { showToast } = useToast()
  if (!filePath) {
    showToast("文件路径无效，请刷新后重试", "warning")
    return
  }
  const kind = previewKind(fileName)
  if (!kind) {
    showToast("该文件类型暂不支持在线预览", "warning")
    return
  }
  buildNavList(kind, filePath)
  loadPreviewContent(kind, filePath, fileName, requireLogin, showToast)
}

// buildPreviewNavList 收集当前目录同类可预览文件，供图片上一张/下一张导航
function buildNavList(kind: PreviewKind, path: string) {
  state.navList = []
  state.navIndex = -1
  if (kind !== "image") return
  const { currentFiles } = useFiles()
  for (const f of currentFiles.value) {
    if (Number(f.isdir) === 1) continue
    const name = f.server_filename || f.filename || (f.path || "").split("/").pop() || ""
    if (previewKind(name) !== "image") continue
    state.navList.push({ path: f.path || "", name })
  }
  state.navIndex = state.navList.findIndex((item) => item.path === path)
  if (state.navIndex === -1 && state.navList.length) state.navIndex = 0
}

function navigatePreview(delta: number) {
  const target = state.navIndex + delta
  if (target < 0 || target >= state.navList.length) return
  state.navIndex = target
  const item = state.navList[target]
  const { requireLogin } = useAuth()
  const { showToast } = useToast()
  loadPreviewContent("image", item.path, item.name, requireLogin, showToast)
}

// 沙盒访客 401（token 失效/提取码变更）不弹管理员登录框，改提示可读错误
async function previewUnauthorized(
  resp: Response,
  inSandbox: boolean,
  requireLogin: (message?: string) => void,
  showToast: (message: string, type: "error" | "warning" | "success" | "info") => void,
) {
  closePreview()
  const message = await parseAPIError(resp, await resp.text())
  if (inSandbox) showToast(message, "error")
  else requireLogin(message)
}

async function loadPreviewContent(
  kind: PreviewKind,
  filePath: string,
  fileName: string,
  requireLogin: (message?: string) => void,
  showToast: (message: string, type: "error" | "warning" | "success" | "info") => void,
) {
  // 沙盒模式（目录短链）下预览请求需携带 token + 提取码凭证
  const { currentDirToken, currentDirPassword } = useFiles()
  const inSandbox = !!currentDirToken.value
  const sandboxPayload = inSandbox
    ? { token: currentDirToken.value, password: currentDirPassword.value }
    : {}
  state.seq++
  const seq = state.seq
  state.kind = kind
  state.path = filePath
  state.name = fileName
  state.truncated = false
  resetPanes()
  state.open = true

  try {
    if (kind === "image") {
      const resp = await apiFetch("/api/preview", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ path: filePath, ...sandboxPayload }),
      })
      if (resp.status === 401) {
        await previewUnauthorized(resp, inSandbox, requireLogin, showToast)
        return
      }
      if (!resp.ok) throw new Error(await parseAPIError(resp, await resp.text()))
      const blob = await resp.blob()
      if (seq !== state.seq) return
      const url = URL.createObjectURL(blob)
      objectUrl = url
      state.imageUrl = url
      state.spinner = false
    } else if (kind === "text") {
      const resp = await apiFetch("/api/text", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ path: filePath, ...sandboxPayload }),
      })
      if (resp.status === 401) {
        await previewUnauthorized(resp, inSandbox, requireLogin, showToast)
        return
      }
      const text = await resp.text()
      if (!resp.ok) throw new Error(await parseAPIError(resp, text))
      const data = JSON.parse(text)
      if (seq !== state.seq) return
      state.spinner = false
      const ext = fileExtOf(fileName)
      state.textIsMarkdown = ext === "md" || ext === "markdown"
      state.textContent = data.content || ""
      state.truncated = !!data.truncated
    } else {
      // 媒体经无状态中转接口 302 到百度直链：浏览器自动透传 Range，
      // 不经 VPS 中转流量（CSP media-src 已放行百度直链域）。
      // 先探测拿可读错误再赋值 src；全程不创建任何对外链接。
      const streamUrl =
        "/api/stream?path=" + encodeURIComponent(filePath) +
        (inSandbox ? "&token=" + encodeURIComponent(currentDirToken.value) + "&pwd=" + encodeURIComponent(currentDirPassword.value) : "")
      const resp = await apiFetch(streamUrl, { headers: { "X-Stream-Probe": "1" } })
      if (resp.status === 401) {
        await previewUnauthorized(resp, inSandbox, requireLogin, showToast)
        return
      }
      if (!resp.ok) throw new Error(await parseAPIError(resp, await resp.text()))
      await resp.json()
      if (seq !== state.seq) return
      state.spinner = false
      state.mediaSrc = streamUrl
    }
  } catch (err) {
    if (seq === state.seq) {
      closePreview()
      showToast("预览失败：" + ((err as Error).message || "未知错误"), "error")
    }
  }
}

export function usePreview() {
  return {
    state,
    openPreview,
    closePreview,
    navigatePreview,
  }
}
