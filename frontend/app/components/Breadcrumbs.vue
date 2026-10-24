<script setup lang="ts">
// 面包屑导航：沙盒虚拟根隔离与全盘层级（等价 renderBreadcrumbs）

interface Crumb {
  path: string
  label: string
  icon?: string
}

const props = defineProps<{ currentDir: string; sandboxRoot: string }>()
const emit = defineEmits<{ navigate: [path: string] }>()

const crumbs = computed<Crumb[]>(() => {
  const items: Crumb[] = []
  if (props.sandboxRoot) {
    const cleanRoot = props.sandboxRoot.replace(/\/+$/, "") || "/"
    items.push({ path: cleanRoot, label: cleanRoot.split("/").pop() || "分享目录", icon: "folder" })
    let relSub = ""
    if (props.currentDir.startsWith(cleanRoot + "/")) {
      relSub = props.currentDir.slice(cleanRoot.length + 1)
    }
    if (relSub) {
      const parts = relSub.split("/").filter(Boolean)
      let acc = cleanRoot
      for (const part of parts) {
        acc += "/" + part
        items.push({ path: acc, label: part })
      }
    }
    return items
  }
  items.push({ path: "/", label: "根目录", icon: "cloud" })
  const clean = props.currentDir.replace(/\/+$/, "")
  if (!clean || clean === "/") return items
  const parts = clean.split("/").filter(Boolean)
  let acc = ""
  for (const part of parts) {
    acc += "/" + part
    items.push({ path: acc, label: part })
  }
  return items
})
</script>

<template>
  <nav class="breadcrumbs" aria-label="目录路径">
    <template v-for="(crumb, index) in crumbs" :key="crumb.path">
      <span v-if="index > 0" class="breadcrumb-sep"><AppIcon name="chevron-right" :size="14" /></span>
      <button
        type="button"
        class="breadcrumb-item"
        :data-path="crumb.path"
        :class="{ 'is-active': index === crumbs.length - 1 }"
        @click="emit('navigate', crumb.path)"
      >
        <span v-if="crumb.icon" class="breadcrumb-icon"><AppIcon :name="crumb.icon" :size="14" /></span>
        <span class="breadcrumb-label">{{ crumb.label }}</span>
      </button>
    </template>
  </nav>
</template>
