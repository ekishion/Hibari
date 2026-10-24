<script setup lang="ts">
// 排序控件：字段下拉（自绘菜单）+ 升降序切换
const SORT_FIELD_LABELS: Record<string, string> = { name: "名称", size: "大小", time: "时间" }

const { sortState, setSort } = useFiles()
const menuOpen = ref(false)
const menuWrap = ref<HTMLElement | null>(null)

const SORT_KEYS: Array<"name" | "size" | "time"> = ["name", "size", "time"]

function choose(key: "name" | "size" | "time") {
  setSort(key, sortState.dir)
  menuOpen.value = false
}

function toggleDir() {
  setSort(sortState.key, sortState.dir === "asc" ? "desc" : "asc")
}

function onDocumentClick(e: MouseEvent) {
  if (menuWrap.value && !menuWrap.value.contains(e.target as Node)) menuOpen.value = false
}

onMounted(() => document.addEventListener("click", onDocumentClick))
onBeforeUnmount(() => document.removeEventListener("click", onDocumentClick))
</script>

<template>
  <div ref="menuWrap" class="sort-group" role="group" aria-label="排序方式">
    <div class="custom-select">
      <button
        type="button"
        class="btn btn-ghost custom-select-trigger"
        aria-haspopup="listbox"
        :aria-expanded="menuOpen"
        aria-label="选择排序字段"
        @click.stop="menuOpen = !menuOpen"
      >
        <span class="btn-label">{{ SORT_FIELD_LABELS[sortState.key] || "名称" }}</span>
        <span class="btn-icon select-caret"><AppIcon name="chevron-down" :size="14" /></span>
      </button>
      <div class="custom-select-menu" role="listbox" aria-label="排序字段" :hidden="!menuOpen">
        <button
          v-for="key in SORT_KEYS"
          :key="key"
          type="button"
          class="custom-select-option"
          :class="{ 'is-active': sortState.key === key }"
          role="option"
          :aria-selected="sortState.key === key"
          @click="choose(key)"
        >
          {{ SORT_FIELD_LABELS[key] }}
        </button>
      </div>
    </div>
    <button type="button" class="btn btn-ghost btn-sort-dir" aria-label="切换升序降序" @click="toggleDir">
      <span class="btn-icon"><AppIcon :name="sortState.dir === 'asc' ? 'arrow-up' : 'arrow-down'" :size="15" /></span>
    </button>
  </div>
</template>
