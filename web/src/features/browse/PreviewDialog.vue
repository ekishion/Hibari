<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api } from '@/api/client'
import { previewKind } from '@/utils/format'
import Dialog from '@/ui/Dialog.vue'

const props = defineProps<{ path: string; name: string }>()
const open = defineModel<boolean>('open', { default: false })

const kind = computed(() => previewKind(props.name))
const src = computed(() => api.rawURL(props.path))

/** 文本预览最多读取的字节数。超出部分丢弃，避免把整份大文件读进内存。 */
const MAX_TEXT_BYTES = 512 * 1024

const text = ref('')
const loading = ref(false)
const failed = ref(false)
const truncated = ref(false)

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

// 文本预览只在打开时拉取，避免打开大文件浪费流量。
// 父级用 v-if 控制挂载，因此挂载时 open 已为 true，必须立即执行一次。
watch(
  open,
  async (isOpen) => {
    if (!isOpen || kind.value !== 'text') return
    text.value = ''
    failed.value = false
    truncated.value = false
    loading.value = true
    try {
      const res = await fetch(src.value, { credentials: 'same-origin' })
      if (!res.ok) throw new Error(String(res.status))
      text.value = await readCapped(res, MAX_TEXT_BYTES)
    } catch {
      failed.value = true
    } finally {
      loading.value = false
    }
  },
  { immediate: true },
)
</script>

<template>
  <Dialog v-model:open="open" :title="name" width="min(920px, 92vw)">
    <div class="stage">
      <img v-if="kind === 'image'" :src="src" :alt="name" />
      <video v-else-if="kind === 'video'" :src="src" controls playsinline />
      <audio v-else-if="kind === 'audio'" :src="src" controls />
      <pre v-else-if="kind === 'text'" class="text">{{ loading ? '加载中…' : failed ? '读取失败' : text }}</pre>
      <p v-else class="unsupported">该类型不支持在线预览，请下载后查看。</p>
    </div>

    <p v-if="truncated" class="hint">文件较大，仅显示前 512 KB。</p>

    <div class="actions">
      <a :href="api.rawURL(path, true)" class="download">下载文件</a>
    </div>
  </Dialog>
</template>

<style scoped>
.stage {
  display: grid;
  place-items: center;
  min-height: 200px;
  max-height: 70dvh;
  overflow: auto;
  background: var(--surface-2);
  border-radius: var(--r-md);
}

img,
video {
  max-height: 70dvh;
  max-width: 100%;
  object-fit: contain;
}

audio {
  width: min(480px, 100%);
  margin: var(--s-8) 0;
}

.text {
  width: 100%;
  max-height: 70dvh;
  padding: var(--s-4);
  overflow: auto;
  font-size: var(--t-sm);
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-word;
}

.unsupported {
  padding: var(--s-10);
  color: var(--text-3);
  font-size: var(--t-sm);
}

.hint {
  margin: 0;
  color: var(--text-3);
  font-size: var(--t-xs);
}

.actions {
  display: flex;
  justify-content: flex-end;
}

.download {
  display: inline-flex;
  align-items: center;
  height: 36px;
  padding: 0 var(--s-4);
  background: var(--text);
  color: var(--bg);
  border-radius: var(--r-md);
  font-size: var(--t-sm);
  font-weight: 500;
  text-decoration: none;
}
</style>
