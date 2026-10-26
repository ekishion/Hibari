<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, ApiFailure } from '@/api/client'
import { session } from '@/composables/useSession'
import Button from '@/ui/Button.vue'
import Input from '@/ui/Input.vue'

const router = useRouter()
const route = useRoute()

const username = ref('')
const password = ref('')
const error = ref('')
const busy = ref(false)

async function submit() {
  error.value = ''
  busy.value = true
  try {
    const { user } = await api.login(username.value, password.value)
    session.setUser(user)
    const next = typeof route.query.next === 'string' ? route.query.next : '/'
    router.replace(next)
  } catch (e) {
    error.value = e instanceof ApiFailure ? e.message : '登录失败，请稍后再试'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="page">
    <form class="card" @submit.prevent="submit">
      <h1>登录</h1>
      <p class="sub">输入管理员账号以管理文件与分享。</p>

      <Input v-model="username" label="用户名" autocomplete="username" autofocus />
      <Input
        v-model="password"
        label="密码"
        type="password"
        autocomplete="current-password"
        :error="error"
      />

      <Button type="submit" :loading="busy" :disabled="!username || !password">登录</Button>
    </form>
  </div>
</template>

<style scoped>
.page {
  display: grid;
  place-items: center;
  min-height: calc(100dvh - var(--header-h));
  padding: var(--s-6) var(--s-4);
}

.card {
  display: flex;
  flex-direction: column;
  gap: var(--s-4);
  width: min(360px, 100%);
  padding: var(--s-8);
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  box-shadow: var(--shadow-1);
}

h1 {
  font-size: var(--t-xl);
}

.sub {
  margin-top: calc(var(--s-3) * -1);
  color: var(--text-2);
  font-size: var(--t-sm);
}
</style>
