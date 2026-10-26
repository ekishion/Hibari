<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '@/api/client'
import type { AuditEvent } from '@/api/types'
import { formatStamp } from '@/utils/format'
import { eventLabel } from './auditLabels'
import Button from '@/ui/Button.vue'

const events = ref<AuditEvent[]>([])
const loading = ref(true)
const error = ref('')
const limit = ref(100)

async function load() {
  loading.value = true
  error.value = ''
  try {
    const res = await api.audit(limit.value)
    events.value = res.events ?? []
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="page">
    <header class="head">
      <div>
        <h1>审计</h1>
        <p class="sub">最近的敏感操作记录。IP 已掩码，保留 30 天。</p>
      </div>
      <Button size="sm" variant="ghost" :loading="loading" @click="load">刷新</Button>
    </header>

    <p v-if="loading && events.length === 0" class="state">加载中…</p>
    <p v-else-if="error" class="state error">{{ error }}</p>
    <p v-else-if="events.length === 0" class="state">暂无记录。</p>

    <div v-else class="table">
      <div class="row head-row">
        <span>时间</span>
        <span>事件</span>
        <span>对象</span>
        <span>来源</span>
      </div>
      <div v-for="(e, i) in events" :key="i" class="row">
        <span class="tnum dim">{{ formatStamp(e.created_at) }}</span>
        <span>{{ eventLabel(e.event) }}</span>
        <span class="dim ellipsis" :title="e.path">{{ e.path || '—' }}</span>
        <span class="dim ellipsis" :title="e.user_agent">{{ e.client_ip }}</span>
      </div>
    </div>
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

.head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--s-4);
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

.table {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  overflow: hidden;
}

.row {
  display: grid;
  grid-template-columns: 148px 96px 1fr 120px;
  gap: var(--s-3);
  padding: var(--s-2) var(--s-4);
  border-bottom: 1px solid var(--border);
  font-size: var(--t-sm);
}

.row:last-child {
  border-bottom: none;
}

.head-row {
  background: var(--surface-2);
  color: var(--text-3);
  font-size: var(--t-xs);
  font-weight: 500;
}

.dim {
  color: var(--text-3);
}

.ellipsis {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

@media (max-width: 640px) {
  .row {
    grid-template-columns: 1fr;
    gap: var(--s-1);
  }

  .head-row {
    display: none;
  }
}
</style>
