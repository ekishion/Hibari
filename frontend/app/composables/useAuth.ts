// 鉴权与前端 UI 配置：等价迁移自原 app.js 鉴权管理
// （auth/status 引导、登录/退出、401 处理、uiConfig 分发）

export interface UiConfig {
  showReadme: boolean
  showReadmeOverview: boolean
  sharePasswordLength: number
  uploadEnabled: boolean
  uploadMaxMB: number
}

const defaultUiConfig: UiConfig = {
  showReadme: true,
  showReadmeOverview: true,
  sharePasswordLength: 6,
  uploadEnabled: false,
  uploadMaxMB: 2048,
}

const authRequired = ref(false)
const authenticated = ref(false)
const uiConfig = reactive<UiConfig>({ ...defaultUiConfig })
// 登录弹窗状态：message 非空表示带错误提示打开
const loginPrompt = ref<{ open: boolean; message: string }>({ open: false, message: "" })

export function useAuth() {
  function requireLogin(message = "") {
    authRequired.value = true
    authenticated.value = false
    loginPrompt.value = { open: true, message }
  }

  function dismissLogin() {
    loginPrompt.value = { open: false, message: "" }
  }

  async function checkAuth(): Promise<boolean> {
    const resp = await apiFetch("/api/auth/status", { method: "GET" })
    if (!resp.ok) throw new Error("无法确认登录状态")
    const data = await resp.json()
    authRequired.value = !!data.auth_required
    authenticated.value = !!data.authenticated
    uiConfig.showReadme = data.show_readme !== false
    uiConfig.showReadmeOverview = data.show_readme_overview !== false
    if (typeof data.share_password_length === "number" && data.share_password_length >= 2) {
      uiConfig.sharePasswordLength = data.share_password_length
    }
    uiConfig.uploadEnabled = data.upload_enabled === true
    if (typeof data.upload_max_mb === "number" && data.upload_max_mb >= 0) {
      uiConfig.uploadMaxMB = data.upload_max_mb
    }
    if (authRequired.value && !authenticated.value) {
      requireLogin("")
      return false
    }
    dismissLogin()
    return true
  }

  async function doLogin(token: string): Promise<void> {
    const resp = await apiFetch("/api/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ token }),
    })
    const text = await resp.text()
    if (!resp.ok) {
      const err = new Error(await parseAPIError(resp, text)) as Error & { retryAfter?: number }
      if (resp.status === 429) {
        const retry = parseInt(resp.headers.get("Retry-After") || "", 10)
        if (!Number.isNaN(retry) && retry > 0) err.retryAfter = retry
      }
      throw err
    }
    authRequired.value = true
    authenticated.value = true
    dismissLogin()
  }

  async function doLogout(): Promise<void> {
    try {
      await apiFetch("/api/logout", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: "{}",
      })
    } catch {
      // 无论请求结果如何都清理本地登录态
    }
    authenticated.value = false
    if (authRequired.value) {
      requireLogin("")
    }
  }

  return {
    authRequired,
    authenticated,
    uiConfig,
    loginPrompt,
    requireLogin,
    dismissLogin,
    checkAuth,
    doLogin,
    doLogout,
  }
}
