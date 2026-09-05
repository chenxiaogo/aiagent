/**
 * 平台侧边栏菜单（两级）
 * ------------------------------------------------------------
 * 目标结构（随 R2~R4 逐步点亮）：
 *
 *   智能体        /agents            （详情页 /agents/:id：版本 / 发布与交付 / 配置 / MCP / 技能 …）
 *   客户          /customers         （客户管理 / 授权与订阅 / 套餐 / 凭据 / 用量计费）  R2
 *   能力市场      /market            （MCP 注册表 / 技能库 / Agent 模板）R3 ✅ 已落地
 *   数据          （已收归智能体：视频源→视频类 Agent、摄像头→摄像头类、知识库→文档类、文件→通用类，均在对应 Agent 详情 Tab）
 *   运行观测      /ops               （调用观测 / 模型路由：LLM·MCP·向量化 / 错误 / 成本 / 延迟）  R4 ✅ 已落地
 *   系统          /settings          （模型管理 / 模型路由 / 提示词 / 策略 / 用户角色）
 *
 * 说明：检索 / 对话 / 报告这三类能力已收敛为「智能体的形态」，
 * 入口统一在智能体工作台 /agent/:id（检索型、对话型、报告型），
 * 平台侧不再单列「智能检索」「通用对话」「智能报告」页面。
 *
 * 约定：菜单项只登记「已经有页面的」条目，避免占位死链；
 * 新页面上线时在这里加一项即可，侧边栏与面包屑自动跟随。
 */

export const MENUS = [
  { path: '/agents', title: '智能体管理', icon: 'Cpu' },
  {
    path: '/market',
    title: '能力市场',
    icon: 'Box',
    children: [
      { path: '/market/knowledge', title: '知识库' },
      { path: '/market/mcp', title: 'MCP 注册表' },
      { path: '/market/skills', title: '技能库' },
      { path: '/market/tools', title: '工具库' },
      { path: '/market/prompts', title: '提示词库' }
    ]
  },
  {
    path: '/ops',
    title: '运行观测',
    icon: 'Monitor',
    children: [
      { path: '/ops/call-logs', title: '调用观测' },
      { path: '/ops/models', title: '模型路由' }
    ]
  },
  {
    path: '/settings',
    title: '系统',
    icon: 'Setting',
    children: [
      { path: '/settings/models', title: '大模型配置' }
    ]
  }
]

/**
 * 当前路径是否属于某个一级菜单（用于侧边栏展开态）
 */
export function isMenuActive(menu, currentPath) {
  if (menu.children?.length) {
    return menu.children.some(c => currentPath.startsWith(c.path))
  }
  return currentPath.startsWith(menu.path)
}
