<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, ApiFailure } from '@/api/client'
import type { Entry, Quota } from '@/api/types'
import { session } from '@/composables/useSession'
import { breadcrumbs, formatBytes, parentOf, previewKind } from '@/utils/format'
import { toast } from '@/ui/toast'
import Button from '@/ui/Button.vue'
import Dialog from '@/ui/Dialog.vue'
import FileRow from './FileRow.vue'
import NameDialog from './NameDialog.vue'
import PreviewDialog from './PreviewDialog.vue'
import ShareDialog from './ShareDialog.vue'
import { ChevronLeft, Upload, FolderPlus, Search } from 'lucide-vue-next'

const route = useRoute()
const router = useRouter()

const dir = ref(typeof route.query.dir === 'string' ? route.query.dir : '/')
const entries = ref<Entry[]>([])
const quota = ref<Quota | null>(null)
const loading = ref(false)
const loadError = ref('')
const filter = ref('')
const selected = ref(new Set<string>())

const previewOpen = ref(false)
const previewTarget = ref<Entry | null>(null)
const nameOpen = ref(false)
const nameTarget = ref<Entry | null>(null)
const shareOpen = ref(false)
const shareTarget = ref<Entry | null>(null)
const removeOpen = ref(false)
const removing = ref(false)
const uploadProgress = ref<number | null>(null)
const fileInput = ref<HTMLInputElement | null>(null)

const canWrite = computed(() => session.state.caps.write)
const canUpload = computed(() => session.state.caps.upload)
const trail = computed(() => breadcrumbs(dir.value))
const atRoot = computed(() => dir.value === '/')

const visible = computed(() => {
  const q = filter.value.trim().toLowerCase()
  const list = q ? entries.value.filter((e) => e.name.toLowerCase().includes(q)) : entries.value
  // 目录在前，同类按名称排序，符合文件管理器直觉。
  return [...list].sort((a, b) =>
    a.is_dir !== b.is_dir ? (a.is_dir ? -1 : 1) : a.name.localeCompare(b.name, 'zh-CN'),
  )
})

const selectedEntries = computed(() => entries.value.filter((e) => selected.value.has(e.path)))

/** 预览里可左右切换的范围：当前筛选结果中的文件，顺序与列表一致。 */
const previewables = computed(() => visible.value.filter((e) => !e.is_dir && previewKind(e.name) !== 'none'))

async function load() {
  loading.value = true
  loadError.value = ''
  selected.value = new Set()
  try {
    const res = await api.list(dir.value)
    entries.value = res.entries ?? []
  } catch (e) {
    loadError.value = e instanceof ApiFailure ? e.message : '加载失败'
    entries.value = []
  } finally {
    loading.value = false
  }
}

async function loadQuota() {
  if (!session.state.caps.quota) return
  try {
    quota.value = await api.quota()
  } catch {
    quota.value = null
  }
}

function navigate(path: string) {
  dir.value = path
  router.replace({ query: path === '/' ? {} : { dir: path } })
}

function open(entry: Entry) {
  if (entry.is_dir) {
    navigate(entry.path)
    return
  }
  previewTarget.value = entry
  previewOpen.value = true
}

/** 单击选中（替换现有选择），Ctrl/Cmd 单击由 FileRow 转为 toggle。 */
function select(entry: Entry) {
  selected.value = new Set([entry.path])
}

function toggle(entry: Entry) {
  const next = new Set(selected.value)
  if (next.has(entry.path)) next.delete(entry.path)
  else next.add(entry.path)
  selected.value = next
}

function selectAll() {
  selected.value = new Set(visible.value.map((e) => e.path))
}

function clearSelection() {
  selected.value = new Set()
}

function askRename() {
  if (selectedEntries.value.length !== 1) return
  nameTarget.value = selectedEntries.value[0]
  nameOpen.value = true
}

function askShare() {
  if (selectedEntries.value.length !== 1) return
  shareTarget.value = selectedEntries.value[0]
  shareOpen.value = true
}

async function doRemove() {
  removing.value = true
  try {
    await api.remove(selectedEntries.value.map((e) => e.path))
    toast.success(`已删除 ${selectedEntries.value.length} 项`)
    removeOpen.value = false
    await load()
    await loadQuota()
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '删除失败')
  } finally {
    removing.value = false
  }
}

function pickFiles() {
  fileInput.value?.click()
}

async function onFilesPicked(event: Event) {
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files ?? [])
  input.value = ''
  if (!files.length) return

  for (const file of files) {
    uploadProgress.value = 0
    try {
      await api.upload(dir.value, file, (p) => (uploadProgress.value = p))
      toast.success(`${file.name} 上传完成`)
    } catch (e) {
      toast.error(`${file.name}: ${e instanceof Error ? e.message : '上传失败'}`)
    }
  }
  uploadProgress.value = null
  await load()
  await loadQuota()
}

function onDrop(event: DragEvent) {
  if (!canUpload.value) return
  const files = Array.from(event.dataTransfer?.files ?? [])
  if (!files.length) return
  const input = fileInput.value
  if (!input) return
  const dt = new DataTransfer()
  files.forEach((f) => dt.items.add(f))
  input.files = dt.files
  onFilesPicked({ target: input } as unknown as Event)
}

watch(dir, load)

onMounted(async () => {
  await load()
  await loadQuota()
})
</script>

<template>
  <div class="page">
    <div class="toolbar">
      <div class="crumbs">
        <!-- 有选中时路径让位给计数：同一行同一高度，计数正好落在最左端，
             工具栏不会因此多出一行、把列表推下去。 -->
        <span v-if="selectedEntries.length" class="count">已选 {{ selectedEntries.length }} 项</span>
        <template v-else>
          <button v-if="!atRoot" type="button" class="back" aria-label="返回上级" @click="navigate(parentOf(dir))">
            <ChevronLeft :size="18" />
          </button>
          <nav aria-label="路径">
            <template v-for="(crumb, i) in trail" :key="crumb.path">
              <span v-if="i > 0" class="sep" aria-hidden="true">/</span>
              <button
                type="button"
                class="crumb"
                :aria-current="i === trail.length - 1 ? 'page' : undefined"
                @click="navigate(crumb.path)"
              >
                {{ crumb.name }}
              </button>
            </template>
          </nav>
        </template>
      </div>

      <div class="actions">
        <label class="search">
          <Search :size="16" aria-hidden="true" />
          <input v-model="filter" type="search" placeholder="筛选当前目录" aria-label="筛选文件" />
        </label>

        <!-- 有选中时原地换成批量操作：同一行同一高度，列表不会位移。 -->
        <template v-if="selectedEntries.length">
          <Button size="sm" variant="ghost" @click="clearSelection">取消</Button>
          <Button v-if="selectedEntries.length === 1" size="sm" variant="ghost" @click="askRename">
            重命名
          </Button>
          <Button v-if="selectedEntries.length === 1" size="sm" variant="ghost" @click="askShare">
            分享
          </Button>
          <Button v-if="canWrite" size="sm" variant="danger" @click="removeOpen = true">删除</Button>
        </template>
        <template v-else>
          <Button v-if="canWrite" size="sm" variant="ghost" @click="nameTarget = null; nameOpen = true">
            <FolderPlus :size="16" />
            新建
          </Button>
          <Button v-if="canUpload" size="sm" @click="pickFiles">
            <Upload :size="16" />
            上传
          </Button>
        </template>
        <input v-if="canUpload" ref="fileInput" type="file" multiple hidden @change="onFilesPicked" />
      </div>
    </div>

    <div v-if="quota" class="quota">
      <div v-if="quota.total > 0" class="bar" :style="{ '--used': `${(quota.used / quota.total) * 100}%` }" />
      <span class="tnum">
        {{ quota.total > 0 ? `${formatBytes(quota.used)} / ${formatBytes(quota.total)}` : `已用 ${formatBytes(quota.used)}` }}
      </span>
    </div>

    <div v-if="uploadProgress !== null" class="progress" role="status">
      <div class="fill" :style="{ width: `${uploadProgress}%` }" />
      <span>上传中 {{ uploadProgress }}%</span>
    </div>

    <div class="table" role="table" aria-label="文件列表" @dragover.prevent @drop.prevent="onDrop">
      <div class="head" role="row">
        <div role="columnheader">
          <input
            type="checkbox"
            aria-label="全选"
            :checked="visible.length > 0 && selected.size === visible.length"
            @change="selected.size === visible.length ? clearSelection() : selectAll()"
          />
        </div>
        <div role="columnheader">名称</div>
        <div role="columnheader" class="right">大小</div>
        <div role="columnheader" class="right">修改时间</div>
      </div>

      <p v-if="loading" class="state">加载中…</p>
      <p v-else-if="loadError" class="state error">{{ loadError }}</p>
      <p v-else-if="visible.length === 0" class="state">
        {{ filter ? '没有匹配的项目' : '此目录为空' }}
      </p>
      <template v-else>
        <FileRow
          v-for="entry in visible"
          :key="entry.path"
          :entry="entry"
          :selected="selected.has(entry.path)"
          @open="open"
          @select="select"
          @toggle="toggle"
        />
      </template>
    </div>

    <p v-if="canUpload" class="drop-hint">可以把文件拖到此处上传</p>

    <PreviewDialog
      v-if="previewTarget"
      v-model:open="previewOpen"
      :entry="previewTarget"
      :siblings="previewables"
    />
    <NameDialog v-model:open="nameOpen" :target="nameTarget" :dir="dir" @created="load" @renamed="load" />
    <ShareDialog v-model:open="shareOpen" :target="shareTarget" />

    <Dialog v-model:open="removeOpen" title="删除确认">
      <p>将永久删除 {{ selectedEntries.length }} 项，此操作不可撤销。</p>
      <ul class="remove-list">
        <li v-for="e in selectedEntries.slice(0, 5)" :key="e.path">{{ e.name }}</li>
        <li v-if="selectedEntries.length > 5">…等 {{ selectedEntries.length }} 项</li>
      </ul>
      <div class="dialog-actions">
        <Button variant="ghost" @click="removeOpen = false">取消</Button>
        <Button variant="danger" :loading="removing" @click="doRemove">删除</Button>
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
  /* 至少撑满首屏，拖拽提示才能落到页面底部而不是紧贴表格。 */
  min-height: calc(100dvh - var(--header-h));
  margin: 0 auto;
  padding: var(--s-6) var(--s-4);
}

.toolbar {
  display: flex;
  align-items: center;
  gap: var(--s-4);
  flex-wrap: wrap;
}

.crumbs {
  display: flex;
  align-items: center;
  gap: var(--s-1);
  min-width: 0;
}

.back {
  display: grid;
  place-items: center;
  width: 32px;
  height: 32px;
  border-radius: var(--r-sm);
  color: var(--text-2);
}

.back:hover {
  background: var(--surface-2);
  color: var(--text);
}

.crumbs nav {
  display: flex;
  align-items: center;
  gap: var(--s-1);
  overflow: hidden;
}

.crumb {
  padding: var(--s-1) var(--s-2);
  border-radius: var(--r-sm);
  color: var(--text-2);
  font-size: var(--t-sm);
  white-space: nowrap;
}

.crumb:hover {
  background: var(--surface-2);
  color: var(--text);
}

.crumb[aria-current='page'] {
  color: var(--text);
  font-weight: 500;
}

.sep {
  color: var(--text-3);
  font-size: var(--t-sm);
}

.actions {
  display: flex;
  align-items: center;
  gap: var(--s-2);
  margin-left: auto;
}

.search {
  display: flex;
  align-items: center;
  gap: var(--s-2);
  height: 32px;
  padding: 0 var(--s-3);
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  color: var(--text-3);
}

.search:focus-within {
  border-color: var(--accent);
}

.search input {
  width: 140px;
  border: none;
  background: none;
  font-size: var(--t-sm);
  outline: none;
}

.quota {
  display: flex;
  align-items: center;
  gap: var(--s-3);
  font-size: var(--t-xs);
  color: var(--text-3);
}

.bar {
  flex: 1;
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

.progress {
  position: relative;
  height: 24px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  overflow: hidden;
}

.fill {
  position: absolute;
  inset: 0 auto 0 0;
  background: color-mix(in oklab, var(--accent) 24%, transparent);
  transition: width var(--dur) var(--ease);
}

.progress span {
  position: relative;
  display: grid;
  place-items: center;
  height: 100%;
  font-size: var(--t-xs);
}

.count {
  color: var(--text-2);
  font-size: var(--t-sm);
  font-weight: 500;
  white-space: nowrap;
}

.table {
  display: flex;
  flex-direction: column;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  overflow: hidden;
}

.head {
  display: grid;
  grid-template-columns: 32px 1fr 96px 148px;
  gap: var(--s-3);
  padding: var(--s-2) var(--s-3);
  border-bottom: 1px solid var(--border);
  color: var(--text-3);
  font-size: var(--t-xs);
  font-weight: 500;
}

.head input {
  width: 16px;
  height: 16px;
  accent-color: var(--accent);
}

.head .right {
  text-align: right;
}

.state {
  padding: var(--s-12) var(--s-4);
  color: var(--text-3);
  font-size: var(--t-sm);
  text-align: center;
}

.state.error {
  color: var(--danger);
}

/* 拖拽提示固定到页面底部：margin-top:auto 吃掉表格之后的全部空白。 */
.drop-hint {
  margin-top: auto;
  padding-top: var(--s-6);
  color: var(--text-3);
  font-size: var(--t-xs);
  text-align: center;
}

.remove-list {
  margin: 0;
  padding-left: var(--s-5);
  color: var(--text-2);
  font-size: var(--t-sm);
}

.dialog-actions {
  display: flex;
  justify-content: flex-end;
  gap: var(--s-2);
}

@media (max-width: 640px) {
  .page {
    padding: var(--s-4) var(--s-3) var(--s-10);
  }

  /* 窄屏下路径独占一行，操作按钮换到下一行右对齐。 */
  .toolbar {
    flex-direction: column;
    align-items: stretch;
    gap: var(--s-3);
  }

  .crumbs {
    flex: none;
    width: 100%;
  }

  .crumbs nav {
    overflow-x: auto;
    scrollbar-width: none;
  }

  .actions {
    margin-left: 0;
    flex-wrap: wrap;
    justify-content: flex-end;
  }

  /* 搜索独占一行，否则会被并排的按钮挤到只剩图标宽度。 */
  .search {
    flex: 1 1 100%;
    min-width: 0;
  }

  .search input {
    width: 100%;
  }

  .head {
    grid-template-columns: 28px 1fr auto;
  }

  .head > :last-child {
    display: none;
  }
}
</style>
