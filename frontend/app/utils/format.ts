// 文件元数据格式化工具（等价迁移自原 components.js）

const EXT_ICONS: Record<string, string> = {
  jpg: "image", jpeg: "image", png: "image", gif: "image", webp: "image", svg: "image", bmp: "image", ico: "image",
  mp4: "film", mkv: "film", avi: "film", mov: "film", webm: "film", flv: "film", m4v: "film",
  mp3: "music", flac: "music", wav: "music", aac: "music", ogg: "music", m4a: "music",
  txt: "file-text", md: "file-text", markdown: "file-text", log: "file-text", pdf: "file-text",
  doc: "file-text", docx: "file-text",
  xls: "file-spreadsheet", xlsx: "file-spreadsheet", csv: "file-spreadsheet",
  zip: "archive", rar: "archive", "7z": "archive", tar: "archive", gz: "archive", bz2: "archive",
  js: "file-code", ts: "file-code", html: "file-code", css: "file-code", json: "file-code",
  go: "file-code", py: "file-code", java: "file-code", c: "file-code", cpp: "file-code",
  rs: "file-code", sh: "file-code", sql: "file-code", yaml: "file-code", yml: "file-code",
  apk: "smartphone", ipa: "smartphone", exe: "package", dmg: "package",
}

const PREVIEW_IMAGE_EXTS = ["jpg", "jpeg", "png", "gif", "webp", "bmp", "svg", "ico"]

export function fileIconMeta(name: string, isDir: boolean): { name: string; category: string } {
  if (isDir) return { name: "folder", category: "folder" }
  const ext = (name || "").split(".").pop()?.toLowerCase() ?? ""
  const icon = EXT_ICONS[ext] || "file"
  return { name: icon, category: icon }
}

export function isPreviewableImage(name: string): boolean {
  const ext = (name || "").split(".").pop()?.toLowerCase() ?? ""
  return PREVIEW_IMAGE_EXTS.includes(ext)
}

export function formatSize(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "-"
  const units = ["B", "KB", "MB", "GB", "TB"]
  let value = bytes
  let unitIndex = 0
  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024
    unitIndex++
  }
  const text = value >= 100 || unitIndex === 0 ? Math.round(value).toString() : value.toFixed(1)
  return `${text} ${units[unitIndex]}`
}

export function formatTime(ts: number): string {
  if (!Number.isFinite(ts) || ts <= 0) return "-"
  const d = new Date(ts * 1000)
  const pad = (n: number) => (n < 10 ? "0" + n : "" + n)
  return (
    d.getFullYear() + "-" + pad(d.getMonth() + 1) + "-" + pad(d.getDate()) +
    " " + pad(d.getHours()) + ":" + pad(d.getMinutes())
  )
}
