// 预览类型判定：等价迁移自原 preview.js 头部
// （TEXT 清单需与后端 handlers_preview.go 的 textPreviewExts 白名单保持同步）

const TEXT_PREVIEW_EXTS = [
  "txt", "md", "markdown", "json", "xml", "yaml", "yml", "csv", "log", "ini", "conf",
  "sh", "bat", "ps1", "py", "js", "ts", "go", "c", "h", "cpp", "hpp", "java", "rs",
  "sql", "toml", "html", "css",
]
const VIDEO_PREVIEW_EXTS = ["mp4", "m4v", "webm", "mov"]
const AUDIO_PREVIEW_EXTS = ["mp3", "flac", "wav", "aac", "ogg", "m4a"]

export type PreviewKind = "image" | "text" | "video" | "audio"

export function fileExtOf(name: string): string {
  return (name || "").split(".").pop()?.toLowerCase() ?? ""
}

export function previewKind(name: string): PreviewKind | null {
  if (isPreviewableImage(name)) return "image"
  const ext = fileExtOf(name)
  if (TEXT_PREVIEW_EXTS.includes(ext)) return "text"
  if (VIDEO_PREVIEW_EXTS.includes(ext)) return "video"
  if (AUDIO_PREVIEW_EXTS.includes(ext)) return "audio"
  return null
}
