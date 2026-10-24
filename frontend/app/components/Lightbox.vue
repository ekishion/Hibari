<script setup lang="ts">
// 预览灯箱 + 自绘媒体播放器：等价迁移自 lightbox.html + preview.js
import { renderMarkdownInto } from '~/utils/markdown'

const { state, closePreview, navigatePreview } = usePreview()
const { requireLogin } = useAuth()
const { showToast } = useToast()

const lastFocused = ref<HTMLElement | null>(null)

const imageEl = ref<HTMLImageElement | null>(null)
const textEl = ref<HTMLElement | null>(null)
const mdBox = ref<HTMLElement | null>(null)
const videoEl = ref<HTMLVideoElement | null>(null)
const audioEl = ref<HTMLAudioElement | null>(null)
const seekEl = ref<HTMLElement | null>(null)
const lightboxEl = ref<HTMLElement | null>(null)

// ===== 媒体播放器状态 =====
const playing = ref(false)
const muted = ref(false)
const fullscreen = ref(false)
const currentTime = ref(0)
const duration = ref(0)
const mediaSeeking = ref(false)

const activeMedia = computed(() => {
  if (state.kind === "video") return videoEl.value
  if (state.kind === "audio") return audioEl.value
  return null
})

const mediaPercent = computed(() =>
  Number.isFinite(duration.value) && duration.value > 0
    ? Math.min(100, (currentTime.value / duration.value) * 100)
    : 0,
)

function formatMediaTime(sec: number): string {
  if (!Number.isFinite(sec) || sec < 0) return "0:00"
  const s = Math.floor(sec % 60)
  const m = Math.floor(sec / 60) % 60
  const h = Math.floor(sec / 3600)
  const mm = h ? String(m).padStart(2, "0") : String(m)
  return (h ? h + ":" : "") + mm + ":" + String(s).padStart(2, "0")
}

const mediaTimeText = computed(() => formatMediaTime(currentTime.value) + " / " + formatMediaTime(duration.value))

function syncMediaUI() {
  const el = activeMedia.value
  if (!el) return
  currentTime.value = el.currentTime
  duration.value = Number.isFinite(el.duration) ? el.duration : 0
}

function toggleMediaPlay() {
  const el = activeMedia.value
  if (!el) return
  if (el.paused || el.ended) {
    el.play()?.catch(() => {})
  } else {
    el.pause()
  }
}

function seekMediaBy(delta: number) {
  const el = activeMedia.value
  if (!el || !Number.isFinite(el.duration)) return
  el.currentTime = Math.min(el.duration, Math.max(0, el.currentTime + delta))
  syncMediaUI()
}

function onMediaError() {
  state.spinner = false
  if (activeMedia.value?.error) {
    showToast("媒体加载失败，请稍后重试", "error")
  }
}

function onSeekPointerDown(event: PointerEvent) {
  mediaSeeking.value = true
  const target = event.target as HTMLElement
  target.setPointerCapture?.(event.pointerId)
  seekFromEvent(event)
}

function seekFromEvent(event: PointerEvent) {
  const el = activeMedia.value
  const seek = seekEl.value
  if (!el || !seek || !Number.isFinite(el.duration) || el.duration <= 0) return
  const rect = seek.getBoundingClientRect()
  if (!rect.width) return
  const ratio = Math.min(1, Math.max(0, (event.clientX - rect.left) / rect.width))
  el.currentTime = ratio * el.duration
  syncMediaUI()
}

function toggleMediaMute() {
  const el = activeMedia.value
  if (!el) return
  el.muted = !el.muted
  muted.value = el.muted
}

function isMediaFullscreen(el: HTMLVideoElement | null): boolean {
  if (document.fullscreenElement || (document as Document & { webkitFullscreenElement?: Element }).webkitFullscreenElement) {
    return true
  }
  if (el && (el as HTMLVideoElement & { webkitDisplayingFullscreen?: boolean }).webkitDisplayingFullscreen) {
    return true
  }
  return false
}

function toggleMediaFullscreen() {
  const el = videoEl.value
  if (!el || state.kind !== "video") return
  type FullscreenVideo = HTMLVideoElement & {
    webkitEnterFullscreen?: () => void
    webkitRequestFullscreen?: () => void
    webkitExitFullscreen?: () => void
  }
  const fv = el as FullscreenVideo
  if (isMediaFullscreen(el)) {
    if (document.exitFullscreen) {
      document.exitFullscreen()?.catch(() => {})
    } else {
      ;(document as Document & { webkitExitFullscreen?: () => void }).webkitExitFullscreen?.()
      fv.webkitExitFullscreen?.()
    }
  } else if (el.requestFullscreen) {
    el.requestFullscreen()?.catch(() => {
      if (fv.webkitEnterFullscreen) {
        try {
          fv.webkitEnterFullscreen()
        } catch {
          // 保持静默
        }
      }
    })
  } else if (fv.webkitRequestFullscreen) {
    try {
      fv.webkitRequestFullscreen()
    } catch {
      fv.webkitEnterFullscreen?.()
    }
  } else {
    fv.webkitEnterFullscreen?.()
  }
  setTimeout(syncFullscreenUI, 100)
}

function syncFullscreenUI() {
  fullscreen.value = isMediaFullscreen(videoEl.value)
}

// ===== 文本/Markdown 渲染 =====
watch(
  () => [state.open, state.kind, state.textContent] as const,
  ([open, kind, content]) => {
    if (!open || kind !== "text") return
    nextTick(() => {
      if (state.textIsMarkdown && mdBox.value) {
        mdBox.value.replaceChildren()
        renderMarkdownInto(mdBox.value, content)
      }
    })
  },
  { immediate: true },
)

// ===== 生命周期与全局事件 =====
watch(
  () => state.open,
  (open) => {
    if (open) {
      if (document.activeElement && document.activeElement !== document.body) {
        lastFocused.value = document.activeElement as HTMLElement
      }
      document.body.classList.add("lightbox-open")
      nextTick(() => (lightboxEl.value?.querySelector(".lightbox-close") as HTMLElement | null)?.focus())
      // 视频自动播放（等价 loadMediaPreview 尾部行为）
      if (state.kind === "video") {
        nextTick(() => {
          videoEl.value?.play()?.catch(() => {})
        })
      }
    } else {
      document.body.classList.remove("lightbox-open")
      const el = lastFocused.value
      lastFocused.value = null
      try {
        el?.focus()
      } catch {
        // ignore
      }
    }
  },
)

// 媒体加载完成后收起 spinner（探测成功即隐藏，避免等待元数据期间遮罩闪烁）
watch(
  () => [state.kind, state.mediaSrc] as const,
  ([kind, src]) => {
    if (kind === "video" || kind === "audio") {
      if (src) state.spinner = false
    }
  },
)

onMounted(() => {
  document.addEventListener("fullscreenchange", syncFullscreenUI)
  document.addEventListener("webkitfullscreenchange", syncFullscreenUI)
})

onBeforeUnmount(() => {
  document.removeEventListener("fullscreenchange", syncFullscreenUI)
  document.removeEventListener("webkitfullscreenchange", syncFullscreenUI)
  document.removeEventListener("keydown", onDocumentKeydown)
})

// 文档级 Escape（等价 document.addEventListener keydown Escape closeLightbox）
function onDocumentKeydown(event: KeyboardEvent) {
  if (event.key === "Escape") closePreview()
}
watch(() => state.open, (open) => {
  if (open) document.addEventListener("keydown", onDocumentKeydown)
  else document.removeEventListener("keydown", onDocumentKeydown)
})

// ===== 灯箱内键盘交互 =====
function trapFocusWithin(event: KeyboardEvent, container: HTMLElement) {
  const focusables = container.querySelectorAll<HTMLElement>(
    'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])',
  )
  const visible = Array.from(focusables).filter((el) => el.offsetParent !== null || el === document.activeElement)
  if (!visible.length) return
  const first = visible[0]
  const last = visible[visible.length - 1]
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault()
    last.focus()
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault()
    first.focus()
  }
}

function onLightboxKeydown(event: KeyboardEvent) {
  const box = lightboxEl.value
  if (event.key === "Tab" && box) {
    trapFocusWithin(event, box)
    return
  }
  const isMedia = state.kind === "video" || state.kind === "audio"
  if (isMedia && event.key === " ") {
    event.preventDefault()
    toggleMediaPlay()
    return
  }
  if (isMedia && event.key === "ArrowLeft") {
    event.preventDefault()
    seekMediaBy(-5)
    return
  }
  if (isMedia && event.key === "ArrowRight") {
    event.preventDefault()
    seekMediaBy(5)
    return
  }
  if (state.navList.length > 1 && event.key === "ArrowLeft") {
    event.preventDefault()
    navigatePreview(-1)
  }
  if (state.navList.length > 1 && event.key === "ArrowRight") {
    event.preventDefault()
    navigatePreview(1)
  }
}

// ===== 图片缩放 =====
function onImageClick(event: MouseEvent) {
  const image = imageEl.value
  if (!image) return
  if (image.classList.contains("is-zoomed")) {
    image.classList.remove("is-zoomed")
    image.style.transformOrigin = ""
    return
  }
  const rect = image.getBoundingClientRect()
  if (!rect.width || !rect.height) return
  const x = ((event.clientX - rect.left) / rect.width) * 100
  const y = ((event.clientY - rect.top) / rect.height) * 100
  image.style.transformOrigin = x + "% " + y + "%"
  image.classList.add("is-zoomed")
}

// ===== 移动端滑动切图 =====
let touchStartX = 0
let touchStartY = 0

function onTouchStart(event: TouchEvent) {
  if (event.touches.length !== 1) return
  touchStartX = event.touches[0].clientX
  touchStartY = event.touches[0].clientY
}

function onTouchEnd(event: TouchEvent) {
  if (state.kind !== "image" || state.navList.length < 2) return
  const touch = event.changedTouches[0]
  if (!touch) return
  const dx = touch.clientX - touchStartX
  const dy = touch.clientY - touchStartY
  // 水平位移足够大且明显大于纵向才判定为滑动，避免误伤缩放点击
  if (Math.abs(dx) > 48 && Math.abs(dx) > Math.abs(dy) * 2) {
    navigatePreview(dx < 0 ? 1 : -1)
  }
}

const showNav = computed(() => state.kind === "image" && state.navList.length > 1)
const captionText = computed(() => (state.truncated ? state.name + "（内容过长，已截断显示）" : state.name))
const mediaErrorText = ref("")
</script>

<template>
  <Teleport to="body">
    <div
      id="image-lightbox"
      ref="lightboxEl"
      :hidden="!state.open"
      :aria-hidden="!state.open"
      role="dialog"
      aria-modal="true"
      aria-labelledby="lightbox-caption"
      @click.self="closePreview"
      @keydown="onLightboxKeydown"
      @touchstart.passive="onTouchStart"
      @touchend.passive="onTouchEnd"
    >
      <button id="lightbox-close" class="lightbox-close" type="button" aria-label="关闭预览" @click="closePreview">
        <AppIcon name="x" :size="18" />
      </button>
      <button v-if="showNav" id="lightbox-prev" class="lightbox-nav lightbox-prev" type="button" aria-label="上一张" :disabled="state.navIndex <= 0" @click="navigatePreview(-1)">
        <AppIcon name="chevron-left" :size="20" />
      </button>
      <button v-if="showNav" id="lightbox-next" class="lightbox-nav lightbox-next" type="button" aria-label="下一张" :disabled="state.navIndex >= state.navList.length - 1" @click="navigatePreview(1)">
        <AppIcon name="chevron-right" :size="20" />
      </button>
      <div id="lightbox-stage">
        <div id="lightbox-spinner" class="lightbox-spinner" :hidden="!state.spinner" aria-label="正在加载">
          <div class="spinner"><AppIcon name="loader" /></div>
        </div>
        <img
          v-show="state.kind === 'image' && state.imageUrl"
          id="lightbox-image"
          ref="imageEl"
          :src="state.kind === 'image' ? state.imageUrl : ''"
          :alt="state.name"
          @load="state.spinner = false"
          @error="state.spinner = false"
          @click="onImageClick"
        >
        <div v-show="state.kind === 'text' && state.textIsMarkdown" id="lightbox-markdown" ref="mdBox" class="md-content" />
        <pre v-show="state.kind === 'text' && !state.textIsMarkdown" id="lightbox-text" ref="textEl">{{ state.textContent }}</pre>
        <div id="lightbox-media" class="lightbox-media" :hidden="state.kind !== 'video' && state.kind !== 'audio'">
          <div id="media-disc" class="media-disc" :hidden="state.kind !== 'audio'" :class="{ 'is-playing': playing }">
            <span class="media-disc-dot" aria-hidden="true" />
            <span class="media-disc-icon"><AppIcon name="music" :size="20" /></span>
          </div>
          <video
            v-show="state.kind === 'video'"
            id="lightbox-video"
            ref="videoEl"
            playsinline
            webkit-playsinline="true"
            x5-playsinline="true"
            :src="state.kind === 'video' ? state.mediaSrc : ''"
            @click="toggleMediaPlay"
            @timeupdate="syncMediaUI"
            @play="playing = true"
            @pause="playing = false"
            @ended="playing = false"
            @loadedmetadata="syncMediaUI"
            @loadeddata="state.spinner = false"
            @canplay="state.spinner = false"
            @playing="state.spinner = false"
            @error="onMediaError"
          />
          <audio
            v-show="state.kind === 'audio'"
            id="lightbox-audio"
            ref="audioEl"
            :src="state.kind === 'audio' ? state.mediaSrc : ''"
            @timeupdate="syncMediaUI"
            @play="playing = true"
            @pause="playing = false"
            @ended="playing = false"
            @loadedmetadata="syncMediaUI"
            @loadeddata="state.spinner = false"
            @canplay="state.spinner = false"
            @playing="state.spinner = false"
          />
          <div class="media-bar">
            <button type="button" class="media-btn" :aria-label="playing ? '暂停' : '播放'" @click="toggleMediaPlay">
              <AppIcon :name="playing ? 'pause' : 'play'" :size="16" />
            </button>
            <div
              id="media-seek"
              ref="seekEl"
              class="media-seek"
              role="slider"
              tabindex="0"
              aria-label="播放进度"
              :aria-valuemin="0"
              :aria-valuemax="Math.round(duration)"
              :aria-valuenow="Math.round(currentTime)"
              :aria-valuetext="mediaTimeText"
              @pointerdown="onSeekPointerDown($event)"
              @pointermove="mediaSeeking && seekFromEvent($event)"
              @pointerup="mediaSeeking = false"
              @pointercancel="mediaSeeking = false"
            >
              <div class="media-track">
                <div id="media-fill" class="media-fill" :style="{ width: mediaPercent + '%' }" />
                <div id="media-thumb" class="media-thumb" :style="{ left: mediaPercent + '%' }" />
              </div>
            </div>
            <span id="media-time" class="media-time">{{ mediaTimeText }}</span>
            <button type="button" class="media-btn" :aria-label="muted ? '取消静音' : '静音'" @click="toggleMediaMute">
              <AppIcon :name="muted ? 'volume-x' : 'volume-2'" :size="16" />
            </button>
            <button
              v-if="state.kind === 'video'"
              id="media-fs"
              type="button"
              class="media-btn"
              :aria-label="fullscreen ? '退出全屏' : '全屏'"
              @click="toggleMediaFullscreen"
            >
              <AppIcon :name="fullscreen ? 'minimize' : 'maximize'" :size="16" />
            </button>
          </div>
        </div>
      </div>
      <div id="lightbox-caption">{{ captionText }}</div>
    </div>
  </Teleport>
</template>
