import { fileURLToPath } from 'node:url'

// 即刻轻盘前端：SPA 模式（ssr:false，登录态/网盘数据全部来自同源 API）。
// 构建产物输出到 ../web/dist，由后端 go:embed 打进单二进制。
export default defineNuxtConfig({
  ssr: false,

  app: {
    head: {
      htmlAttrs: { lang: 'zh-CN' },
      title: '即刻轻盘',
      meta: [
        { charset: 'UTF-8' },
        { name: 'viewport', content: 'width=device-width, initial-scale=1.0' },
        { name: 'color-scheme', content: 'light dark' },
        { name: 'theme-color', content: '#ffffff' },
        {
          name: 'description',
          content: '基于百度网盘青春版的极简文件中转站，支持文件夹浏览与安全下载。',
        },
      ],
      link: [
        {
          rel: 'icon',
          type: 'image/svg+xml',
          href: "data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24'%3E%3Crect width='24' height='24' rx='6' fill='%230a0a0a'/%3E%3Cpath d='M17.5 17H9a4 4 0 1 1 3.8-5.2A3 3 0 0 1 17.5 17Z' fill='none' stroke='white' stroke-width='1.6' stroke-linejoin='round'/%3E%3C/svg%3E",
        },
      ],
    },
  },

  nitro: {
    output: {
      publicDir: fileURLToPath(new URL('../web/dist', import.meta.url)),
    },
  },

  // 开发模式：把 API 与下载短链代理到本地后端（go run ./cmd/jikeqingpan）
  devProxy: {
    '/api': { target: 'http://127.0.0.1:4172', changeOrigin: false },
    '/d': { target: 'http://127.0.0.1:4172', changeOrigin: false },
    '/healthz': { target: 'http://127.0.0.1:4172', changeOrigin: false },
  },

  compatibilityDate: '2026-10-24',
  typescript: { strict: true, typeCheck: false },
})
