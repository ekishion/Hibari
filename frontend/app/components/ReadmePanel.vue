<script setup lang="ts">
// README 面板：安全渲染自研 markdown + 目录概览（等价 checkAndLoadReadme / renderReadmeOverview）
import { fileNameOf, fileTimeOf, type BaiduFile } from '~/composables/useFiles'
import { renderMarkdownInto } from '~/utils/markdown'

const props = defineProps<{ files: BaiduFile[] }>()

const { uiConfig } = useAuth()
const { readmeState } = useFiles()

const contentBox = ref<HTMLElement | null>(null)

const OVERVIEW_TYPE_LABELS: Record<string, string> = {
  image: "图片", film: "视频", music: "音频", "file-text": "文档",
  archive: "压缩包", "file-code": "代码", "file-spreadsheet": "表格",
  smartphone: "应用", package: "安装包",
}

const overview = computed(() => {
  if (uiConfig.showReadmeOverview === false) return null
  const list = props.files || []
  if (!list.length) return null

  let dirs = 0
  let fileCount = 0
  let totalSize = 0
  let latest = 0
  const byType: Record<string, number> = {}

  for (const f of list) {
    if (Number(f.isdir) === 1) {
      dirs++
      continue
    }
    fileCount++
    totalSize += Number(f.size) || 0
    const t = fileTimeOf(f)
    if (t > latest) latest = t
    const cat = fileIconMeta(fileNameOf(f), false).category
    byType[cat] = (byType[cat] || 0) + 1
  }

  const entries = Object.keys(byType)
    .map((cat) => ({ cat, count: byType[cat] }))
    .sort((a, b) => b.count - a.count)

  return {
    dirs,
    fileCount,
    totalSize: formatSize(totalSize),
    latest: latest > 0 ? formatTime(latest) : "-",
    types: entries.length >= 2 ? entries.map((e) => ({ label: OVERVIEW_TYPE_LABELS[e.cat] || "其他", count: e.count })) : [],
  }
})

// markdown 渲染走安全 DOM 构造（非 v-html），与原实现一致
watch(
  () => [readmeState.visible, readmeState.content] as const,
  ([visible]) => {
    if (!visible) return
    nextTick(() => {
      if (contentBox.value) {
        contentBox.value.replaceChildren()
        renderMarkdownInto(contentBox.value, readmeState.content)
      }
    })
  },
)
</script>

<template>
  <section id="readme-panel" class="readme-panel" :hidden="!readmeState.visible" aria-labelledby="readme-title">
    <div class="readme-layout">
      <div class="readme-main">
        <div class="readme-heading">
          <div class="readme-icon file-icon" aria-hidden="true">
            <AppIcon name="file-text" :size="20" />
          </div>
          <div>
            <h2 id="readme-title">目录说明</h2>
            <p id="readme-source">{{ readmeState.source }}</p>
          </div>
        </div>
        <div id="readme-content" ref="contentBox" class="md-content" />
      </div>
      <aside v-if="overview" id="readme-overview" class="readme-overview" aria-label="目录概览">
        <h3>目录概览</h3>
        <div class="overview-item"><span class="overview-label">文件夹</span><span class="overview-value" :title="overview.dirs">{{ overview.dirs }}</span></div>
        <div class="overview-item"><span class="overview-label">文件</span><span class="overview-value" :title="overview.fileCount">{{ overview.fileCount }}</span></div>
        <div class="overview-item"><span class="overview-label">总大小</span><span class="overview-value" :title="overview.totalSize">{{ overview.totalSize }}</span></div>
        <div class="overview-item"><span class="overview-label">最近修改</span><span class="overview-value" :title="overview.latest">{{ overview.latest }}</span></div>
        <template v-if="overview.types.length">
          <h3 class="overview-sub">文件类型</h3>
          <div v-for="t in overview.types" :key="t.label" class="overview-item">
            <span class="overview-label">{{ t.label }}</span><span class="overview-value" :title="t.count">{{ t.count }}</span>
          </div>
        </template>
      </aside>
    </div>
  </section>
</template>
