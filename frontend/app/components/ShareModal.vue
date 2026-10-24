<script setup lang="ts">
// 创建分享弹窗：等价迁移自 share_modal.html + share_modal.js
// （文件→/api/download 短链；目录→/api/dir-link；成功后展示链接/提取码/二维码）

import { renderTo } from '~/utils/qrcode'

const { shareOpen, shareTarget, closeShare, openSharesManage } = useShareModals()
const { uiConfig } = useAuth()
const { showToast } = useToast()

const visible = computed(() => shareOpen.value)
const isDir = computed(() => shareTarget.value.type === "dir")

const formPanelVisible = ref(true)
const pwdEnabled = ref(true)
const password = ref("")
const expireSeconds = ref("604800")
const maxUses = ref("0")
const submitting = ref(false)

const resultVisible = ref(false)
const resultUrl = ref("")
const resultFullUrl = ref("")
const resultPassword = ref("")

const targetIcon = computed(() => fileIconMeta(shareTarget.value.name || "", isDir.value).name)
const pwdInput = ref<HTMLInputElement | null>(null)
const qrBox = ref<HTMLElement | null>(null)

const EXPIRE_OPTIONS = [
  { value: "86400", label: "1 天有效" },
  { value: "604800", label: "7 天有效" },
  { value: "2592000", label: "30 天有效" },
  { value: "0", label: "永久有效" },
]
const USES_OPTIONS = [
  { value: "0", label: "不限次数" },
  { value: "1", label: "仅限 1 次 (阅后即焚)" },
  { value: "5", label: "限制 5 次" },
  { value: "10", label: "限制 10 次" },
]

watch(visible, (open) => {
  if (!open) return
  formPanelVisible.value = true
  resultVisible.value = false
  pwdEnabled.value = true
  password.value = generateRandomSharePin(uiConfig.sharePasswordLength)
  expireSeconds.value = "604800"
  maxUses.value = "0"
  submitting.value = false
  nextTick(() => pwdInput.value?.focus())
})

watch(password, () => {
  if (resultVisible.value) resultPassword.value = password.value
})

function generateRandomSharePin(len: number): string {
  const chars = "23456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
  const n = len || 6
  let res = ""
  if (window.crypto?.getRandomValues) {
    const buf = new Uint8Array(n)
    window.crypto.getRandomValues(buf)
    for (let i = 0; i < n; i++) res += chars[buf[i] % chars.length]
  } else {
    for (let i = 0; i < n; i++) res += chars[Math.floor(Math.random() * chars.length)]
  }
  return res
}

function randomizePin() {
  password.value = generateRandomSharePin(uiConfig.sharePasswordLength)
  pwdInput.value?.focus()
}

function onTogglePwd() {
  if (pwdEnabled.value && !password.value) {
    password.value = generateRandomSharePin(uiConfig.sharePasswordLength)
    pwdInput.value?.focus()
  }
}

async function createShare() {
  if (!shareTarget.value) return
  submitting.value = true
  const hasPwd = pwdEnabled.value
  const pwd = hasPwd ? password.value.trim() : ""
  const expire = parseInt(expireSeconds.value, 10) || 0
  const endpoint = isDir.value ? "/api/dir-link" : "/api/download"
  const payload = isDir.value
    ? { dir: shareTarget.value.path, password: pwd, expire_seconds: expire }
    : { path: shareTarget.value.path, password: pwd, expire_seconds: expire, max_uses: parseInt(maxUses.value, 10) || 0 }

  try {
    const resp = await apiFetch(endpoint, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    })
    if (!resp.ok) {
      const text = await resp.text()
      throw new Error(await parseAPIError(resp, text))
    }
    const data = await resp.json()
    showResult(data, pwd)
  } catch (err) {
    showToast("创建分享失败：" + ((err as Error).message || "未知错误"), "error")
  } finally {
    submitting.value = false
  }
}

function showResult(data: { url?: string; token?: string }, pwd: string) {
  formPanelVisible.value = false
  resultVisible.value = true
  const relUrl = data.url || (isDir.value ? "/?d=" + data.token : "/d/" + data.token)
  resultFullUrl.value = window.location.origin + relUrl
  resultUrl.value = resultFullUrl.value
  resultPassword.value = pwd
  nextTick(() => {
    if (qrBox.value) {
      renderTo(qrBox.value, resultFullUrl.value, {
        size: 144,
        margin: 1,
        // 二维码模块固定深色：容器底色是写死的白色，跟随主题的
        // currentColor 会在暗色模式下变成白模块白底而无法扫描
        fgColor: "#000000",
        bgColor: "transparent",
      })
    }
  })
}

async function copyShareUrl() {
  ;(await copyText(resultFullUrl.value))
    ? showToast("分享链接已复制到剪贴板", "success")
    : showToast("复制失败，请手动长按复制", "error")
}

async function copyPwd() {
  ;(await copyText(resultPassword.value))
    ? showToast("提取码已复制", "success")
    : showToast("复制失败", "error")
}

async function copyFull() {
  let copyStr =
    "【即刻轻盘分享】\n名称：" + (shareTarget.value.name || shareTarget.value.path || "网盘资源") +
    "\n链接：" + resultFullUrl.value
  if (resultPassword.value) copyStr += "\n提取码：" + resultPassword.value
  ;(await copyText(copyStr))
    ? showToast("完整分享口令已复制，可直接发送", "success")
    : showToast("复制失败，请手动长按复制", "error")
}

defineExpose({ closeShare })
</script>

<template>
  <Teleport to="body">
    <div id="share-overlay" class="modal-overlay" :hidden="!visible" :aria-hidden="!visible" @click.self="closeShare">
      <div class="share-card" role="dialog" aria-modal="true" aria-labelledby="share-dialog-title">
        <div class="share-card-header">
          <div class="share-card-title-group">
            <span class="share-mark"><AppIcon name="share-2" :size="17" /></span>
            <h2 id="share-dialog-title">{{ isDir ? "分享文件夹" : "分享文件" }}</h2>
          </div>
          <button type="button" id="share-close" class="modal-close" aria-label="关闭分享窗口" @click="closeShare">
            <AppIcon name="x" :size="16" />
          </button>
        </div>

        <div class="share-target-card">
          <div class="share-target-icon" id="share-target-icon">
            <AppIcon :name="targetIcon" :size="20" />
          </div>
          <div class="share-target-meta">
            <div class="share-target-name" id="share-target-name">{{ shareTarget.name || shareTarget.path || "未命名" }}</div>
            <div class="share-target-sub" id="share-target-sub">{{ isDir ? "文件夹 · 目录分享" : "单个文件 · 直接外链" }}</div>
          </div>
        </div>

        <div v-if="formPanelVisible" id="share-form-panel" class="share-form-panel">
          <div class="share-form-row">
            <div class="share-form-label-group">
              <label for="share-pwd-toggle" class="share-form-label">提取码保护</label>
              <span class="share-form-desc">仅凭提取码方可浏览或下载</span>
            </div>
            <label class="switch-wrap">
              <input id="share-pwd-toggle" v-model="pwdEnabled" type="checkbox" class="switch-input" @change="onTogglePwd">
              <span class="switch-slider" />
            </label>
          </div>

          <div v-if="pwdEnabled" id="share-pwd-box" class="share-pwd-box">
            <div class="input-with-action">
              <input
                id="share-pwd-input"
                ref="pwdInput"
                v-model="password"
                type="text"
                placeholder="输入 6 位提取码"
                maxlength="16"
                autocomplete="off"
                spellcheck="false"
              >
              <button type="button" class="btn btn-ghost btn-sm" title="随机生成 6 位提取码" @click="randomizePin">
                <span class="btn-icon"><AppIcon name="sparkles" :size="14" /></span>
                <span>随机</span>
              </button>
            </div>
          </div>

          <div class="share-form-row">
            <div class="share-form-label-group">
              <span class="share-form-label">有效期</span>
              <span class="share-form-desc">到期后链接将自动失效</span>
            </div>
            <MiniSelect v-model="expireSeconds" :options="EXPIRE_OPTIONS" label="有效期" />
          </div>

          <div v-if="!isDir" class="share-form-row" id="share-max-uses-row">
            <div class="share-form-label-group">
              <span class="share-form-label">下载次数限制</span>
              <span class="share-form-desc">达到上限后短链自动销毁</span>
            </div>
            <MiniSelect v-model="maxUses" :options="USES_OPTIONS" label="下载次数限制" />
          </div>

          <div class="share-actions">
            <button type="button" id="btn-create-share" class="btn btn-primary btn-block" :class="{ 'is-loading': submitting }" :disabled="submitting" @click="createShare">
              <span class="btn-spinner"><AppIcon name="loader" :size="16" /></span>
              <span class="btn-label">{{ submitting ? "正在生成…" : "生成分享链接与二维码" }}</span>
            </button>
            <div class="share-footer-link-wrap">
              <button type="button" class="btn-link-subtle" @click="closeShare(); openSharesManage()">查看已创建的分享记录</button>
            </div>
          </div>
        </div>

        <div v-else id="share-result-panel" class="share-result-panel">
          <div class="share-link-group">
            <div class="share-link-input-wrap">
              <input type="text" id="share-result-url" :value="resultUrl" readonly aria-label="分享链接">
              <button type="button" id="btn-copy-share-url" class="btn btn-secondary btn-sm" aria-label="复制链接" @click="copyShareUrl">
                <span class="btn-icon"><AppIcon name="copy" :size="14" /></span>
                <span>复制</span>
              </button>
            </div>
            <div v-if="resultPassword" class="share-link-input-wrap" id="share-result-pwd-wrap">
              <div class="share-pwd-input-box">
                <span class="share-pwd-tag">提取码</span>
                <input type="text" id="share-result-pwd" :value="resultPassword" readonly aria-label="提取码">
              </div>
              <button type="button" id="btn-copy-share-pwd" class="btn btn-secondary btn-sm" aria-label="复制提取码" @click="copyPwd">
                <span class="btn-icon"><AppIcon name="copy" :size="14" /></span>
                <span>复制</span>
              </button>
            </div>
          </div>

          <div class="share-qr-wrap">
            <div id="share-qr-container" ref="qrBox" class="share-qr-container" aria-label="分享二维码" />
            <div class="share-qr-tip">手机扫一扫，随时分享与下载</div>
          </div>

          <div class="share-result-actions">
            <button type="button" id="btn-copy-share-full" class="btn btn-primary btn-block" @click="copyFull">
              <span class="btn-icon"><AppIcon name="copy-check" :size="16" /></span>
              <span>复制完整分享文案</span>
            </button>
          </div>
        </div>
      </div>
    </div>
  </Teleport>
</template>
