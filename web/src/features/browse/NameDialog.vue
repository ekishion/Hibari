<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { Entry } from '@/api/types'
import Button from '@/ui/Button.vue'
import Dialog from '@/ui/Dialog.vue'
import Input from '@/ui/Input.vue'

const props = defineProps<{ target: Entry | null; dir: string }>()
const open = defineModel<boolean>('open', { default: false })
const emit = defineEmits<{ created: [string]; renamed: [string] }>()

const name = ref('')
const error = ref('')
const busy = ref(false)

const mode = computed(() => (props.target ? 'rename' : 'mkdir'))
const title = computed(() => (mode.value === 'rename' ? '重命名' : '新建文件夹'))

// 每次打开时重置输入，避免残留上一次的文本。
watch(
  open,
  (isOpen) => {
    if (!isOpen) return
    name.value = props.target ? props.target.name : ''
    error.value = ''
    busy.value = false
  },
  { immediate: true },
)

async function submit() {
  error.value = ''
  const value = name.value.trim()
  if (!value) {
    error.value = '名称不能为空'
    return
  }
  if (/[/\\]/.test(value)) {
    error.value = '名称不能包含斜杠'
    return
  }

  busy.value = true
  try {
    const { api } = await import('@/api/client')
    if (mode.value === 'rename' && props.target) {
      const res = await api.rename(props.target.path, value)
      emit('renamed', res.path)
    } else {
      const res = await api.mkdir(props.dir, value)
      emit('created', res.path)
    }
    open.value = false
  } catch (e) {
    error.value = e instanceof Error ? e.message : '操作失败'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <Dialog v-model:open="open" :title="title">
    <form class="form" @submit.prevent="submit">
      <Input v-model="name" label="名称" :error="error" autofocus />
      <div class="actions">
        <Button variant="ghost" @click="open = false">取消</Button>
        <Button type="submit" :loading="busy">{{ mode === 'rename' ? '保存' : '创建' }}</Button>
      </div>
    </form>
  </Dialog>
</template>

<style scoped>
.form {
  display: flex;
  flex-direction: column;
  gap: var(--s-4);
}

.actions {
  display: flex;
  justify-content: flex-end;
  gap: var(--s-2);
}
</style>
