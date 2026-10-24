<script setup lang="ts">
import { iconSpec } from '~/utils/icons'

const props = withDefaults(defineProps<{ name: string; size?: number }>(), { size: 24 })
const spec = computed(() => iconSpec(props.name))
</script>

<template>
  <svg
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="2"
    stroke-linecap="round"
    stroke-linejoin="round"
    class="icon"
    aria-hidden="true"
    focusable="false"
    :width="size"
    :height="size"
  >
    <template v-for="([tag, attrs], i) in spec" :key="i">
      <component :is="tag" v-bind="attrs" />
    </template>
  </svg>
</template>
