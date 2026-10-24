package web

import "embed"

// FS 嵌入前端构建产物（frontend/ 的 Nuxt 输出，见 frontend/package.json 的 build 脚本）。
// 目录约定：frontend 构建输出到 web/dist（nuxt generate）。
//
//go:embed all:dist
var FS embed.FS
