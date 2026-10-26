/**
 * 主题偏好。颜色本身由 tokens.css 的 light-dark() 提供，
 * 这里只决定 color-scheme 取哪一侧，不需要第二套变量。
 */
import { reactive, readonly, watch } from 'vue'

export type Theme = 'light' | 'dark'

const KEY = 'hibari.theme'

// localStorage 在部分隐私模式下会抛异常，读不到就跟随系统。
function initial(): Theme {
  try {
    const saved = localStorage.getItem(KEY)
    if (saved === 'light' || saved === 'dark') return saved
  } catch {
    // 忽略：退回系统偏好。
  }
  return matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

const state = reactive<{ theme: Theme }>({ theme: initial() })

function apply(theme: Theme) {
  document.documentElement.style.colorScheme = theme
}

apply(state.theme)
watch(
  () => state.theme,
  (t) => {
    apply(t)
    try {
      localStorage.setItem(KEY, t)
    } catch {
      // 存不下也不影响本次会话。
    }
  },
)

export const theme = {
  state: readonly(state),
  toggle() {
    state.theme = state.theme === 'dark' ? 'light' : 'dark'
  },
}
