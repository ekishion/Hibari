<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api } from '@/api/client'
import type { Entry } from '@/api/types'
import { formatBytes, formatTime, previewKind } from '@/utils/format'
import Dialog from '@/ui/Dialog.vue'
import { ChevronLeft, ChevronRight, Download, FileQuestion } from 'lucide-vue-next'

/** 只接收可预览的同级文件，由父级从当前筛选结果里挑好。 */
const props = defineProps<{ entry: Entry; siblings: Entry[] }>()
const open = defineModel<boolean>('open', { default: false })

const index = ref(Math.max(0, props.siblings.findIndex((e) => e.path === props.entry.path)))
const current = computed(() => props.siblings[index.value] ?? props.entry)
/** 递增后媒体元素的 key 变化，浏览器会重建并重新发起请求。 */
const reload = ref(0)

const kind = computed(() => previewKind(current.value.name))
const src = computed(() => api.rawURL(current.value.path))
/** 重试时换一个地址，否则浏览器会直接用缓存里的失败结果。 */
const mediaSrc = computed(() => (reload.value ? `${src.value}&r=${reload.value}` : src.value))
const downloadHref = computed(() => api.rawURL(current.value.path, true))

const hasPrev = computed(() => index.value > 0)
const hasNext = computed(() => index.value < props.siblings.length - 1)

// 父级复用同一实例切换目标文件（关闭后再点另一个），索引必须跟着走，
// 否则 current 仍指向 siblings[index] 里的上一个文件。
watch(
  () => props.entry.path,
  (path) => {
    const i = props.siblings.findIndex((e) => e.path === path)
    if (i >= 0) index.value = i
  },
)

/** 文本预览最多读取的字节数。超出部分丢弃，避免把整份大文件读进内存。 */
const MAX_TEXT_BYTES = 512 * 1024

const text = ref('')
const loading = ref(false)
const failed = ref(false)
const errorMsg = ref('')
const truncated = ref(false)
/** 文件已不存在。此时不给下载入口：文件都没了，下载只会再失败一次。 */
const gone = ref(false)

/** 只认「确实不在了」这一类终态，其它失败仍保留下载入口。 */
function isGone(status: number, body: { error?: { code?: string } } | null) {
  return status === 404 || status === 410 || body?.error?.code === 'not_found'
}

function step(delta: number) {
  const next = index.value + delta
  if (next < 0 || next >= props.siblings.length) return
  index.value = next
}

/** 流式读取前 limit 个字节后主动断开，不等待整个响应体。 */
async function readCapped(res: Response, limit: number): Promise<string> {
  const reader = res.body?.getReader()
  if (!reader) return res.text()

  const chunks: Uint8Array[] = []
  let total = 0
  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    chunks.push(value)
    total += value.byteLength
    if (total >= limit) {
      await reader.cancel()
      truncated.value = true
      break
    }
  }

  const buf = new Uint8Array(Math.min(total, limit))
  let offset = 0
  for (const chunk of chunks) {
    const take = Math.min(chunk.byteLength, buf.length - offset)
    if (take <= 0) break
    buf.set(chunk.subarray(0, take), offset)
    offset += take
  }
  return new TextDecoder().decode(buf)
}

/**
 * 媒体元素加载失败时事件不带状态码，单独探一次拿真实原因。
 * 只取 1 字节，且不跟随重定向——跟随会把请求打到上游域名，被 connect-src 拦下。
 * no-store：raw 接口带 max-age=60，走缓存会把删除前的旧响应当成本次结果。
 */
async function probeError() {
  errorMsg.value = ''
  gone.value = false
  try {
    const res = await fetch(src.value, {
      credentials: 'same-origin',
      redirect: 'manual',
      cache: 'no-store',
      headers: { Range: 'bytes=0-0' },
    })
    // 重定向说明直链成功、服务端能取到字节，问题在浏览器侧的编码支持。
    if (res.type === 'opaqueredirect' || res.ok) {
      errorMsg.value =
        kind.value === 'image' ? '浏览器无法显示该图片。' : '浏览器无法播放该格式，请下载后查看。'
      return
    }
    const body = await res.json().catch(() => null)
    gone.value = isGone(res.status, body)
    errorMsg.value = body?.error?.message ?? `读取失败（${res.status}）`
  } catch {
    errorMsg.value = '网络中断，请检查连接后重试。'
  }
}

async function loadText() {
  loading.value = true
  failed.value = false
  errorMsg.value = ''
  gone.value = false
  truncated.value = false
  text.value = ''
  try {
    const res = await fetch(src.value, { credentials: 'same-origin' })
    if (!res.ok) {
      const body = await res.json().catch(() => null)
      gone.value = isGone(res.status, body)
      throw new Error(body?.error?.message ?? `读取失败（${res.status}）`)
    }
    text.value = await readCapped(res, MAX_TEXT_BYTES)
  } catch (e) {
    failed.value = true
    errorMsg.value = e instanceof Error ? e.message : '读取失败'
  } finally {
    loading.value = false
  }
}

function onMediaError() {
  loading.value = false
  failed.value = true
  probeError()
}

function retry() {
  if (kind.value === 'text') {
    loadText()
    return
  }
  failed.value = false
  loading.value = true
  reload.value++
}

// 打开或切换到另一个文件时重置状态并重新拉取。
// 父级用 v-if 控制挂载，因此挂载时 open 已为 true，必须立即执行一次。
watch(
  [open, index],
  () => {
    failed.value = false
    errorMsg.value = ''
    gone.value = false
    truncated.value = false
    text.value = ''
    if (!open.value) {
      loading.value = false
      return
    }
    if (kind.value === 'text') {
      loadText()
      return
    }
    loading.value = kind.value !== 'none'
  },
  { immediate: true },
)
</script>

<template>
  <Dialog v-model:open="open" :title="current.name" width="min(1000px, 94vw)" height="86dvh" flush>
    <template #subtitle>
      <p class="meta tnum">
        {{ current.is_dir ? '目录' : formatBytes(current.size) }}
        <span aria-hidden="true">·</span>
        {{ formatTime(current.mod_time) }}
      </p>
    </template>

    <div class="stage">
      <img
        v-if="kind === 'image' && !failed"
        :key="`i-${reload}-${current.path}`"
        :src="mediaSrc"
        :alt="current.name"
        @load="loading = false"
        @error="onMediaError"
      />
      <video
        v-else-if="kind === 'video' && !failed"
        :key="`v-${reload}-${current.path}`"
        :src="mediaSrc"
        controls
        playsinline
        @loadeddata="loading = false"
        @error="onMediaError"
      />
      <audio
        v-else-if="kind === 'audio' && !failed"
        :key="`a-${reload}-${current.path}`"
        :src="mediaSrc"
        controls
        @loadeddata="loading = false"
        @error="onMediaError"
      />
      <pre v-else-if="kind === 'text' && !failed" class="text">{{ loading ? '' : text }}</pre>

      <div v-if="failed" class="notice" role="alert">
        <FileQuestion :size="28" aria-hidden="true" />
        <p class="notice-msg">{{ errorMsg || '无法加载该文件' }}</p>
      </div>
      <p v-else-if="loading" class="notice loading" role="status">加载中…</p>
      <p v-else-if="kind === 'none'" class="notice">该类型不支持在线预览，请下载后查看。</p>
    </div>

    <template #footer>
      <div class="bar">
        <p v-if="truncated" class="hint">文件较大，仅显示前 512 KB。</p>
        <div v-else-if="siblings.length > 1" class="pager">
          <button type="button" :disabled="!hasPrev" aria-label="上一个" @click="step(-1)">
            <ChevronLeft :size="16" />
          </button>
          <span class="tnum">{{ index + 1 }} / {{ siblings.length }}</span>
          <button type="button" :disabled="!hasNext" aria-label="下一个" @click="step(1)">
            <ChevronRight :size="16" />
          </button>
        </div>
        <span v-else />

        <!-- 下载只留这一处；文件已不存在时连同重试一起撤掉，两个都必然失败。 -->
        <div v-if="!gone" class="tools">
          <button v-if="failed" type="button" class="tool" @click="retry">重试</button>
          <a :href="downloadHref" class="tool primary">
            <Download :size="15" aria-hidden="true" />
            下载
          </a>
        </div>
      </div>
    </template>
  </Dialog>
</template>

<style scoped>
.meta {
  margin-top: var(--s-1);
  color: var(--text-3);
  font-size: var(--t-xs);
}

.stage {
  position: relative;
  display: grid;
  place-items: center;
  flex: 1;
  min-height: 0;
  padding: var(--s-3);
  background: var(--surface-2);
  overflow: hidden;
}

img,
video {
  max-width: 100%;
  max-height: 100%;
  object-fit: contain;
}

audio {
  width: min(480px, 100%);
}

.text {
  width: 100%;
  height: 100%;
  padding: var(--s-4);
  background: var(--surface);
  border-radius: var(--r-sm);
  overflow: auto;
  font-size: var(--t-sm);
  line-height: 1.65;
  white-space: pre-wrap;
  word-break: break-word;
  tab-size: 2;
}

.notice {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--s-2);
  padding: var(--s-8) var(--s-4);
  color: var(--text-2);
  font-size: var(--t-sm);
  text-align: center;
}

.notice svg {
  color: var(--text-3);
}

.notice.loading {
  position: absolute;
  inset: 0;
  justify-content: center;
  color: var(--text-3);
}

.notice-msg {
  max-width: 46ch;
  overflow-wrap: anywhere;
}

.bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--s-4);
}

.hint {
  color: var(--text-3);
  font-size: var(--t-xs);
}

.pager {
  display: flex;
  align-items: center;
  gap: var(--s-2);
  color: var(--text-2);
  font-size: var(--t-sm);
}

.pager button {
  display: grid;
  place-items: center;
  width: 28px;
  height: 28px;
  border-radius: var(--r-sm);
  color: var(--text-2);
}

.pager button:hover:not(:disabled) {
  background: var(--surface-2);
  color: var(--text);
}

.pager button:disabled {
  opacity: 0.35;
  cursor: not-allowed;
}

.tools {
  display: flex;
  align-items: center;
  gap: var(--s-2);
  margin-left: auto;
}

.tool {
  display: inline-flex;
  align-items: center;
  gap: var(--s-1);
  height: 32px;
  padding: 0 var(--s-3);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  color: var(--text);
  font-size: var(--t-sm);
  text-decoration: none;
}

.tool:hover {
  background: var(--surface-2);
  border-color: var(--border-strong);
}

.tool.primary {
  background: var(--text);
  border-color: var(--text);
  color: var(--bg);
}

.tool.primary:hover {
  opacity: 0.88;
}

@media (max-width: 640px) {
  .stage {
    padding: var(--s-2);
  }

  .text {
    padding: var(--s-3);
  }

  /* 触控目标不小于 44px；空间不够时底栏换行，不挤压按钮。 */
  .bar {
    flex-wrap: wrap;
    row-gap: var(--s-2);
  }

  .pager {
    gap: var(--s-1);
  }

  .pager button {
    width: 44px;
    height: 44px;
  }

  .tool {
    height: 44px;
    padding: 0 var(--s-4);
  }
}
</style>
