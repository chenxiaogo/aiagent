/**
 * 工具分组（能力市场·工具库的 category 字段）
 * ------------------------------------------------------------
 * 分组的作用有两个：
 *   1. 建工具时归入某一组，避免「运维 / ops / 主机」各写各的导致分组发散
 *   2. 智能体挂载工具时按组浏览、整组勾选，不用在平铺的长表里一个个找
 *
 * 新增分组：在这里加一项即可，工具库表单与挂载界面的分组都会自动跟随。
 */

export const TOOL_CATEGORIES = [
  { value: '检索', label: '检索', icon: '🔍', desc: '知识库 / 视频 / 摄像头等内容检索', color: 'primary' },
  { value: '运维', label: '运维', icon: '🖥️', desc: '主机命令、资源查看、脚本执行', color: 'danger' },
  { value: '文件', label: '文件', icon: '📁', desc: '远程文件读写与传输', color: 'warning' },
  { value: '数据', label: '数据', icon: '📊', desc: '数据库查询与统计分析', color: 'success' },
  { value: '通信', label: '通信', icon: '💬', desc: '消息推送、邮件、Webhook', color: 'info' },
  { value: '效率', label: '效率', icon: '⚡', desc: '报告生成、定时与流程辅助', color: 'warning' },
  { value: '工具', label: '通用', icon: '🧰', desc: '时间、计算等通用能力', color: 'info' },
  { value: '其他', label: '其他', icon: '🧩', desc: '未归类工具', color: 'info' },
]

const DEFAULT_CATEGORY = {
  value: '其他', label: '其他', icon: '🧩', desc: '未归类工具', color: 'info',
}

/** 取分组元信息，未知分组按「其他」处理并保留原名 */
export function toolCategoryMeta(value) {
  const hit = TOOL_CATEGORIES.find(c => c.value === value)
  if (hit) return hit
  if (!value) return DEFAULT_CATEGORY
  return { value, label: value, icon: '🧩', desc: '自定义分组', color: 'info' }
}

/** 分组下拉选项：预设分组 + 数据中已出现的自定义分组（去重、按分组顺序排） */
export function toolCategoryOptions(existing = []) {
  const extra = (existing || [])
    .map(v => String(v || '').trim())
    .filter(Boolean)
    .filter(v => !TOOL_CATEGORIES.some(c => c.value === v))
  return [
    ...TOOL_CATEGORIES,
    ...[...new Set(extra)].sort().map(v => ({
      value: v, label: v, icon: '🏷️', desc: '自定义分组', color: 'info',
    })),
  ]
}
