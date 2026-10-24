// 弹窗编排状态壳：分享弹窗 / 分享管理
// （预览灯箱状态在 usePreview.ts，勿在此重复定义，避免自动导入同名冲突）

export interface ShareTarget {
  type: "file" | "dir"
  path: string
  name: string
}

const shareOpen = ref(false)
const shareTarget = ref<ShareTarget>({ type: "dir", path: "/", name: "" })
const sharesManageOpen = ref(false)
const uploadOpen = ref(false)

export function useShareModals() {
  function openShare(target: ShareTarget) {
    shareTarget.value = target
    shareOpen.value = true
  }
  function closeShare() {
    shareOpen.value = false
  }
  function openSharesManage() {
    sharesManageOpen.value = true
  }
  function closeSharesManage() {
    sharesManageOpen.value = false
  }
  return { shareOpen, shareTarget, openShare, closeShare, sharesManageOpen, openSharesManage, closeSharesManage }
}

export function useUploadModal() {
  function openUpload() {
    uploadOpen.value = true
  }
  function closeUpload() {
    uploadOpen.value = false
  }
  return { uploadOpen, openUpload, closeUpload }
}
