<script setup lang="ts">
import { computed } from 'vue'
import type { Entry } from '@/api/types'
import { formatBytes, formatTime, previewKind } from '@/utils/format'
import { File, Folder, Image, Film, Music, FileText } from 'lucide-vue-next'

const props = defineProps<{ entry: Entry; selected: boolean }>()
const emit = defineEmits<{ open: [Entry]; toggle: [Entry] }>()

const icon = computed(() => {
  if (props.entry.is_dir) return Folder
  switch (previewKind(props.entry.name)) {
    case 'image':
      return Image
    case 'video':
      return Film
    case 'audio':
      return Music
    case 'text':
      return FileText
    default:
      return File
  }
})
</script>

<template>
  <div
    class="row"
    :class="{ selected }"
    role="row"
    tabindex="0"
    @click="emit('open', entry)"
    @keydown.enter.prevent="emit('open', entry)"
    @keydown.space.prevent="emit('toggle', entry)"
  >
    <div class="check" role="cell">
      <input
        type="checkbox"
        :checked="selected"
        :aria-label="`选择 ${entry.name}`"
        @click.stop
        @change="emit('toggle', entry)"
      />
    </div>

    <div class="name" role="cell">
      <component :is="icon" :size="18" class="icon" aria-hidden="true" />
      <span class="label" :title="entry.name">{{ entry.name }}</span>
    </div>

    <div class="size tnum" role="cell">{{ entry.is_dir ? '—' : formatBytes(entry.size) }}</div>
    <div class="time tnum" role="cell">{{ formatTime(entry.mod_time) }}</div>
  </div>
</template>

<style scoped>
.row {
  display: grid;
  grid-template-columns: 32px 1fr 96px 148px;
  align-items: center;
  gap: var(--s-3);
  min-height: 44px;
  padding: 0 var(--s-3);
  border-radius: var(--r-md);
  cursor: pointer;
  transition: background var(--dur-fast) var(--ease);
}

.row:hover {
  background: var(--surface-2);
}

.row:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: -2px;
}

.row.selected {
  background: color-mix(in oklab, var(--accent) 10%, transparent);
}

.check {
  display: grid;
  place-items: center;
}

.check input {
  width: 16px;
  height: 16px;
  accent-color: var(--accent);
  cursor: pointer;
}

.name {
  display: flex;
  align-items: center;
  gap: var(--s-3);
  min-width: 0;
}

.icon {
  flex-shrink: 0;
  color: var(--text-3);
}

.label {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.size,
.time {
  color: var(--text-3);
  font-size: var(--t-sm);
}

.time {
  text-align: right;
}

@media (max-width: 640px) {
  .row {
    grid-template-columns: 28px 1fr auto;
    min-height: 56px;
  }

  .time {
    display: none;
  }

  .size {
    text-align: right;
  }
}
</style>
