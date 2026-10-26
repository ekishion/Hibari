<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api } from '@/api/client'
import type { Entry } from '@/api/types'
import Button from '@/ui/Button.vue'
import Dialog from '@/ui/Dialog.vue'
import Input from '@/ui/Input.vue'
import { toast } from '@/ui/toast'

const props = defineProps<{ target: Entry | null }>()
const open = defineModel<boolean>('open', { default: false })

const password = ref('')
const days = ref('7')
const busy = ref(false)
const created = ref<{ url: string; password: string } | null>(null)

const isDir = computed(() => props.target?.is_dir ?? false)

watch(open, (isOpen) => {
  if (isOpen) {
    password.value = ''
    days.value = '7'
    created.value = null
  }
})

async function submit() {
  if (!props.target) return
  busy.value = true
  try {
    const ttl = Math.max(1, Number(days.value) || 7) * 86400
    const share = await api.createShare(props.target.path, password.value.trim(), ttl)
    created.value = {
      url: `${location.origin}/api/share/${share.token}`,
      password: password.value.trim(),
    }
  } catch (e) {
    toast.error(e instanceof Error ? e.message : '创建分享失败')
  } finally {
    busy.value = false
  }
}

async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    toast.success('已复制')
  } catch {
    toast.error('复制失败，请手动选中')
  }
}
</script>

<template>
  <Dialog v-model:open="open" :title="created ? '分享已创建' : '创建分享'">
    <div v-if="!created" class="form">
      <p class="target">{{ target?.name }}</p>
      <p class="hint">{{ isDir ? '目录分享会建立只读浏览沙盒。' : '文件分享提供直接下载。' }}</p>
      <Input
        v-model="password"
        label="提取码"
        hint="留空表示不设提取码"
        placeholder="可选"
        autocomplete="off"
      />
      <Input v-model="days" label="有效期（天）" type="number" />
      <div class="actions">
        <Button variant="ghost" @click="open = false">取消</Button>
        <Button :loading="busy" @click="submit">创建</Button>
      </div>
    </div>

    <div v-else class="result">
      <div class="line">
        <code>{{ created.url }}</code>
        <Button size="sm" variant="ghost" @click="copy(created.url)">复制</Button>
      </div>
      <div v-if="created.password" class="line">
        <span>提取码</span>
        <code>{{ created.password }}</code>
        <Button size="sm" variant="ghost" @click="copy(created.password)">复制</Button>
      </div>
      <div class="actions">
        <Button @click="open = false">完成</Button>
      </div>
    </div>
  </Dialog>
</template>

<style scoped>
.form,
.result {
  display: flex;
  flex-direction: column;
  gap: var(--s-4);
}

.target {
  font-weight: 500;
  word-break: break-all;
}

.hint {
  margin-top: calc(var(--s-3) * -1);
  color: var(--text-3);
  font-size: var(--t-sm);
}

.line {
  display: flex;
  align-items: center;
  gap: var(--s-3);
  padding: var(--s-3);
  background: var(--surface-2);
  border-radius: var(--r-md);
}

.line code {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--t-sm);
}

.actions {
  display: flex;
  justify-content: flex-end;
  gap: var(--s-2);
}
</style>
