<script setup lang="ts">
// 访客端提取码输入弹窗：等价迁移自 unlock_modal.html
const { unlockRequest } = useFiles()
const { showToast } = useToast()

const visible = computed(() => unlockRequest.value.open)
const password = ref("")
const errorMessage = ref("")
const submitting = ref(false)
const pwdInput = ref<HTMLInputElement | null>(null)

watch(visible, (open) => {
  if (open) {
    password.value = ""
    // 打开时展示上次失败原因（如"提取码错误，请重新输入"）
    errorMessage.value = unlockRequest.value.message
    nextTick(() => pwdInput.value?.focus())
  }
})

async function submit() {
  const value = password.value.trim()
  if (!value) {
    errorMessage.value = "请输入提取码"
    pwdInput.value?.focus()
    return
  }
  submitting.value = true
  try {
    unlockRequest.value.onUnlock(value)
  } finally {
    submitting.value = false
  }
}

function onCancel() {
  unlockRequest.value = { open: false, message: "", onUnlock: () => {} }
  showToast("已取消解锁，返回首页", "info")
  navigateTo("/")
}
</script>

<template>
  <Teleport to="body">
    <div id="unlock-overlay" class="modal-overlay" :hidden="!visible" :aria-hidden="!visible" @click.self="onCancel">
      <div class="unlock-card" role="dialog" aria-modal="true" aria-labelledby="unlock-title">
        <span class="unlock-mark"><AppIcon name="lock" :size="20" /></span>
        <h2 id="unlock-title">请输入提取码</h2>
        <p id="unlock-desc">该分享目录受访问码保护，验证后即可继续浏览</p>
        <form novalidate @submit.prevent="submit">
          <div class="input-wrap">
            <span class="input-icon"><AppIcon name="key-round" :size="16" /></span>
            <input
              id="unlock-pwd-input"
              ref="pwdInput"
              v-model="password"
              type="text"
              maxlength="16"
              autocomplete="off"
              required
              placeholder="请输入提取码"
            >
          </div>
          <div class="login-actions">
            <button type="submit" class="btn btn-primary" :disabled="submitting">
              <span class="btn-icon"><AppIcon name="unlock" :size="16" /></span>
              <span class="btn-label">提取文件</span>
            </button>
          </div>
          <div id="unlock-error" class="login-error" :hidden="!errorMessage">{{ errorMessage }}</div>
        </form>
      </div>
    </div>
  </Teleport>
</template>
