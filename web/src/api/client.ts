import type {
  ApiError,
  AuditEvent,
  Entry,
  Listing,
  Quota,
  Share,
  ShareInfo,
  Status,
  User,
} from './types'

/** 带后端错误码的异常，UI 直接展示 message。 */
export class ApiFailure extends Error {
  constructor(
    readonly code: string,
    message: string,
    readonly status: number,
  ) {
    super(message)
    this.name = 'ApiFailure'
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    credentials: 'same-origin',
    headers: init?.body ? { 'Content-Type': 'application/json' } : undefined,
    ...init,
  })

  if (res.status === 204) return undefined as T

  const text = await res.text()
  let data: unknown = null
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      // 反向代理的错误页、网关超时页都不是 JSON。
      throw new ApiFailure('bad_response', `响应不是合法 JSON（${res.status}）`, res.status)
    }
  }

  if (!res.ok) {
    const body = data as ApiError | null
    throw new ApiFailure(
      body?.error?.code ?? 'unknown',
      body?.error?.message ?? `请求失败（${res.status}）`,
      res.status,
    )
  }
  return data as T
}

const get = <T>(path: string) => request<T>(path)
const post = <T>(path: string, body?: unknown) =>
  request<T>(path, { method: 'POST', body: body === undefined ? undefined : JSON.stringify(body) })
const del = <T>(path: string) => request<T>(path, { method: 'DELETE' })

export const api = {
  status: () => get<Status>('/api/status'),

  setup: (username: string, password: string) =>
    post<{ user: User }>('/api/setup', { username, password }),

  login: (username: string, password: string) =>
    post<{ user: User }>('/api/login', { username, password }),

  logout: () => post<{ ok: boolean }>('/api/logout'),

  changePassword: (current: string, next: string) =>
    post<{ ok: boolean }>('/api/password', {
      current_password: current,
      new_password: next,
    }),

  list: (dir: string) => get<Listing>(`/api/files?dir=${encodeURIComponent(dir)}`),

  quota: () => get<Quota>('/api/quota'),

  mkdir: (dir: string, name: string) => post<{ path: string }>('/api/files/mkdir', { dir, name }),

  rename: (path: string, newName: string) =>
    post<{ path: string }>('/api/files/rename', { path, new_name: newName }),

  remove: (paths: string[]) => post<{ removed: number }>('/api/files/remove', { paths }),

  /** 上传用 XHR 以获得进度回调。 */
  upload(dir: string, file: File, onProgress?: (percent: number) => void): Promise<{ path: string; size: number }> {
    const form = new FormData()
    form.append('dir', dir)
    form.append('name', file.name)
    form.append('file', file)

    return new Promise((resolve, reject) => {
      const xhr = new XMLHttpRequest()
      xhr.open('POST', '/api/upload')
      xhr.upload.onprogress = (e) => {
        if (e.lengthComputable) onProgress?.(Math.round((e.loaded / e.total) * 100))
      }
      xhr.onload = () => {
        let body: unknown = null
        try {
          body = JSON.parse(xhr.responseText)
        } catch {
          /* 空响应或非 JSON，交给下面统一处理 */
        }
        if (xhr.status >= 200 && xhr.status < 300) {
          resolve(body as { path: string; size: number })
          return
        }
        const err = (body as ApiError | null)?.error
        reject(new ApiFailure(err?.code ?? 'unknown', err?.message ?? `上传失败（${xhr.status}）`, xhr.status))
      }
      xhr.onerror = () => reject(new ApiFailure('network', '网络中断，上传失败', 0))
      xhr.send(form)
    })
  },

  createShare: (path: string, password: string, ttlSeconds: number) =>
    post<Share>('/api/shares', { path, password, ttl_seconds: ttlSeconds }),

  listShares: () => get<{ shares: Share[] }>('/api/shares'),

  revokeShare: (token: string) => del<{ ok: boolean }>(`/api/shares/${token}`),

  shareInfo: (token: string, password: string) =>
    post<ShareInfo>('/api/share/info', { token, password }),

  shareBrowse: (token: string, password: string, dir: string) =>
    post<{ dir: string; entries: Entry[] }>('/api/share/browse', { token, password, dir }),

  audit: (limit = 100) => get<{ events: AuditEvent[] }>(`/api/audit?limit=${limit}`),

  rawURL: (path: string, download = false) =>
    `/api/files/raw?path=${encodeURIComponent(path)}${download ? '&download=1' : ''}`,
}
