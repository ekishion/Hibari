import { fileURLToPath, URL } from 'node:url'
import { writeFileSync } from 'node:fs'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

const outDir = '../internal/httpapi/assets'

export default defineConfig({
  plugins: [
    vue(),
    {
      // emptyOutDir 会删掉 .gitkeep，但它是干净克隆下 go:embed 能编译的唯一保障。
      name: 'keep-embed-placeholder',
      closeBundle() {
        writeFileSync(fileURLToPath(new URL(`${outDir}/.gitkeep`, import.meta.url)), '')
      },
    },
  ],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://127.0.0.1:4172',
      '/healthz': 'http://127.0.0.1:4172',
    },
  },
  build: {
    // 直接产出到 Go 的嵌入目录，构建即打包。
    outDir,
    emptyOutDir: true,
    target: 'es2022',
  },
})
