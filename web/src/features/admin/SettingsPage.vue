<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, ApiFailure } from '@/api/client'
import { session } from '@/composables/useSession'
import { toast } from '@/ui/toast'
import Button from '@/ui/Button.vue'
import Input from '@/ui/Input.vue'
import Dialog from '@/ui/Dialog.vue'

const router = useRouter()

const current = ref('')
const next = ref('')
const confirm = ref('')
const error = ref('')
const busy = ref(false)
const logoutOpen = ref(false)

const backendLabel = computed(() => (session.state.backend === 'baidu' ? '百度网盘青春版' : '本地磁盘'))
const caps = computed(() => session.state.caps)

const mismatch = computed(() => confirm.value !== '' && confirm.value !== next.value)
const tooShort = computed(() => next.value !== '' && next.value.length < 8)
const canSubmit = computed(() => current.value !== '' && next.value.length >= 8 && next.value === confirm.value)

async function submit() {
  error.value = ''
  busy.value = true
  try {
    await api.changePassword(current.value, next.value)
    toast.success('密码已修改，请重新登录')
    session.setUser(null)
    router.replace({ name: 'login' })
  } catch (e) {
    error.value = e instanceof ApiFailure ? e.message : '修改失败'
  } finally {
    busy.value = false
  }
}

async function logout() {
  try {
    await api.logout()
  } catch {
    // 忽略：本地状态照常清理。
  }
  session.setUser(null)
  router.replace({ name: 'login' })
}

onMounted(() => {
  current.value = ''
})
</script>

<template>
  <div class="page">
    <header>
      <h1>设置</h1>
      <p class="sub">账号与运行环境信息。</p>
    </header>

    <section class="card">
      <h2>运行环境</h2>
      <dl>
        <div>
          <dt>存储后端</dt>
          <dd>{{ backendLabel }}</dd>
        </div>
        <div>
          <dt>版本</dt>
          <dd class="tnum">{{ session.state.version }}</dd>
        </div>
        <div>
          <dt>能力</dt>
          <dd>
            <span class="chip" :class="{ on: caps.write }">写操作{{ caps.write ? '可用' : '不可用' }}</span>
            <span class="chip" :class="{ on: caps.upload }">上传{{ caps.upload ? '可用' : '不可用' }}</span>
            <span class="chip" :class="{ on: caps.quota }">容量{{ caps.quota ? '可用' : '不可用' }}</span>
          </dd>
        </div>
      </dl>
      <p class="note">
        后端由配置文件决定，修改后需重启服务。百度后端为非官方接口，稳定性不作保证。
      </p>
    </section>

    <section class="card">
      <h2>修改密码</h2>
      <form class="form" @submit.prevent="submit">
        <Input v-model="current" label="当前密码" type="password" autocomplete="current-password" />
        <Input
          v-model="next"
          label="新密码"
          type="password"
          autocomplete="new-password"
          hint="至少 8 个字符"
          :error="tooShort ? '密码至少 8 个字符' : ''"
        />
        <Input
          v-model="confirm"
          label="确认新密码"
          type="password"
          autocomplete="new-password"
          :error="mismatch ? '两次输入不一致' : ''"
        />
        <p v-if="error" class="error" role="alert">{{ error }}</p>
        <div class="actions">
          <Button type="submit" :loading="busy" :disabled="!canSubmit">保存</Button>
        </div>
      </form>
    </section>

    <section class="card">
      <h2>会话</h2>
      <p class="note">退出登录会立即吊销当前会话令牌。</p>
      <div class="actions">
        <Button variant="ghost" @click="logoutOpen = true">退出登录</Button>
      </div>
    </section>

    <Dialog v-model:open="logoutOpen" title="退出登录">
      <p>确认退出当前账号？</p>
      <div class="dialog-actions">
        <Button variant="ghost" @click="logoutOpen = false">取消</Button>
        <Button @click="logout">退出</Button>
      </div>
    </Dialog>
  </div>
</template>

<style scoped>
.page {
  display: flex;
  flex-direction: column;
  gap: var(--s-4);
  max-width: 720px;
  margin: 0 auto;
  padding: var(--s-6) var(--s-4) var(--s-12);
}

h1 {
  font-size: var(--t-xl);
}

h2 {
  font-size: var(--t-md);
}

.sub {
  color: var(--text-2);
  font-size: var(--t-sm);
}

.card {
  display: flex;
  flex-direction: column;
  gap: var(--s-4);
  padding: var(--s-5);
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
}

dl {
  display: flex;
  flex-direction: column;
  gap: var(--s-3);
  margin: 0;
}

dl > div {
  display: flex;
  align-items: baseline;
  gap: var(--s-4);
}

dt {
  width: 96px;
  flex-shrink: 0;
  color: var(--text-3);
  font-size: var(--t-sm);
}

dd {
  margin: 0;
  display: flex;
  flex-wrap: wrap;
  gap: var(--s-2);
}

.chip {
  padding: 2px var(--s-2);
  background: var(--surface-2);
  border-radius: var(--r-sm);
  color: var(--text-3);
  font-size: var(--t-xs);
}

.chip.on {
  color: var(--success);
}

.note {
  color: var(--text-3);
  font-size: var(--t-xs);
  line-height: 1.6;
}

.form {
  display: flex;
  flex-direction: column;
  gap: var(--s-4);
}

.error {
  color: var(--danger);
  font-size: var(--t-sm);
}

.actions {
  display: flex;
  justify-content: flex-end;
  gap: var(--s-2);
}

.dialog-actions {
  display: flex;
  justify-content: flex-end;
  gap: var(--s-2);
}
</style>
