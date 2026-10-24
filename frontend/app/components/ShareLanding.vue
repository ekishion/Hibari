<script setup lang="ts">
// 单文件分享落地页：等价迁移自 share_page.js（提取码验证/失效卡片/直接下载）
// 旧版的"在线预览"按钮依赖管理员鉴权的预览端点，对访客始终 401，迁移时移除。

interface ShareInfo {
  token: string
  file_name: string
  has_password?: boolean
  password_valid?: boolean
  size?: number
  expires_at?: number
  max_uses?: number
  uses?: number
  download_url?: string
}

const props = defineProps<{ token: string }>()

const route = useRoute()
const { showToast } = useToast()

type CardState =
  | { kind: "loading" }
  | { kind: "error"; title: string; message: string }
  | { kind: "password"; fileName: string; error: string }
  | { kind: "ready"; info: ShareInfo }

const card = ref<CardState>({ kind: "loading" })
const pwdInput = ref("")
const verifying = ref(false)
const pwdField = ref<HTMLInputElement | null>(null)

function shake(el: HTMLElement | null) {
  if (!el) return
  el.classList.remove("is-shake")
  void el.offsetWidth
  el.classList.add("is-shake")
}

onMounted(() => {
  if (!props.token) {
    card.value = { kind: "error", title: "参数错误", message: "无效的分享链接地址" }
    return
  }
  const rememberedPwd = (route.query.pwd as string) || getStoredSharePassword(props.token)
  fetchShareInfo(props.token, rememberedPwd)
})

async function fetchShareInfo(token: string, password: string) {
  card.value = { kind: "loading" }
  try {
    const resp = await apiFetch("/api/share/info", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ token, password: password || "" }),
    })
    if (resp.status === 404) {
      card.value = { kind: "error", title: "下载链接已失效", message: "该分享链接可能已过期、已被创建者取消或已达到最大访问次数。" }
      return
    }
    if (resp.status === 401) {
      card.value = { kind: "password", fileName: "", error: password ? "提取码错误，请重新输入" : "" }
      return
    }
    if (!resp.ok) {
      const text = await resp.text()
      card.value = { kind: "error", title: "获取信息失败", message: await parseAPIError(resp, text) }
      return
    }
    const data: ShareInfo = await resp.json()
    if (data.has_password && !data.password_valid) {
      card.value = { kind: "password", fileName: data.file_name, error: "" }
      nextTick(() => pwdField.value?.focus())
      return
    }
    if (password) setStoredSharePassword(token, password)
    card.value = { kind: "ready", info: data }
  } catch {
    card.value = { kind: "error", title: "网络异常", message: "无法连接至轻盘服务，请刷新重试" }
  }
}

async function submitPassword() {
  if (card.value.kind !== "password") return
  const val = pwdInput.value.trim()
  if (!val) {
    shake(pwdField.value)
    return
  }
  verifying.value = true
  try {
    const resp = await apiFetch("/api/share/info", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ token: props.token, password: val }),
    })
    if (resp.status === 401) {
      card.value = { kind: "password", fileName: card.value.fileName, error: "提取码错误，请重新核对后输入" }
      shake(pwdField.value)
      pwdField.value?.focus()
      return
    }
    if (!resp.ok) {
      const text = await resp.text()
      card.value = { kind: "password", fileName: card.value.fileName, error: await parseAPIError(resp, text) }
      return
    }
    const data: ShareInfo = await resp.json()
    setStoredSharePassword(props.token, val)
    card.value = { kind: "ready", info: data }
  } catch (err) {
    showToast("验证失败：" + ((err as Error).message || "未知错误"), "error")
  } finally {
    verifying.value = false
  }
}

function startDownload(downloadUrl?: string) {
  if (!downloadUrl) return
  triggerDownloadNavigation(downloadUrl)
}
</script>

<template>
  <div class="container" id="share-landing">
    <div class="share-landing-wrapper">
      <div v-if="card.kind === 'loading'" class="share-landing-card">
        <div class="spinner landing-loading-spinner"><AppIcon name="loader" /></div>
        <p class="landing-desc">正在准备文件信息…</p>
      </div>

      <div v-else-if="card.kind === 'error'" class="share-landing-card is-error">
        <div class="landing-squircle-badge is-warning"><AppIcon name="link-2-off" :size="22" /></div>
        <h1 class="landing-title">{{ card.title }}</h1>
        <p class="landing-desc">{{ card.message }}</p>
        <div class="landing-actions">
          <NuxtLink to="/" class="btn btn-primary btn-block">返回网盘首页</NuxtLink>
        </div>
      </div>

      <div v-else-if="card.kind === 'password'" class="share-landing-card">
        <div class="landing-squircle-badge"><AppIcon name="lock" :size="22" /></div>
        <h1 class="landing-title">{{ card.fileName || "受密码保护的文件" }}</h1>
        <p class="landing-desc">该分享链接已设置提取码，请输入访问码提取并下载。</p>
        <form class="landing-form" novalidate @submit.prevent="submitPassword">
          <div class="landing-input-wrap">
            <input
              ref="pwdField"
              v-model="pwdInput"
              type="text"
              name="pwd"
              class="landing-password-input"
              placeholder="请输入 6 位提取码"
              autocomplete="off"
              maxlength="16"
              autofocus
            >
          </div>
          <div class="landing-error-tip" :hidden="!card.error">{{ card.error }}</div>
          <button type="submit" class="btn btn-primary btn-block" :disabled="verifying">
            <span class="btn-spinner"><AppIcon name="loader" :size="16" /></span>
            <span class="btn-label">{{ verifying ? "正在验证…" : "立即提取并下载" }}</span>
          </button>
        </form>
      </div>

      <div v-else class="share-landing-card">
        <div class="landing-squircle-badge">
          <AppIcon :name="fileIconMeta(card.info.file_name, false).name" :size="22" />
        </div>
        <h1 class="landing-title">{{ card.info.file_name || "文件下载" }}</h1>
        <div class="landing-meta-tags">
          <span v-if="card.info.size" class="landing-meta-tag">{{ formatSize(card.info.size) }}</span>
          <span v-if="card.info.has_password" class="landing-meta-tag is-badge">提取码已验证</span>
          <span v-if="(card.info.max_uses || 0) > 0" class="landing-meta-tag">限次下载</span>
        </div>
        <div class="landing-actions">
          <button type="button" class="btn btn-primary btn-block btn-lg" @click="startDownload(card.info.download_url)">
            <span class="btn-icon"><AppIcon name="download" :size="16" /></span>
            <span class="btn-label">立即下载</span>
          </button>
          <NuxtLink to="/" class="btn btn-ghost btn-block">进入网盘首页</NuxtLink>
        </div>
      </div>
    </div>
  </div>
</template>
