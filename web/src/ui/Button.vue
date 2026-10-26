<script setup lang="ts">
import { computed } from 'vue'

type Variant = 'primary' | 'ghost' | 'danger'
type Size = 'sm' | 'md'

const props = withDefaults(
  defineProps<{
    variant?: Variant
    size?: Size
    disabled?: boolean
    loading?: boolean
    type?: 'button' | 'submit'
  }>(),
  { variant: 'primary', size: 'md', disabled: false, loading: false, type: 'button' },
)

const classes = computed(() => [props.variant, props.size])
</script>

<template>
  <button :type="type" :class="classes" :disabled="disabled || loading" :aria-busy="loading">
    <span v-if="loading" class="spinner" aria-hidden="true" />
    <slot />
  </button>
</template>

<style scoped>
button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: var(--s-2);
  border-radius: var(--r-md);
  font-weight: 500;
  white-space: nowrap;
  transition:
    background var(--dur-fast) var(--ease),
    border-color var(--dur-fast) var(--ease),
    opacity var(--dur-fast) var(--ease);
}

button:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.sm {
  height: 32px;
  padding: 0 var(--s-3);
  font-size: var(--t-sm);
}

.md {
  height: 40px;
  padding: 0 var(--s-4);
}

.primary {
  background: var(--text);
  color: var(--bg);
}

.primary:hover:not(:disabled) {
  opacity: 0.88;
}

.ghost {
  background: transparent;
  color: var(--text);
  border: 1px solid var(--border);
}

.ghost:hover:not(:disabled) {
  background: var(--surface-2);
  border-color: var(--border-strong);
}

.danger {
  background: transparent;
  color: var(--danger);
  border: 1px solid var(--border);
}

.danger:hover:not(:disabled) {
  background: color-mix(in oklab, var(--danger) 10%, transparent);
  border-color: var(--danger);
}

.spinner {
  width: 14px;
  height: 14px;
  border: 2px solid currentColor;
  border-top-color: transparent;
  border-radius: var(--r-full);
  animation: spin 0.6s linear infinite;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
