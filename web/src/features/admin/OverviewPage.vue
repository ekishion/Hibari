<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api } from '@/api/client'
import type { AuditEvent, Quota, Share } from '@/api/types'
import { session } from '@/composables/useSession'
import { formatBytes, formatStamp } from '@/utils/format'
import { eventLabel } from './auditLabels'

const quota = ref<Quota | null>(null)
const shares = ref<Share[]>([])
const events = ref<AuditEvent[]>([])
const loading = ref(true)
const error = ref('')

const backendLabel = computed(() => (session.state.backend === 'baidu' ? '百度网盘青春版' : '本地磁盘'))

const usedPercent = computed(() => {
  const q = quota.value
  if (!q || q.total <= 0) return 0
  return Math.min(100, Math.round((q.used / q.total) * 100))
})

const totalUses = computed(() => shares.value.reduce((sum, s) => sum + s.uses, 0))

// 三个请求互不依赖，任一失败只影响对应卡片，不整页报错。
async function load() {
  loading.value = true
  error.value = ''
  const results = await Promise.allSettled([
    session.state.caps.quota ? api.quota() : Promise.resolve(null),
    api.listShares(),
    api.audit(8),
  ])
  const [q, s, a] = results
  if (q.status === 'fulfilled') quota.value = q.value
  if (s.status === 'fulfilled') shares.value = s.value.shares ?? []
  if (a.status === 'fulfilled') events.value = a.value.events ?? []
  if (results.every((r) => r.status === 'rejected')) {
    error.value = '加载失败，请检查服务状态'
  }
  loading.value = false
}

onMounted(load)
</script>

<template>
  <div class="page">
    <header>
      <h1>概览</h1>
      <p class="sub">当前部署的存储状态与最近活动。</p>
    </header>

    <p v-if="loading" class="state">加载中…</p>
    <p v-else-if="error" class="state error">{{ error }}</p>

    <template v-else>
      <div class="stats">
        <section class="stat">
          <h2>存储用量</h2>
          <p v-if="quota && quota.total > 0" class="value tnum">
            {{ formatBytes(quota.used) }}
            <span class="of">/ {{ formatBytes(quota.total) }}</span>
          </p>
          <p v-else-if="quota" class="value tnum">{{ formatBytes(quota.used) }}</p>
          <p v-else class="value dim">未知</p>
          <div v-if="quota && quota.total > 0" class="bar" :style="{ '--used': `${usedPercent}%` }" />
          <p class="meta">
            {{ quota && quota.total > 0 ? `剩余 ${formatBytes(quota.free)}` : '后端未设容量上限' }}
          </p>
        </section>

        <section class="stat">
          <h2>活跃分享</h2>
          <p class="value tnum">{{ shares.length }}<span class="of"> 条</span></p>
          <p class="meta">累计被访问 {{ totalUses }} 次</p>
        </section>

        <section class="stat">
          <h2>运行环境</h2>
          <p class="value">{{ backendLabel }}</p>
          <p class="meta tnum">版本 {{ session.state.version }}</p>
        </section>
      </div>

      <section class="card">
        <div class="card-head">
          <h2>最近操作</h2>
          <RouterLink to="/audit" class="more">查看全部</RouterLink>
        </div>
        <p v-if="events.length === 0" class="empty">暂无记录。</p>
        <ul v-else class="events">
          <li v-for="(e, i) in events" :key="i">
            <span class="when tnum">{{ formatStamp(e.created_at) }}</span>
            <span class="what">{{ eventLabel(e.event) }}</span>
            <span class="target" :title="e.path">{{ e.path || '—' }}</span>
          </li>
        </ul>
      </section>
    </template>
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

h2 {
  font-size: var(--t-sm);
  color: var(--text-2);
  font-weight: 500;
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

.stats {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: var(--s-3);
}

.stat {
  display: flex;
  flex-direction: column;
  gap: var(--s-2);
  padding: var(--s-4) var(--s-5);
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
}

.value {
  font-size: var(--t-xl);
  font-weight: 600;
  letter-spacing: -0.02em;
}

.value.dim {
  color: var(--text-3);
}

.of {
  color: var(--text-3);
  font-size: var(--t-sm);
  font-weight: 400;
}

.bar {
  height: 4px;
  background: var(--border);
  border-radius: var(--r-full);
  overflow: hidden;
}

.bar::before {
  content: '';
  display: block;
  width: var(--used);
  height: 100%;
  background: var(--accent);
}

.meta {
  color: var(--text-3);
  font-size: var(--t-xs);
}

.card {
  display: flex;
  flex-direction: column;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  overflow: hidden;
}

.card-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: var(--s-3) var(--s-4);
  border-bottom: 1px solid var(--border);
}

.more {
  font-size: var(--t-xs);
}

.empty {
  padding: var(--s-8) var(--s-4);
  color: var(--text-3);
  font-size: var(--t-sm);
  text-align: center;
}

.events {
  margin: 0;
  padding: 0;
  list-style: none;
}

.events li {
  display: grid;
  grid-template-columns: 148px 88px 1fr;
  gap: var(--s-3);
  padding: var(--s-2) var(--s-4);
  border-bottom: 1px solid var(--border);
  font-size: var(--t-sm);
}

.events li:last-child {
  border-bottom: none;
}

.when,
.target {
  color: var(--text-3);
}

.target {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

@media (max-width: 640px) {
  .stats {
    grid-template-columns: 1fr;
  }

  .events li {
    grid-template-columns: 1fr auto;
    gap: var(--s-1);
  }

  /* 窄屏把对象路径换到第二行整行展示。 */
  .target {
    grid-column: 1 / -1;
  }
}
</style>
