// 弹窗编排状态壳：分享弹窗 / 分享管理 / 预览灯箱
// （状态与打开/关闭逻辑在对应视图阶段补全，此处先立住跨组件契约）

export interface ShareTarget {
  type: "file" | "dir"
  path: string
  name: string
}

const shareOpen = ref(false)
const shareTarget = ref<ShareTarget>({ type: "dir", path: "/", name: "" })
const sharesManageOpen = ref(false)
const uploadOpen = ref(false)

// 预览灯箱状态
const previewOpen = ref(false)

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

export function usePreview() {
  function openPreview(_filePath: string, _fileName: string) {
    // 视图阶段实现
    previewOpen.value = true
  }
  function closePreview() {
    previewOpen.value = false
  }
  return { previewOpen, openPreview, closePreview }
}
