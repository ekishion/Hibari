<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { api, ApiFailure } from '@/api/client'
import type { Entry, ShareInfo } from '@/api/types'
import { formatBytes, formatTime, previewKind } from '@/utils/format'
import Button from '@/ui/Button.vue'
import Input from '@/ui/Input.vue'
import { File, Folder, Image, Film, Music, FileText, ChevronLeft, Download } from 'lucide-vue-next'

const route = useRoute()

const token = computed(() => String(route.params.token ?? ''))
const password = ref(typeof route.query.pwd === 'string' ? route.query.pwd : '')

const info = ref<ShareInfo | null>(null)
const entries = ref<Entry[]>([])
const dir = ref('/')
const loading = ref(true)
const browsing = ref(false)
const error = ref('')
const passwordError = ref('')

const unlocked = computed(() => info.value?.unlocked === true)
const isDir = computed(() => info.value?.kind === 'dir')

function iconFor(entry: Entry) {
  if (entry.is_dir) return Folder
  switch (previewKind(entry.name)) {
    case 'image':
      return Image
    case 'video':
      return Film
    case 'audio':
      return Music
    case 'text':
      return FileText
    default:
      return File
  }
}

function rawURL(relPath: string, download = false) {
  const params = new URLSearchParams({ path: relPath })
  if (password.value) params.set('pwd', password.value)
  if (download) params.set('download', '1')
  return `/api/share/${token.value}/raw?${params}`
}

async function unlock() {
  passwordError.value = ''
  loading.value = true
  try {
    const res = await api.shareInfo(token.value, password.value)
    info.value = res
    if (res.unlocked) {
      await loadDir('/')
    } else {
      passwordError.value = password.value ? '提取码不正确' : ''
    }
  } catch (e) {
    error.value = e instanceof ApiFailure ? e.message : '无法打开分享'
  } finally {
    loading.value = false
  }
}

async function loadDir(path: string) {
  if (!isDir.value) return
  browsing.value = true
  try {
    const res = await api.shareBrowse(token.value, password.value, path)
    entries.value = res.entries ?? []
    dir.value = res.dir ?? path
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载目录失败'
  } finally {
    browsing.value = false
  }
}

function openEntry(entry: Entry) {
  if (entry.is_dir) {
    loadDir(entry.path)
    return
  }
  // 分享页的用途是取走文件：目录里的文件也直接下载，不在新标签里在线预览。
  location.href = rawURL(isDir.value ? entry.path : '/', true)
}

onMounted(unlock)
</script>

<template>
  <div class="page">
    <div class="card">
      <header>
        <div class="badge" aria-hidden="true">
          <component :is="isDir ? Folder : File" :size="22" />
        </div>
        <div class="meta">
          <h1>{{ info?.name || '分享内容' }}</h1>
          <p v-if="info?.expires_at" class="expiry tnum">
            有效期至 {{ formatTime(info.expires_at) }}
          </p>
        </div>
      </header>

      <p v-if="loading" class="state">正在打开…</p>
      <p v-else-if="error" class="state error" role="alert">{{ error }}</p>

      <!-- 需要提取码 -->
      <form v-else-if="!unlocked" class="gate" @submit.prevent="unlock">
        <p class="hint">此分享受提取码保护。</p>
        <Input
          v-model="password"
          label="提取码"
          :error="passwordError"
          autocomplete="off"
          autofocus
        />
        <Button type="submit" :loading="loading">打开</Button>
      </form>

      <!-- 已解锁 -->
      <template v-else>
        <!-- 单文件 -->
        <div v-if="!isDir" class="single">
          <p class="hint">此分享包含一个文件。</p>
          <a :href="rawURL('/', true)" class="primary">
            <Download :size="16" />
            下载文件
          </a>
        </div>

        <!-- 目录沙盒 -->
        <div v-else class="browser">
          <button v-if="dir !== '/'" type="button" class="up" @click="loadDir(dir.replace(/\/[^/]+$/, '') || '/')">
            <ChevronLeft :size="16" />
            返回上级
          </button>

          <p v-if="browsing" class="state">加载中…</p>
          <p v-else-if="entries.length === 0" class="state">此目录为空</p>
          <ul v-else class="list">
            <li v-for="entry in entries" :key="entry.path">
              <button type="button" class="item" @click="openEntry(entry)">
                <component :is="iconFor(entry)" :size="16" class="icon" aria-hidden="true" />
                <span class="name">{{ entry.name }}</span>
                <span class="size tnum">{{ entry.is_dir ? '' : formatBytes(entry.size) }}</span>
              </button>
            </li>
          </ul>
        </div>
      </template>
    </div>

    <p class="footer">由云雀提供分享服务</p>
  </div>
</template>

<style scoped>
.page {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--s-4);
  min-height: calc(100dvh - var(--header-h));
  padding: var(--s-8) var(--s-4) var(--s-12);
}

.card {
  display: flex;
  flex-direction: column;
  gap: var(--s-5);
  width: min(560px, 100%);
  padding: var(--s-6);
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  box-shadow: var(--shadow-1);
}

header {
  display: flex;
  align-items: center;
  gap: var(--s-3);
}

.badge {
  display: grid;
  place-items: center;
  width: 44px;
  height: 44px;
  flex-shrink: 0;
  background: var(--surface-2);
  border-radius: var(--r-md);
  color: var(--text-2);
}

.meta {
  min-width: 0;
}

h1 {
  font-size: var(--t-lg);
  overflow-wrap: anywhere;
}

.expiry {
  color: var(--text-3);
  font-size: var(--t-xs);
}

.state {
  padding: var(--s-6) 0;
  color: var(--text-3);
  font-size: var(--t-sm);
  text-align: center;
}

.state.error {
  color: var(--danger);
}

.gate {
  display: flex;
  flex-direction: column;
  gap: var(--s-4);
}

.hint {
  color: var(--text-2);
  font-size: var(--t-sm);
}

.single {
  display: flex;
  flex-direction: column;
  gap: var(--s-4);
}

.primary {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: var(--s-2);
  height: 40px;
  background: var(--text);
  color: var(--bg);
  border-radius: var(--r-md);
  font-size: var(--t-sm);
  font-weight: 500;
  text-decoration: none;
}

.browser {
  display: flex;
  flex-direction: column;
  gap: var(--s-2);
}

.up {
  display: inline-flex;
  align-items: center;
  gap: var(--s-1);
  align-self: flex-start;
  padding: var(--s-1) var(--s-2);
  border-radius: var(--r-sm);
  color: var(--text-2);
  font-size: var(--t-sm);
}

.up:hover {
  background: var(--surface-2);
  color: var(--text);
}

.list {
  display: flex;
  flex-direction: column;
  margin: 0;
  padding: 0;
  list-style: none;
}

.item {
  display: flex;
  align-items: center;
  gap: var(--s-3);
  width: 100%;
  min-height: 44px;
  padding: 0 var(--s-2);
  border-radius: var(--r-sm);
  text-align: left;
}

.item:hover {
  background: var(--surface-2);
}

.icon {
  flex-shrink: 0;
  color: var(--text-3);
}

.name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--t-sm);
}

.size {
  color: var(--text-3);
  font-size: var(--t-xs);
}

.footer {
  color: var(--text-3);
  font-size: var(--t-xs);
}

@media (max-width: 640px) {
  .card {
    padding: var(--s-5);
  }
}
</style>
