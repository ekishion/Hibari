/**
 * 全局提示。挂在 body 上的单例队列，避免每个页面自己维护一份。
 */
import { reactive } from 'vue'

export interface Toast {
  id: number
  message: string
  kind: 'info' | 'success' | 'error'
}

const state = reactive<{ items: Toast[] }>({ items: [] })
let seq = 0

function push(message: string, kind: Toast['kind'], ms: number) {
  const id = ++seq
  state.items.push({ id, message, kind })
  window.setTimeout(() => dismiss(id), ms)
}

function dismiss(id: number) {
  const i = state.items.findIndex((t) => t.id === id)
  if (i >= 0) state.items.splice(i, 1)
}

export const toast = {
  state,
  dismiss,
  info: (message: string) => push(message, 'info', 3000),
  success: (message: string) => push(message, 'success', 3000),
  error: (message: string) => push(message, 'error', 5000),
}
