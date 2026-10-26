<script setup lang="ts">
/**
 * 下拉菜单。用原生 popover 做浮层定位与点击外部关闭，
 * 内容由调用方通过默认插槽提供。
 */
import { onBeforeUnmount, ref, useId } from 'vue'

withDefaults(defineProps<{ label: string; align?: 'start' | 'end' }>(), { align: 'end' })

const open = ref(false)
const wrap = ref<HTMLElement | null>(null)
const id = useId()

function toggle() {
  open.value = !open.value
}

function close() {
  open.value = false
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && open.value) {
    e.stopPropagation()
    close()
  }
}

function onPointerDown(e: PointerEvent) {
  if (open.value && wrap.value && !wrap.value.contains(e.target as Node)) close()
}

document.addEventListener('pointerdown', onPointerDown)
document.addEventListener('keydown', onKeydown)
onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onPointerDown)
  document.removeEventListener('keydown', onKeydown)
})
</script>

<template>
  <div ref="wrap" class="menu-wrap">
    <button
      type="button"
      class="trigger"
      :aria-expanded="open"
      :aria-controls="id"
      aria-haspopup="menu"
      @click="toggle"
    >
      {{ label }}
    </button>
    <div v-if="open" :id="id" class="panel" :class="align" role="menu" @click="close">
      <slot />
    </div>
  </div>
</template>

<style scoped>
.menu-wrap {
  position: relative;
}

.trigger {
  height: 32px;
  padding: 0 var(--s-3);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  font-size: var(--t-sm);
  color: var(--text-2);
  transition: border-color var(--dur-fast) var(--ease);
}

.trigger:hover,
.trigger[aria-expanded='true'] {
  border-color: var(--border-strong);
  color: var(--text);
}

.panel {
  position: absolute;
  top: calc(100% + var(--s-1));
  z-index: 50;
  display: flex;
  flex-direction: column;
  min-width: 168px;
  padding: var(--s-1);
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  box-shadow: var(--shadow-2);
}

.panel.start {
  left: 0;
}

.panel.end {
  right: 0;
}

.panel :deep(button),
.panel :deep(a) {
  display: flex;
  align-items: center;
  gap: var(--s-2);
  width: 100%;
  padding: var(--s-2) var(--s-3);
  border-radius: var(--r-sm);
  font-size: var(--t-sm);
  color: var(--text);
  text-align: left;
  text-decoration: none;
}

.panel :deep(button:hover),
.panel :deep(a:hover) {
  background: var(--surface-2);
}

.panel :deep(.danger) {
  color: var(--danger);
}
</style>
