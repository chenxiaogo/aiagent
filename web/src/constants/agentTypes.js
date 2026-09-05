/**
 * 智能体类型注册表
 * ------------------------------------------------------------
 * 新增一种智能体类型，只需：
 *   1. 在 AGENT_TYPES 里加一项
 *   2. 若是全新形态，写对应 workspace 组件并在 meta.component 里懒加载引入
 * 无需改动路由、菜单、工作台容器逻辑。
 *
 * workspace 形态（shape）说明：
 *   - 'search'：素材检索型（左列表 + 右预览），如视频 / 摄像头 / 文档
 *   - 'chat'  ：对话型（多轮问答），如通用助手
 *   - 'report'：报告型（对话 + 报告生成/管理），报告归属于该智能体
 *   - 'custom'：完全自定义组件，后续扩展（会议纪要等）
 */

// 各形态对应的工作区组件（懒加载，避免首屏打包过大）
const workspaceLoaders = {
  search: () => import('@/components/search/AssetSearch.vue'),
  chat: () => import('@/components/chat/ChatPanel.vue'),
  report: () => import('@/components/report/ReportWorkspace.vue'),
  ops: () => import('@/views/agent/OpsWorkspace.vue')
}

export const AGENT_TYPES = [
  {
    value: 'video',
    label: '视频检索',
    icon: '🎬',
    desc: '视频场景语义检索，定位关键画面',
    shape: 'search',
    color: 'primary',
    // 检索型需要告知 AssetSearch 用哪种数据源
    source: 'video',
    placeholder: '描述画面内容… 例如：穿红色外套的男性走过门前',
    emptyHint: '输入描述，检索视频场景片段'
  },
  {
    value: 'camera',
    label: '摄像头检索',
    icon: '📷',
    desc: '摄像头事件混合检索，支持时间/人物/车辆等条件',
    shape: 'search',
    color: 'warning',
    source: 'camera',
    placeholder: '描述你要找的事件… 例如：昨天下午有人在门口取包裹',
    emptyHint: '输入描述，检索摄像头事件'
  },
  {
    value: 'doc',
    label: '文档检索',
    icon: '📄',
    desc: '文档向量 + 全文混合检索',
    shape: 'search',
    color: 'success',
    source: 'doc',
    placeholder: '输入关键词或问题… 例如：部署步骤',
    emptyHint: '输入关键词，检索文档内容'
  },
  {
    value: 'report',
    label: '报告生成',
    icon: '📊',
    desc: '基于数据与检索结果生成分析报告，报告归属于该智能体',
    shape: 'report',
    color: 'success',
    source: '',
    placeholder: '',
    emptyHint: '点击「生成报告」创建第一份报告'
  },
  {
    value: 'ops',
    label: '运维助手',
    icon: '🖥️',
    desc: '左侧主机树按主机/主机组开会话，下方切换 Agent 轨迹与 SSH 终端',
    shape: 'ops',
    color: 'primary',
    source: '',
    placeholder: '输入运维指令，例如：查看服务器磁盘使用情况…',
    emptyHint: ''
  },
  {
    value: 'general',
    label: '通用对话',
    icon: '💬',
    desc: '纯多轮对话，适合问答与创作',
    shape: 'chat',
    color: 'info',
    source: '',
    placeholder: '输入消息，开始对话…',
    emptyHint: ''
  }
]

// 兜底类型：未知 category 一律按通用对话处理，保证不白屏
export const DEFAULT_AGENT_TYPE = AGENT_TYPES.find(t => t.value === 'general')

/**
 * 按类型值取配置（带兜底）
 * @param {string} value 类型值
 * @returns {object} 类型配置
 */
export function getAgentType(value) {
  return AGENT_TYPES.find(t => t.value === value) || DEFAULT_AGENT_TYPE
}

/**
 * 取该类型对应的工作区组件加载器
 * @param {string} value 类型值
 * @returns {Function} 动态 import 函数
 */
export function getWorkspaceLoader(value) {
  const type = getAgentType(value)
  return workspaceLoaders[type.shape] || workspaceLoaders.chat
}

/**
 * 用于下拉选择的选项（供表单使用）
 */
export function agentTypeOptions() {
  return AGENT_TYPES.map(t => ({
    value: t.value,
    label: t.label,
    icon: t.icon,
    desc: t.desc
  }))
}
