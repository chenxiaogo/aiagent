// 知识库类型：一个知识库只代表一种内容类型，仅有视频 / 摄像头 / 文件三类
export const KB_TYPES = [
  { value: 'video', label: '视频', icon: '🎬' },
  { value: 'camera', label: '摄像头', icon: '📷' },
  { value: 'file', label: '文件', icon: '📄' }
]

export function kbTypeLabel(type) {
  return KB_TYPES.find(t => t.value === type)?.label || '通用'
}

export function kbTypeIcon(type) {
  return KB_TYPES.find(t => t.value === type)?.icon || '🗂️'
}
