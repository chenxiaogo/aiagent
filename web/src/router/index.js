import { h } from 'vue'
import { createRouter, createWebHashHistory } from 'vue-router'
import { useUserStore } from '@/stores/user'
import { getMyMenus } from '@/api/menu'
import { getMe } from '@/api/auth'

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    {
      path: '/login',
      name: 'Login',
      component: () => import('@/views/login/Login.vue'),
      meta: { public: true }
    },
    {
      path: '/',
      name: 'Root',
      component: () => import('@/layouts/MainLayout.vue'),
      redirect: '/agents',
      children: [
        // 智能体列表：静态保留一份作为兜底。
        //
        // 不能只靠动态菜单注册 —— 根路径 redirect、catch-all、历史兼容路由都指向 /agents，
        // 而 vue-router 解析 redirect 发生在 beforeEach 之前：动态路由还没注册时
        // /agents 匹配不到，会落到 catch-all 再 redirect 回 /agents，直接爆栈。
        // 这里静态声明后 name 与菜单里的 AgentList 一致，动态注册时会整体替换掉它。
        {
          path: 'agents',
          name: 'AgentList',
          component: () => import('@/views/agent/AgentList.vue'),
          meta: { title: '智能体管理' }
        },
        // 智能体详情：带若干 Tab 子路由。
        // 它不在平台菜单内（菜单只到「智能体管理」列表页），因此这里静态声明，
        // 动态菜单只负责注册带 component 的菜单叶子路由。
        {
          path: 'agents/:id',
          component: () => import('@/views/agent/AgentDetail.vue'),
          redirect: to => ({ path: `${to.path}/config` }),
          children: [
            {
              path: 'config',
              name: 'AgentConfig',
              component: () => import('@/views/agent/tabs/AgentConfig.vue'),
              meta: { title: '基础配置' }
            },
            {
              path: 'versions',
              name: 'AgentVersions',
              component: () => import('@/views/agent/tabs/AgentVersions.vue'),
              meta: { title: '版本' }
            },
            {
              path: 'delivery',
              name: 'AgentDelivery',
              component: () => import('@/views/agent/tabs/AgentDelivery.vue'),
              meta: { title: '发布与交付' }
            },
            {
              path: 'models',
              name: 'AgentModels',
              component: () => import('@/views/agent/tabs/AgentModels.vue'),
              meta: { title: '模型' }
            },
            {
              path: 'skills',
              name: 'AgentSkills',
              component: () => import('@/views/agent/tabs/AgentSkills.vue'),
              meta: { title: '技能' }
            },
            {
              path: 'preset',
              name: 'AgentPreset',
              component: () => import('@/views/agent/tabs/AgentPreset.vue'),
              meta: { title: '预设问题' }
            },
            {
              path: 'resources',
              name: 'AgentResources',
              component: () => import('@/views/agent/tabs/AgentResources.vue'),
              meta: { title: '资源' }
            }
          ]
        }
      ]
    },
    // 智能体工作台：独立全屏页面（不套管理平台布局），一个智能体即一个独立应用
    {
      path: '/agent/:id',
      name: 'AgentWorkspace',
      component: () => import('@/views/agent/AgentWorkspace.vue')
    },
    // 历史路径兼容（数据类功能已收归智能体详情，旧书签退回智能体列表）
    { path: '/videos', redirect: '/agents' },
    { path: '/files', redirect: '/agents' },
    { path: '/knowledge', redirect: '/agents' },
    { path: '/settings', redirect: '/settings/models' },
    { path: '/settings/prompts', redirect: '/market/prompts' },
    {
      // 兜底路由刻意不做 redirect：redirect 到某个「可能未注册」的路径时，
      // 会再次落回这里形成死循环（Maximum call stack size exceeded）。
      // 直接渲染空状态，既断掉循环，也能承载「无权限 / 路径不存在」两种语义。
      path: '/:pathMatch(.*)*',
      name: 'NotFound',
      component: {
        render: () => h('div', { class: 'route-fallback' }, '页面不存在或你没有访问权限')
      }
    }
  ]
})

// 前端视图组件映射：component 字符串 → 动态 import
const viewModules = import.meta.glob('../views/**/*.vue')

function loadView(component) {
  if (!component) return () => import('@/views/agent/AgentList.vue')
  const normalized = component.replace(/^view\//, '').replace(/\.vue$/, '')
  const key = `../views/${normalized}.vue`
  if (viewModules[key]) return viewModules[key]
  // 取末段文件名（如 AgentList）做大小写不敏感模糊匹配
  const last = normalized.split('/').pop()
  if (last) {
    const lower = last.toLowerCase()
    const match = Object.keys(viewModules).find((k) => {
      const base = k.split('/').pop().replace(/\.vue$/, '')
      return base.toLowerCase() === lower
    })
    if (match) return viewModules[match]
  }
  // 兼容带前缀的路径（含大小写不敏感）
  const lc = normalized.toLowerCase()
  const broad = Object.keys(viewModules).find((k) => {
    const base = k.replace(/^\.\.\/views\//, '').replace(/\.vue$/, '').toLowerCase()
    return base === lc
  })
  if (broad) return viewModules[broad]
  // 兜底智能体列表
  return () => import('@/views/agent/AgentList.vue')
}

// 将后端菜单树转成 vue-router 子路由。
// 仅对带 component 的叶子菜单注册路由（目录/分组不注册路由，避免嵌套 path 拼接问题）；
// 侧边栏的子菜单展开结构由 MainLayout 依据菜单树渲染，路由本身是扁平的。
function menuToRoutes(menus) {
  const result = []
  const walk = (list) => {
    for (const m of list) {
      if (m.children?.length) {
        walk(m.children)
      } else if (m.component) {
        result.push({
          path: m.path.replace(/^\//, ''),
          name: m.name,
          meta: {
            title: m.title,
            icon: m.icon,
            hidden: m.hidden,
            perm: m.permCode,
            btns: m.btns || []
          },
          component: loadView(m.component)
        })
      }
    }
  }
  walk(menus)
  return result
}

// 根据菜单树动态注册路由（同名路由先移除再注册，支持刷新菜单后覆盖）
export function setupDynamicRoutes(menus) {
  const dynamic = menuToRoutes(menus)
  for (const r of dynamic) {
    if (r.name && router.hasRoute(r.name)) {
      router.removeRoute(r.name)
    }
    router.addRoute('Root', r)
  }
  return router.getRoutes()
}

// 刷新页面时先用本地缓存的菜单把路由补齐，
// 避免 beforeEach 异步拉菜单期间目标路由尚未注册而落到 catch-all。
try {
  const cached = JSON.parse(localStorage.getItem('aiagent_menus') || '[]')
  if (cached.length) setupDynamicRoutes(cached)
} catch (e) {
  // 缓存损坏时忽略，后续由 beforeEach 重新拉取
}

// 全局前置守卫
router.beforeEach(async (to) => {
  if (to.meta.public) return true
  const userStore = useUserStore()
  if (!userStore.isLogin) {
    return { path: '/login', query: { redirect: to.fullPath } }
  }
  // 首次进入：刷新用户信息（权限点/角色）+ 拉取动态菜单并注册路由
  if (!userStore._menusLoaded) {
    try {
      if (userStore.token) {
        const me = await getMe()
        if (me && me.username) userStore.setUser(me)
      }
      const res = await getMyMenus()
      const menus = res.data || []
      userStore.setMenus(menus)
      userStore._menusLoaded = true
      setupDynamicRoutes(menus)
      // 重新导航到目标（路由可能刚注册）。
      // 只回传 path/query/hash，不展开整个 to —— to 里带着旧解析结果的
      // matched / params，原样回传会让 vue-router 拿过期匹配重复解析。
      return { path: to.path, query: to.query, hash: to.hash, replace: true }
    } catch (e) {
      userStore.logout()
      return { path: '/login' }
    }
  }
  // 菜单权限码校验（菜单级兜底，接口级由后端 RequirePerm / Casbin 保证）
  if (to.meta.perm && !userStore.hasPerm(to.meta.perm)) {
    return { path: '/agents' }
  }
  return true
})

export default router
