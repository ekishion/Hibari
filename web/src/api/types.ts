/** 后端契约类型。字段与 internal/httpapi 的 DTO 一一对应。 */

export interface ApiError {
  error: { code: string; message: string }
}

export interface User {
  id: string
  username: string
  role: string
}

export interface Caps {
  write: boolean
  upload: boolean
  quota: boolean
}

export interface Status {
  needs_setup: boolean
  version: string
  backend: string
  caps: Caps
  authenticated: boolean
  user?: User
}

export interface Entry {
  name: string
  path: string
  is_dir: boolean
  size: number
  mod_time: number
}

export interface Listing {
  dir: string
  entries: Entry[]
}

export interface Quota {
  total: number
  used: number
  free: number
}

export interface Share {
  token: string
  kind: 'file' | 'dir'
  path: string
  name: string
  has_password: boolean
  max_uses: number
  uses: number
  expires_at: number
  created_at: number
}

export interface ShareInfo {
  token: string
  kind?: 'file' | 'dir'
  name?: string
  has_password: boolean
  unlocked: boolean
  expires_at?: number
}

export interface AuditEvent {
  event: string
  path: string
  client_ip: string
  user_agent: string
  created_at: string
}
