/** 审计事件的中文名。未知事件回退到原始 code，避免新事件显示为空。 */
const LABELS: Record<string, string> = {
  setup: '初始化',
  login: '登录',
  'password.change': '修改密码',
  mkdir: '新建目录',
  rename: '重命名',
  remove: '删除',
  move: '移动',
  copy: '复制',
  upload: '上传',
  'share.create': '创建分享',
  'share.revoke': '撤销分享',
  'share.download': '分享下载',
  'share.browse': '分享访问',
}

export function eventLabel(event: string): string {
  return LABELS[event] ?? event
}
