<script setup lang="ts">
import { computed, useId } from 'vue'

const props = withDefaults(
  defineProps<{
    label: string
    type?: string
    autocomplete?: string
    placeholder?: string
    hint?: string
    error?: string
    disabled?: boolean
    autofocus?: boolean
  }>(),
  { type: 'text', disabled: false, autofocus: false },
)

const model = defineModel<string>({ default: '' })
const id = useId()

const describedBy = computed(() => {
  const ids: string[] = []
  if (props.hint) ids.push(`${id}-hint`)
  if (props.error) ids.push(`${id}-error`)
  return ids.length ? ids.join(' ') : undefined
})
</script>

<template>
  <div class="field">
    <label :for="id">{{ label }}</label>
    <input
      :id="id"
      v-model="model"
      :type="type"
      :autocomplete="autocomplete"
      :placeholder="placeholder"
      :disabled="disabled"
      :autofocus="autofocus"
      :aria-invalid="error ? true : undefined"
      :aria-describedby="describedBy"
    />
    <p v-if="hint && !error" :id="`${id}-hint`" class="hint">{{ hint }}</p>
    <p v-if="error" :id="`${id}-error`" class="error" role="alert">{{ error }}</p>
  </div>
</template>

<style scoped>
.field {
  display: flex;
  flex-direction: column;
  gap: var(--s-2);
}

label {
  font-size: var(--t-sm);
  font-weight: 500;
  color: var(--text-2);
}

input {
  height: 40px;
  padding: 0 var(--s-3);
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  transition: border-color var(--dur-fast) var(--ease);
}

input::placeholder {
  color: var(--text-3);
}

input:hover:not(:disabled) {
  border-color: var(--border-strong);
}

input:focus {
  outline: none;
  border-color: var(--accent);
  box-shadow: 0 0 0 3px color-mix(in oklab, var(--accent) 20%, transparent);
}

input[aria-invalid='true'] {
  border-color: var(--danger);
}

input:disabled {
  opacity: 0.6;
}

/* 触控目标不小于 44px。 */
@media (max-width: 640px) {
  input {
    height: 44px;
  }
}

.hint,
.error {
  font-size: var(--t-xs);
  line-height: 1.5;
}

.hint {
  color: var(--text-3);
}

.error {
  color: var(--danger);
}
</style>
