<script setup lang="ts">
// 登录弹窗：等价迁移自 login_modal.html + app.js 的登录/倒计时逻辑
const { loginPrompt, doLogin, dismissLogin } = useAuth()
const { showToast } = useToast()

const token = ref("")
const errorMessage = ref("")
const submitting = ref(false)
const countdown = ref(0)
const tokenInput = ref<HTMLInputElement | null>(null)

let countdownTimer: ReturnType<typeof setInterval> | null = null

watch(
  () => loginPrompt.value.open,
  (open) => {
    if (open) {
      errorMessage.value = loginPrompt.value.message
      nextTick(() => {
        tokenInput.value?.focus()
        tokenInput.value?.select()
      })
    } else {
      errorMessage.value = ""
      token.value = ""
    }
  },
)

function startCountdown(seconds: number) {
  countdown.value = Math.max(1, seconds)
  if (countdownTimer) clearInterval(countdownTimer)
  countdownTimer = setInterval(() => {
    countdown.value--
    if (countdown.value <= 0) {
      if (countdownTimer) clearInterval(countdownTimer)
      countdownTimer = null
      tokenInput.value?.focus()
    }
  }, 1000)
}

async function submit() {
  const value = token.value.trim()
  if (!value) {
    errorMessage.value = "请输入访问令牌"
    tokenInput.value?.focus()
    return
  }
  errorMessage.value = ""
  submitting.value = true
  try {
    await doLogin(value)
    showToast("验证成功", "success")
  } catch (err) {
    const retryAfter = (err as Error & { retryAfter?: number }).retryAfter
    if (retryAfter) {
      startCountdown(retryAfter)
    } else {
      errorMessage.value = (err as Error).message || "验证失败，请重试"
      tokenInput.value?.focus()
      tokenInput.value?.select()
    }
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <Teleport to="body">
    <div id="login-overlay" :hidden="!loginPrompt.open" :aria-hidden="!loginPrompt.open">
      <div class="login-card" role="dialog" aria-modal="true" aria-labelledby="login-title">
        <span class="login-mark">
          <AppIcon name="shield-lock" :size="20" />
        </span>
        <h2 id="login-title">验证访问权限</h2>
        <p>该网盘已开启访问保护，输入访问令牌即可浏览和下载文件。</p>
        <form novalidate @submit.prevent="submit">
          <label for="login-token">访问令牌</label>
          <div class="input-wrap">
            <span class="input-icon"><AppIcon name="key-round" :size="16" /></span>
            <input
              id="login-token"
              ref="tokenInput"
              v-model="token"
              name="access_token"
              type="password"
              autocomplete="current-password"
              required
              placeholder="请输入访问令牌"
            >
          </div>
          <div class="login-actions">
            <button type="submit" class="btn btn-primary" :disabled="submitting || countdown > 0">
              <span class="btn-spinner"><AppIcon name="loader" :size="16" /></span>
              <span class="btn-label">{{ countdown > 0 ? `请等待 ${countdown} 秒` : "验证并进入" }}</span>
            </button>
          </div>
          <div id="login-error" class="login-error" :hidden="!errorMessage">{{ errorMessage }}</div>
        </form>
      </div>
    </div>
  </Teleport>
</template>
