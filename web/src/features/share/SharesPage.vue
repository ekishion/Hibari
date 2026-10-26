<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '@/api/client'
import type { Share } from '@/api/types'
import { formatTime } from '@/utils/format'
import { toast } from '@/ui/toast'
import Button from '@/ui/Button.vue'
import Dialog from '@/ui/Dialog.vue'
import { Link2, Trash2 } from 'lucide-vue-next'

const shares = ref<Share[]>([])
const loading = ref(true)
const error = ref('')
const pending = ref<Share | null>(null)
const revoking = ref(false)

async function load() {
  loading.value = true
  error.value = ''
  try {
    const res = await api.listShares()
    shares.value = res.shares ?? []
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
  } finally {
    loading.value = false
  }
}

async function revoke() {
  if (!pending.value) return
  revoking.value = true
  try {
    await api.revokeShare(pending.value.token)
    toast.success('已撤销分享')
    pending.value = null
    await load()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '撤销失败')
  } finally {
    revoking.value = false
  }
}

async function copyLink(share: Share) {
  const url = `${location.origin}/api/share/${share.token}`
  try {
    await navigator.clipboard.writeText(url)
    toast.success('链接已复制')
  } catch {
    toast.error('复制失败，请手动复制')
  }
}

onMounted(load)
</script>

<template>
  <div class="page">
    <header class="head">
      <div>
        <h1>分享</h1>
        <p class="sub">所有未过期的分享链接。撤销后立即失效。</p>
      </div>
    </header>

    <p v-if="loading" class="state">加载中…</p>
    <p v-else-if="error" class="state error">{{ error }}</p>
    <p v-else-if="shares.length === 0" class="state">还没有创建过分享。</p>

    <ul v-else class="list">
      <li v-for="share in shares" :key="share.token" class="item">
        <div class="main">
          <div class="name">
            {{ share.name }}
            <span class="tag">{{ share.kind === 'dir' ? '目录' : '文件' }}</span>
            <span v-if="share.has_password" class="tag">提取码</span>
          </div>
          <div class="detail tnum">
            创建 {{ formatTime(share.created_at) }} · 到期 {{ formatTime(share.expires_at) }}
            <template v-if="share.max_uses > 0"> · {{ share.uses }}/{{ share.max_uses }} 次</template>
          </div>
        </div>
        <div class="actions">
          <Button size="sm" variant="ghost" @click="copyLink(share)">
            <Link2 :size="14" />
            复制链接
          </Button>
          <Button size="sm" variant="danger" @click="pending = share">
            <Trash2 :size="14" />
            撤销
          </Button>
        </div>
      </li>
    </ul>

    <Dialog :open="pending !== null" title="撤销分享" @update:open="(v) => !v && (pending = null)">
      <p>撤销后该链接立即失效，已发出的链接无法再访问。</p>
      <div class="dialog-actions">
        <Button variant="ghost" @click="pending = null">取消</Button>
        <Button variant="danger" :loading="revoking" @click="revoke">撤销</Button>
      </div>
    </Dialog>
  </div>
</template>

<style scoped>
.page {
  display: flex;
  flex-direction: column;
  gap: var(--s-4);
  max-width: var(--content-max);
  margin: 0 auto;
  padding: var(--s-6) var(--s-4) var(--s-12);
}

h1 {
  font-size: var(--t-xl);
}

.sub {
  color: var(--text-2);
  font-size: var(--t-sm);
}

.state {
  padding: var(--s-12) 0;
  color: var(--text-3);
  font-size: var(--t-sm);
  text-align: center;
}

.state.error {
  color: var(--danger);
}

.list {
  display: flex;
  flex-direction: column;
  margin: 0;
  padding: 0;
  list-style: none;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  overflow: hidden;
}

.item {
  display: flex;
  align-items: center;
  gap: var(--s-4);
  padding: var(--s-3) var(--s-4);
  border-bottom: 1px solid var(--border);
}

.item:last-child {
  border-bottom: none;
}

.main {
  flex: 1;
  min-width: 0;
}

.name {
  display: flex;
  align-items: center;
  gap: var(--s-2);
  font-weight: 500;
  overflow-wrap: anywhere;
}

.tag {
  padding: 1px var(--s-2);
  background: var(--surface-2);
  border-radius: var(--r-sm);
  color: var(--text-3);
  font-size: var(--t-xs);
  font-weight: 400;
}

.detail {
  color: var(--text-3);
  font-size: var(--t-xs);
}

.actions {
  display: flex;
  gap: var(--s-2);
  flex-shrink: 0;
}

.dialog-actions {
  display: flex;
  justify-content: flex-end;
  gap: var(--s-2);
}

@media (max-width: 640px) {
  .item {
    flex-direction: column;
    align-items: stretch;
    gap: var(--s-2);
  }

  .actions {
    justify-content: flex-end;
  }
}
</style>
