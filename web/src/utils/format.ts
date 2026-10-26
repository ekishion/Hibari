/** 字节数转可读大小。空文件与空配额显示 0 B，未知值显示 —。 */
export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return '—'
  if (bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let value = bytes
  let i = 0
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024
    i++
  }
  return `${value < 10 && i > 0 ? value.toFixed(1) : Math.round(value)} ${units[i]}`
}

/** Unix 秒转本地日期，仅显示到分钟。 */
export function formatTime(unix: number): string {
  if (!unix) return '—'
  const d = new Date(unix * 1000)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

/** RFC3339 时间戳转本地日期，仅显示到秒。 */
export function formatStamp(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

/** 从绝对路径取父目录。根目录的父目录仍是根。 */
export function parentOf(path: string): string {
  if (path === '/' || !path) return '/'
  const i = path.lastIndexOf('/')
  return i <= 0 ? '/' : path.slice(0, i)
}

/** 把绝对路径拆成可点击的层级。 */
export function breadcrumbs(path: string): { name: string; path: string }[] {
  const parts = path.split('/').filter(Boolean)
  const out = [{ name: '根目录', path: '/' }]
  let acc = ''
  for (const part of parts) {
    acc += `/${part}`
    out.push({ name: part, path: acc })
  }
  return out
}

export type PreviewKind = 'image' | 'video' | 'audio' | 'text' | 'none'

const IMAGE = /\.(avif|bmp|gif|jpe?g|png|svg|webp)$/i
const VIDEO = /\.(mp4|webm|mov|m4v|mkv)$/i
const AUDIO = /\.(mp3|m4a|aac|flac|ogg|wav|opus)$/i
const TEXT = /\.(txt|md|markdown|json|ya?ml|toml|ini|conf|log|csv|tsv|xml|html?|css|scss|js|mjs|cjs|ts|tsx|jsx|vue|py|rb|rs|go|java|c|h|cpp|hpp|sh|bash|zsh|ps1|sql|env|gitignore|dockerfile)$/i

/** 依据扩展名判断预览方式。 */
export function previewKind(name: string): PreviewKind {
  if (IMAGE.test(name)) return 'image'
  if (VIDEO.test(name)) return 'video'
  if (AUDIO.test(name)) return 'audio'
  if (TEXT.test(name)) return 'text'
  return 'none'
}
