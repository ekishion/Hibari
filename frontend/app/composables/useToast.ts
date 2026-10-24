// 全局消息提示：单条复用语义与原 components.js showToast 一致（底部居中、2.8s 自动消失）

export type ToastType = "error" | "warning" | "success" | "info"

interface ToastState {
  message: string
  type: ToastType
  visible: boolean
}

const state = reactive<ToastState>({ message: "", type: "info", visible: false })

let hideTimer: ReturnType<typeof setTimeout> | null = null

const TOAST_ICON: Record<ToastType, string> = {
  error: "circle-alert",
  warning: "triangle-alert",
  success: "circle-check",
  info: "info",
}

export function useToast() {
  function showToast(message: string, type: ToastType = "info") {
    state.message = message
    state.type = type
    state.visible = true
    if (hideTimer) clearTimeout(hideTimer)
    hideTimer = setTimeout(() => {
      state.visible = false
    }, 2800)
  }

  return { state, showToast, iconFor: (type: ToastType) => TOAST_ICON[type] }
}
