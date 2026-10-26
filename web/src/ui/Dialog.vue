<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, useSlots, watch } from 'vue'

/**
 * 基于原生 <dialog>：Esc 关闭、焦点陷阱、焦点恢复、::backdrop 都由浏览器提供。
 * 组件只负责把 open 状态同步到 showModal/close。
 *
 * flush 让调用方自己排版头部以下的区域（预览这类需要三段式固定的场景），
 * 此时插槽内容区获得 flex:1 与 min-height:0。
 */
withDefaults(
  defineProps<{ title: string; description?: string; width?: string; height?: string; flush?: boolean }>(),
  { width: '420px', height: '', flush: false },
)

const open = defineModel<boolean>('open', { default: false })
const slots = useSlots()
const dialog = ref<HTMLDialogElement | null>(null)

function sync(isOpen: boolean) {
  const el = dialog.value
  if (!el) return
  if (isOpen && !el.open) el.showModal()
  if (!isOpen && el.open) el.close()
}

// 挂载时若已为打开状态（父级用 v-if 控制），需要主动 showModal。
onMounted(() => sync(open.value))
watch(open, sync)

// 浏览器自身触发的关闭（Esc、表单 method=dialog）要同步回 v-model。
function onClose() {
  open.value = false
}

onBeforeUnmount(() => {
  if (dialog.value?.open) dialog.value.close()
})
</script>

<template>
  <dialog
    ref="dialog"
    :class="{ flush }"
    :style="{ '--dialog-w': width, '--dialog-h': height || undefined }"
    @close="onClose"
    @cancel="onClose"
  >
    <div class="body">
      <header>
        <div class="heading">
          <h2>{{ title }}</h2>
          <p v-if="description" class="desc">{{ description }}</p>
          <slot name="subtitle" />
        </div>
        <button type="button" class="close" aria-label="关闭" @click="open = false">✕</button>
      </header>
      <slot />
      <footer v-if="slots.footer">
        <slot name="footer" />
      </footer>
    </div>
  </dialog>
</template>

<style scoped>
dialog {
  width: min(var(--dialog-w), calc(100vw - var(--s-8)));
  max-height: calc(100dvh - var(--s-12));
  /* 原生 dialog 依赖 inset+margin 居中，覆盖样式时必须显式写回。 */
  inset: 0;
  margin: auto;
  padding: 0;
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  background: var(--surface);
  color: var(--text);
  box-shadow: var(--shadow-3);
  overflow: auto;
}

/* flush：高度由调用方给，内部自己分配空间，整框不再滚动。 */
dialog.flush[open] {
  display: flex;
  flex-direction: column;
  height: min(var(--dialog-h, 80dvh), calc(100dvh - var(--s-12)));
  overflow: hidden;
}

dialog::backdrop {
  background: light-dark(rgb(0 0 0 / 0.4), rgb(0 0 0 / 0.7));
  backdrop-filter: blur(2px);
}

dialog[open] {
  animation: appear var(--dur) var(--ease);
}

@keyframes appear {
  from {
    opacity: 0;
    transform: translateY(8px) scale(0.98);
  }
}

.body {
  display: flex;
  flex-direction: column;
  gap: var(--s-4);
  padding: var(--s-6);
}

dialog.flush .body {
  flex: 1;
  gap: 0;
  min-height: 0;
  padding: 0;
}

header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--s-3);
}

dialog.flush header {
  padding: var(--s-4) var(--s-5);
  border-bottom: 1px solid var(--border);
}

.heading {
  min-width: 0;
}

h2 {
  font-size: var(--t-lg);
  overflow-wrap: anywhere;
}

dialog.flush h2 {
  font-size: var(--t-md);
}

.close {
  display: grid;
  place-items: center;
  width: 28px;
  height: 28px;
  margin: calc(var(--s-1) * -1);
  border-radius: var(--r-sm);
  color: var(--text-3);
  font-size: var(--t-sm);
}

.close:hover {
  background: var(--surface-2);
  color: var(--text);
}

.desc {
  margin-top: calc(var(--s-2) * -1);
  color: var(--text-2);
  font-size: var(--t-sm);
}

dialog.flush .desc {
  margin-top: var(--s-1);
  font-size: var(--t-xs);
}

dialog.flush footer {
  padding: var(--s-3) var(--s-5);
  border-top: 1px solid var(--border);
}

@media (max-width: 640px) {
  dialog {
    /* 手机上多让出 16px 给内容：两侧各留 8px 边距。 */
    width: min(var(--dialog-w), calc(100vw - var(--s-4)));
    max-height: calc(100dvh - var(--s-6));
  }

  dialog.flush[open] {
    height: min(var(--dialog-h, 80dvh), calc(100dvh - var(--s-6)));
  }

  .body {
    padding: var(--s-5) var(--s-4);
  }

  dialog.flush header,
  dialog.flush footer {
    padding: var(--s-3) var(--s-4);
  }

  /* 触控目标不小于 44px。 */
  .close {
    width: 44px;
    height: 44px;
    margin: calc(var(--s-2) * -1) calc(var(--s-2) * -1) 0 0;
  }
}
</style>
