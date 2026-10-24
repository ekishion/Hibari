// 目录浏览状态：等价迁移自原 file_browser.js
// （沙盒虚拟根隔离、受限面包屑、列表渲染、排序检索与 README 面板）

export interface BaiduFile {
  fs_id?: number
  path?: string
  server_filename?: string
  filename?: string
  size?: number | string
  isdir?: number | string
  server_mtime?: number
  server_ctime?: number
  mtime?: number
}

export type SortKey = "name" | "size" | "time"
export type SortDir = "asc" | "desc"

const SORT_STORAGE_KEY = "jikeqingpan_sort"

export function fileNameOf(file: BaiduFile): string {
  return file.server_filename || file.filename || (file.path || "").split("/").pop() || "未命名"
}

export function fileTimeOf(file: BaiduFile): number {
  return Number(file.server_mtime || file.server_ctime || file.mtime) || 0
}

function sortFiles(files: BaiduFile[], sortKey: SortKey, sortDir: SortDir): BaiduFile[] {
  const list = (files || []).slice()
  list.sort((a, b) => {
    const aIsDir = Number(a.isdir) === 1
    const bIsDir = Number(b.isdir) === 1
    if (aIsDir !== bIsDir) return aIsDir ? -1 : 1

    let res = 0
    if (sortKey === "size") {
      res = (Number(a.size) || 0) - (Number(b.size) || 0)
    } else if (sortKey === "time") {
      res = fileTimeOf(a) - fileTimeOf(b)
    } else {
      const aName = a.server_filename || a.filename || (a.path || "").split("/").pop() || ""
      const bName = b.server_filename || b.filename || (b.path || "").split("/").pop() || ""
      res = aName.localeCompare(bName, "zh-CN", { numeric: true, sensitivity: "base" })
    }
    return sortDir === "desc" ? -res : res
  })
  return list
}

const currentFiles = ref<BaiduFile[]>([])
const currentDir = ref("/")
const sandboxRoot = ref("")
const currentDirToken = ref("")
const currentDirPassword = ref("")
const searchQuery = ref("")
const loading = ref(false)
const fileListSeq = ref(0)

// 排序状态：以 const 持有并只做原地修改（等价原 app.js 的注释说明）
const sortState = reactive<{ key: SortKey; dir: SortDir }>({ key: "name", dir: "asc" })

// README 面板与目录概览
const README_FILE_NAMES = ["readme.md", "readme.markdown", "readme.txt", "readme"]
const readmeState = reactive<{ visible: boolean; content: string; source: string }>({
  visible: false,
  content: "",
  source: "README",
})
const readmeSeq = ref(0)

// 目录短链解锁弹窗回调（由 UnlockModal 组件消费）；message 为上次失败的可读原因
const unlockRequest = ref<{ open: boolean; message: string; onUnlock: (pwd: string) => void }>({
  open: false,
  message: "",
  onUnlock: () => {},
})

let sortLoaded = false

export function useFiles() {
  const { uiConfig } = useAuth()
  const { showToast } = useToast()

  if (!sortLoaded && typeof localStorage !== "undefined") {
    sortLoaded = true
    try {
      const parsed = JSON.parse(localStorage.getItem(SORT_STORAGE_KEY) || "null")
      if (parsed && ["name", "size", "time"].includes(parsed.key) && ["asc", "desc"].includes(parsed.dir)) {
        sortState.key = parsed.key
        sortState.dir = parsed.dir
      }
    } catch {
      // ignore
    }
  }

  function saveSortState() {
    try {
      localStorage.setItem(SORT_STORAGE_KEY, JSON.stringify(sortState))
    } catch {
      // ignore
    }
  }

  function setSort(key: SortKey, dir: SortDir) {
    if (sortState.key !== key || sortState.dir !== dir) {
      sortState.key = key
      sortState.dir = dir
      saveSortState()
    }
  }

  function resetFileSearch() {
    searchQuery.value = ""
  }

  // 目录短链解锁：打开弹窗（携带上次失败原因）并在解锁成功后携带新提取码重载
  function requestUnlock(retry: () => void, message = "") {
    unlockRequest.value = {
      open: true,
      message,
      onUnlock: (pwd: string) => {
        currentDirPassword.value = pwd
        unlockRequest.value = { open: false, message: "", onUnlock: () => {} }
        retry()
      },
    }
  }

  async function loadFiles(dir: string, opts: { refresh?: boolean } = {}) {
    fileListSeq.value++
    const seq = fileListSeq.value
    loading.value = true

    try {
      const resp = await apiFetch("/api/files", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          dir: dir || currentDir.value || "/",
          token: currentDirToken.value,
          password: currentDirPassword.value,
          refresh: !!opts.refresh,
        }),
      })

      if (resp.status === 401) {
        const errData = await resp.json().catch(() => null)
        // 首次提示（share_password_required）与密码错误（share_password_incorrect）
        // 都回到解锁弹窗，并把服务端可读消息带过去
        if (errData?.error?.code === "share_password_required" || errData?.error?.code === "share_password_incorrect") {
          loading.value = false
          requestUnlock(() => loadFiles(dir, opts), errData?.error?.message || "")
          return
        }
        throw new Error(errData?.error?.message || "未授权访问")
      }
      if (!resp.ok) {
        const text = await resp.text()
        throw new Error(await parseAPIError(resp, text))
      }

      const data = await resp.json()
      if (seq !== fileListSeq.value) return
      loading.value = false

      if (data.sandbox_root) sandboxRoot.value = data.sandbox_root
      if (data.resolved_dir) currentDir.value = data.resolved_dir
      else if (dir) currentDir.value = dir

      currentFiles.value = data.list || []
      await checkAndLoadReadme(currentFiles.value, currentDir.value)
    } catch (err) {
      if (seq !== fileListSeq.value) return
      loading.value = false
      showToast("加载目录失败：" + ((err as Error).message || "未知错误"), "error")
    }
  }

  function enterDir(dir: string) {
    const root = sandboxRoot.value.replace(/\/+$/, "")
    if (root && !dir.startsWith(root)) {
      showToast("无法超出共享目录范围", "warning")
      return
    }
    currentDir.value = dir
    resetFileSearch()
    loadFiles(dir)
  }

  async function checkAndLoadReadme(files: BaiduFile[], dir: string) {
    // show_readme 关闭时整个面板不出现，也不发请求（后端路由同样未注册）
    if (uiConfig.showReadme === false) {
      readmeState.visible = false
      return
    }
    const readmeFile = (files || []).find((f) =>
      README_FILE_NAMES.includes(String(f.server_filename || f.filename || "").toLowerCase()),
    )
    if (!readmeFile) {
      readmeState.visible = false
      return
    }
    readmeSeq.value++
    const seq = readmeSeq.value
    const filePath =
      readmeFile.path || (dir.replace(/\/+$/, "") + "/" + (readmeFile.server_filename || readmeFile.filename))

    try {
      const resp = await apiFetch("/api/readme", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ path: filePath }),
      })
      if (!resp.ok) {
        readmeState.visible = false
        return
      }
      const data = await resp.json()
      if (seq !== readmeSeq.value || !data?.content) {
        readmeState.visible = false
        return
      }
      readmeState.visible = true
      readmeState.content = data.content
      readmeState.source = data.name || readmeFile.server_filename || "README"
    } catch {
      if (seq === readmeSeq.value) readmeState.visible = false
    }
  }

  const visibleFiles = computed(() => {
    const query = searchQuery.value.trim().toLowerCase()
    const filtered = query
      ? currentFiles.value.filter((f) =>
          (f.server_filename || f.filename || (f.path || "").split("/").pop() || "")
            .toLowerCase()
            .includes(query),
        )
      : currentFiles.value
    return sortFiles(filtered, sortState.key, sortState.dir)
  })

  // 返回上一级行：沙盒根或全盘根时为 null（等价 buildParentItem 判定）
  const parentPath = computed<string | null>(() => {
    const root = sandboxRoot.value
    if (root && (currentDir.value === root || currentDir.value === root + "/")) return null
    if (!root && (currentDir.value === "/" || !currentDir.value)) return null
    const clean = currentDir.value.replace(/\/+$/, "")
    const parent = clean.substring(0, clean.lastIndexOf("/")) || "/"
    if (root && !parent.startsWith(root)) return null
    return parent
  })

  return {
    currentFiles,
    currentDir,
    sandboxRoot,
    currentDirToken,
    currentDirPassword,
    searchQuery,
    loading,
    sortState,
    visibleFiles,
    parentPath,
    readmeState,
    unlockRequest,
    loadFiles,
    enterDir,
    setSort,
    resetFileSearch,
  }
}
