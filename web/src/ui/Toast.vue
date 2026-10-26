<script setup lang="ts">
import { toast } from './toast'
</script>

<template>
  <div class="stack" role="status" aria-live="polite">
    <TransitionGroup name="toast">
      <div v-for="t in toast.state.items" :key="t.id" :class="['toast', t.kind]">
        <span>{{ t.message }}</span>
        <button type="button" aria-label="关闭提示" @click="toast.dismiss(t.id)">✕</button>
      </div>
    </TransitionGroup>
  </div>
</template>

<style scoped>
.stack {
  position: fixed;
  right: var(--s-4);
  bottom: var(--s-4);
  z-index: 100;
  display: flex;
  flex-direction: column;
  gap: var(--s-2);
  max-width: min(380px, calc(100vw - var(--s-8)));
  pointer-events: none;
}

.toast {
  display: flex;
  align-items: flex-start;
  gap: var(--s-3);
  padding: var(--s-3) var(--s-4);
  background: var(--surface);
  border: 1px solid var(--border);
  border-left: 3px solid var(--text-3);
  border-radius: var(--r-md);
  box-shadow: var(--shadow-2);
  font-size: var(--t-sm);
  pointer-events: auto;
}

.toast.success {
  border-left-color: var(--success);
}

.toast.error {
  border-left-color: var(--danger);
}

.toast button {
  flex-shrink: 0;
  color: var(--text-3);
  font-size: var(--t-xs);
}

.toast button:hover {
  color: var(--text);
}

.toast-enter-active,
.toast-leave-active {
  transition:
    opacity var(--dur) var(--ease),
    transform var(--dur) var(--ease);
}

.toast-enter-from,
.toast-leave-to {
  opacity: 0;
  transform: translateY(8px);
}
</style>
