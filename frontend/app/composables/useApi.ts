// API 访问层：等价迁移自原 core/api.js（CSRF 双提交、错误解析、剪贴板与导航助手）。
// 全部为纯函数，不依赖组件上下文。

export function getCookie(name: string): string {
  const prefix = name + "="
  const parts = document.cookie.split(";")
  for (const part of parts) {
    const trimmed = part.trim()
    if (trimmed.startsWith(prefix)) {
      return decodeURIComponent(trimmed.slice(prefix.length))
    }
  }
  return ""
}

export async function refreshCsrfCookie(): Promise<void> {
  try {
    await fetch("/healthz", { credentials: "same-origin" })
  } catch {
    // 忽略：后续请求会再次失败并给出可读错误
  }
}

export interface ApiFetchOptions extends RequestInit {}

export async function apiFetch(url: string, options: RequestInit = {}): Promise<Response> {
  const method = (options.method || "GET").toUpperCase()
  const headers = new Headers(options.headers || {})
  if (method !== "GET" && method !== "HEAD") {
    const token = getCookie("csrf_token")
    if (token) headers.set("X-CSRF-Token", token)
  }
  const send = () => fetch(url, { ...options, headers, credentials: "same-origin" })
  let resp = await send()
  if (resp.status === 403) {
    const text = await resp.clone().text().catch(() => "")
    if (text.includes("csrf_invalid")) {
      // CSRF Cookie 过期：重新获取后重试一次
      await refreshCsrfCookie()
      resp = await send()
    }
  }
  return resp
}

export async function parseAPIError(resp: Response, bodyText: string): Promise<string> {
  if (bodyText) {
    try {
      const parsed = JSON.parse(bodyText)
      const message = parsed?.error?.message
      if (typeof message === "string" && message) return message
    } catch {
      // 非 JSON 响应体，走状态码映射
    }
  }
  switch (resp.status) {
    case 401: return "访问未授权或登录已过期"
    case 403: return "操作被拒绝，无权访问"
    case 429: return "请求过于频繁，请稍候再试"
    case 502: case 503: case 504: return "服务器响应异常，请稍后重试"
    default: return `请求失败 (${resp.status})`
  }
}

export function triggerDownloadNavigation(url: string): void {
  const anchor = document.createElement("a")
  anchor.href = url
  anchor.rel = "noopener"
  document.body.appendChild(anchor)
  anchor.click()
  anchor.remove()
}

export async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    // 回退到 execCommand
  }
  try {
    const textarea = document.createElement("textarea")
    textarea.value = text
    textarea.style.position = "fixed"
    textarea.style.opacity = "0"
    document.body.appendChild(textarea)
    textarea.select()
    const ok = document.execCommand("copy")
    textarea.remove()
    return ok
  } catch {
    return false
  }
}

export function shareTokenFromLocation(): string {
  return new URLSearchParams(window.location.search).get("s") || ""
}

export function dirTokenFromLocation(): string {
  return new URLSearchParams(window.location.search).get("d") || ""
}

export function dirPasswordFromLocation(): string {
  const params = new URLSearchParams(window.location.search)
  return params.get("pwd") || params.get("password") || ""
}

const SHARE_PWD_PREFIX = "jkqp_share_pwd_"

export function getStoredSharePassword(token: string): string {
  try {
    return sessionStorage.getItem(SHARE_PWD_PREFIX + token) || ""
  } catch {
    return ""
  }
}

export function setStoredSharePassword(token: string, password: string): void {
  try {
    if (password) sessionStorage.setItem(SHARE_PWD_PREFIX + token, password)
    else sessionStorage.removeItem(SHARE_PWD_PREFIX + token)
  } catch {
    // 隐私模式等场景忽略
  }
}
