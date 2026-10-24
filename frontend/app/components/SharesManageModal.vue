<script setup lang="ts">
// 活跃分享管理弹窗：等价迁移自 shares_manage.html + share_modal.js 管理段
import { fileIconMeta, formatTime } from '~/utils/format'

const { sharesManageOpen, closeSharesManage } = useShareModals()
const { showToast } = useToast()

interface ShareItem {
  token: string
  file_name?: string
  dir_name?: string
  file_path?: string
  dir?: string
  has_password: boolean
  expires_at: number
  created_at: number
  max_uses?: number
  uses?: number
  url: string
  _type?: "file" | "dir"
}

const visible = computed(() => sharesManageOpen.value)
const loading = ref(false)
const files = ref<ShareItem[]>([])
const dirs = ref<ShareItem[]>([])
const currentTab = ref<"all" | "files" | "dirs">("all")
const keyword = ref("")
const revoking = ref("")

watch(visible, (open) => {
  if (open) {
    keyword.value = ""
    currentTab.value = "all"
    fetchSharesList()
  }
})

async function fetchSharesList() {
  loading.value = true
  try {
    const resp = await apiFetch("/api/shares", { method: "GET" })
    if (!resp.ok) {
      const text = await resp.text()
      throw new Error(await parseAPIError(resp, text))
    }
    const data = await resp.json()
    files.value = Array.isArray(data.files) ? data.files : []
    dirs.value = Array.isArray(data.dirs) ? data.dirs : []
  } catch (err) {
    showToast("获取分享列表失败：" + ((err as Error).message || "未知错误"), "error")
  } finally {
    loading.value = false
  }
}

const items = computed<ShareItem[]>(() => {
  let list: ShareItem[] = []
  if (currentTab.value === "all") {
    list = [
      ...files.value.map((f) => ({ ...f, _type: "file" as const })),
      ...dirs.value.map((d) => ({ ...d, _type: "dir" as const })),
    ]
    list.sort((a, b) => (b.created_at || 0) - (a.created_at || 0))
  } else if (currentTab.value === "files") {
    list = files.value.map((f) => ({ ...f, _type: "file" as const }))
  } else {
    list = dirs.value.map((d) => ({ ...d, _type: "dir" as const }))
  }
  const kw = keyword.value.toLowerCase().trim()
  if (kw) {
    list = list.filter((it) => {
      const name = (it.file_name || it.dir_name || "").toLowerCase()
      const path = (it.file_path || it.dir || "").toLowerCase()
      return name.includes(kw) || path.includes(kw)
    })
  }
  return list
})

function formatRemaining(expiresAtTs: number): string {
  const diff = Number(expiresAtTs) - Math.floor(Date.now() / 1000)
  if (diff <= 0) return "已过期"
  const days = Math.floor(diff / 86400)
  if (days >= 2) return days + " 天后到期"
  const hours = Math.floor(diff / 3600)
  if (hours >= 1) return hours + " 小时后到期"
  return Math.max(1, Math.floor(diff / 60)) + " 分钟后到期"
}

function usesTag(item: ShareItem): string {
  if ((item.max_uses || 0) > 0) return `已用 ${item.uses || 0} / 限 ${item.max_uses} 次`
  return (item.uses || 0) > 0 ? `已用 ${item.uses} 次` : "不限次"
}

async function copyItemUrl(item: ShareItem) {
  const fullUrl = window.location.origin + item.url
  ;(await copyText(fullUrl)) ? showToast("分享链接已复制", "success") : showToast("复制失败，请手动复制", "error")
}

async function revoke(item: ShareItem) {
  revoking.value = item.token
  try {
    const resp = await apiFetch("/api/shares/revoke", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ token: item.token }),
    })
    if (!resp.ok) {
      const text = await resp.text()
      throw new Error(await parseAPIError(resp, text))
    }
    showToast("已成功撤销该分享链接", "success")
    files.value = files.value.filter((f) => f.token !== item.token)
    dirs.value = dirs.value.filter((d) => d.token !== item.token)
  } catch (err) {
    showToast("撤销失败：" + ((err as Error).message || "未知错误"), "error")
  } finally {
    revoking.value = ""
  }
}
</script>

<template>
  <Teleport to="body">
    <div id="shares-manage-overlay" class="modal-overlay" :hidden="!visible" :aria-hidden="!visible" @click.self="closeSharesManage">
      <div class="shares-manage-card" role="dialog" aria-modal="true" aria-labelledby="shares-manage-title">
        <div class="shares-manage-header">
          <div class="share-card-title-group">
            <span class="share-mark"><AppIcon name="share-2" :size="17" /></span>
            <div>
              <h2 id="shares-manage-title">活跃分享管理</h2>
              <p class="shares-manage-subtitle">查看所有已生效的外链，支持随时手动撤销</p>
            </div>
          </div>
          <button type="button" id="shares-manage-close" class="modal-close" aria-label="关闭分享管理窗口" @click="closeSharesManage">
            <AppIcon name="x" :size="16" />
          </button>
        </div>

        <div class="shares-manage-tabs" role="tablist">
          <button type="button" class="shares-tab" :class="{ 'is-active': currentTab === 'all' }" role="tab" :aria-selected="currentTab === 'all'" @click="currentTab = 'all'">
            全部 ({{ files.length + dirs.length }})
          </button>
          <button type="button" class="shares-tab" :class="{ 'is-active': currentTab === 'files' }" role="tab" :aria-selected="currentTab === 'files'" @click="currentTab = 'files'">
            文件 ({{ files.length }})
          </button>
          <button type="button" class="shares-tab" :class="{ 'is-active': currentTab === 'dirs' }" role="tab" :aria-selected="currentTab === 'dirs'" @click="currentTab = 'dirs'">
            文件夹 ({{ dirs.length }})
          </button>
        </div>

        <div class="shares-manage-toolbar">
          <div class="search-box">
            <span><AppIcon name="search" :size="15" /></span>
            <input v-model="keyword" type="search" placeholder="筛选文件或目录名称…" autocomplete="off" aria-label="筛选分享">
          </div>
          <button type="button" class="btn btn-ghost btn-sm" aria-label="刷新列表" title="刷新分享列表" @click="fetchSharesList">
            <span class="btn-icon"><AppIcon name="refresh-cw" :size="14" /></span>
            <span>刷新</span>
          </button>
        </div>

        <div class="shares-manage-list" role="region" aria-label="分享列表">
          <div v-if="loading" class="shares-loading">
            <span class="btn-spinner"><AppIcon name="loader" :size="16" /></span>
            <span>正在加载活跃分享…</span>
          </div>

          <div v-else-if="!items.length" class="shares-empty">
            <span class="shares-empty-icon"><AppIcon :name="keyword ? 'circle-alert' : 'inbox'" :size="24" /></span>
            <p class="shares-empty-text">{{ keyword ? "未找到匹配的分享链接" : "暂无活跃的分享链接" }}</p>
            <p class="shares-empty-sub">{{ keyword ? "请尝试更换关键词搜索" : "在文件列表中点击“分享”即可创建新外链" }}</p>
          </div>

          <template v-else>
            <div v-for="item in items" :key="item.token" class="shares-item-row" :data-token="item.token">
              <div class="shares-item-icon" :class="{ 'is-dir': item._type === 'dir' }">
                <AppIcon :name="item._type === 'dir' ? 'folder' : fileIconMeta(item.file_name || '', false).name" :size="18" />
              </div>
              <div class="shares-item-info">
                <div class="shares-item-title-row">
                  <span class="shares-item-title">{{ item._type === "dir" ? (item.dir_name || "未命名目录") : (item.file_name || "未命名文件") }}</span>
                </div>
                <div class="shares-item-path">{{ item._type === "dir" ? item.dir : item.file_path }}</div>
                <div class="shares-item-tags">
                  <span class="shares-tag">{{ item._type === "dir" ? "目录分享" : "文件外链" }}</span>
                  <span class="shares-tag" :class="{ 'is-locked': item.has_password }">{{ item.has_password ? "提取码保护" : "公开访问" }}</span>
                  <span v-if="item.expires_at" class="shares-tag" :title="'到期时间：' + formatTime(item.expires_at)">{{ formatRemaining(item.expires_at) }}</span>
                  <span v-if="item._type !== 'dir'" class="shares-tag">{{ usesTag(item) }}</span>
                </div>
              </div>
              <div class="shares-item-actions">
                <button type="button" class="btn btn-secondary btn-sm" title="复制分享链接" @click="copyItemUrl(item)">
                  <span class="btn-icon"><AppIcon name="copy" :size="14" /></span>
                  <span>复制</span>
                </button>
                <button
                  type="button"
                  class="btn btn-ghost btn-sm btn-revoke"
                  title="撤销并废除该分享链接"
                  :disabled="revoking === item.token"
                  @click="revoke(item)"
                >
                  <span class="btn-spinner"><AppIcon name="loader" :size="14" /></span>
                  <span class="btn-label">{{ revoking === item.token ? "撤销中…" : "撤销" }}</span>
                </button>
              </div>
            </div>
          </template>
        </div>
      </div>
    </div>
  </Teleport>
</template>
