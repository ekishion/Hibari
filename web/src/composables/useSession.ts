/** 全局应用状态：后端能力、登录身份。单例，避免各页面重复请求 /api/status。 */
import { reactive, readonly } from 'vue'
import { api } from '@/api/client'
import type { Status, User } from '@/api/types'

const state = reactive<{
  loaded: boolean
  needsSetup: boolean
  version: string
  backend: string
  caps: { write: boolean; upload: boolean; quota: boolean }
  user: User | null
}>({
  loaded: false,
  needsSetup: false,
  version: '',
  backend: '',
  caps: { write: false, upload: false, quota: false },
  user: null,
})

async function refresh(): Promise<Status> {
  const s = await api.status()
  state.loaded = true
  state.needsSetup = s.needs_setup
  state.version = s.version
  state.backend = s.backend
  state.caps = s.caps
  state.user = s.authenticated ? (s.user ?? null) : null
  return s
}

function setUser(user: User | null) {
  state.user = user
  if (user) state.needsSetup = false
}

export const session = {
  state: readonly(state),
  refresh,
  setUser,
  get authenticated() {
    return state.user !== null
  },
}
