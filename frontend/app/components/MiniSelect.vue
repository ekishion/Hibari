<script setup lang="ts">
// 通用自绘下拉（等价原 bindCustomSelect 的交互形态）
const props = withDefaults(defineProps<{
  modelValue: string
  options: Array<{ value: string; label: string }>
  label: string
}>(), {})
const emit = defineEmits<{ "update:modelValue": [value: string] }>()

const open = ref(false)
const wrap = ref<HTMLElement | null>(null)

const currentLabel = computed(
  () => props.options.find((o) => o.value === props.modelValue)?.label ?? props.options[0]?.label ?? "",
)

function choose(value: string) {
  emit("update:modelValue", value)
  open.value = false
}

function onDocumentClick(e: MouseEvent) {
  if (wrap.value && !wrap.value.contains(e.target as Node)) open.value = false
}

onMounted(() => document.addEventListener("click", onDocumentClick))
onBeforeUnmount(() => document.removeEventListener("click", onDocumentClick))
</script>

<template>
  <div ref="wrap" class="custom-select">
    <button
      type="button"
      class="btn btn-ghost custom-select-trigger"
      aria-haspopup="listbox"
      :aria-expanded="open"
      :aria-label="label"
      @click.stop="open = !open"
    >
      <span class="btn-label">{{ currentLabel }}</span>
      <span class="btn-icon select-caret"><AppIcon name="chevron-down" :size="14" /></span>
    </button>
    <div class="custom-select-menu" role="listbox" :aria-label="label" :hidden="!open">
      <button
        v-for="option in options"
        :key="option.value"
        type="button"
        class="custom-select-option"
        :class="{ 'is-active': option.value === modelValue }"
        role="option"
        :aria-selected="option.value === modelValue"
        @click="choose(option.value)"
      >
        {{ option.label }}
      </button>
    </div>
  </div>
</template>
