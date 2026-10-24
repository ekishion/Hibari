// 深浅色主题：等价迁移自原 app.js 主题管理（localStorage + 系统跟随 + meta theme-color 同步）

export type Theme = "light" | "dark"

const THEME_STORAGE_KEY = "jikeqingpan_theme"

const theme = ref<Theme>("light")

function readStoredTheme(): Theme {
  try {
    const stored = localStorage.getItem(THEME_STORAGE_KEY)
    if (stored === "light" || stored === "dark") return stored
  } catch {
    // ignore
  }
  if (typeof window !== "undefined" && window.matchMedia?.("(prefers-color-scheme: dark)").matches) {
    return "dark"
  }
  return "light"
}

function applyTheme(next: Theme) {
  theme.value = next
  if (typeof document === "undefined") return
  document.documentElement.setAttribute("data-theme", next === "dark" ? "dark" : "light")
  const themeColor = document.querySelector('meta[name="theme-color"]')
  if (themeColor) themeColor.setAttribute("content", next === "dark" ? "#0a0a0b" : "#ffffff")
}

export function useTheme() {
  if (!theme.value || !document.documentElement.hasAttribute("data-theme")) {
    applyTheme(readStoredTheme())
  }

  const isDark = computed(() => theme.value === "dark")

  function toggle() {
    const next: Theme = theme.value === "dark" ? "light" : "dark"
    applyTheme(next)
    try {
      localStorage.setItem(THEME_STORAGE_KEY, next)
    } catch {
      // ignore
    }
  }

  return { theme, isDark, toggle }
}
