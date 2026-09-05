import { defineStore } from 'pinia'

export const useUserStore = defineStore('user', {
  state: () => ({
    token: localStorage.getItem('aiagent_token') || '',
    user: JSON.parse(localStorage.getItem('aiagent_user') || 'null'),
    // 后端菜单树（含按钮授权），侧边栏与动态路由都从这里取
    menus: JSON.parse(localStorage.getItem('aiagent_menus') || '[]'),
    _menusLoaded: false
  }),
  getters: {
    isLogin: (state) => !!state.token,
    isAdmin: (state) => state.user?.isAdmin === true,
    nickname: (state) => state.user?.nickname || state.user?.username || '未登录',
    roleName: (state) => (state.user?.isAdmin ? '管理员' : state.user?.roleName || ''),
    perms: (state) => state.user?.perms || [],
    // 权限点校验（兼容 v-perm / hasPerm 用法）
    hasPerm: (state) => (perm) => {
      if (state.user?.isAdmin) return true
      return (state.user?.perms || []).includes(perm)
    },
    // 按钮权限：按权限码判断（按钮的 permCode 落在用户权限点集合内即可）
    hasBtn: (state) => (permCode) => {
      if (state.user?.isAdmin) return true
      return (state.user?.perms || []).includes(permCode)
    },
    // 是否拥有某菜单（按菜单路径判断）
    hasMenu: (state) => (path) => {
      if (state.user?.isAdmin) return true
      return state.menus.some((m) => m.path === path)
    }
  },
  actions: {
    setLogin(token, user, menus) {
      this.token = token
      this.user = user
      if (menus) this.setMenus(menus)
      localStorage.setItem('aiagent_token', token)
      localStorage.setItem('aiagent_user', JSON.stringify(user))
    },
    setMenus(menus) {
      this.menus = menus || []
      localStorage.setItem('aiagent_menus', JSON.stringify(this.menus))
    },
    setUser(user) {
      this.user = { ...this.user, ...user }
      localStorage.setItem('aiagent_user', JSON.stringify(this.user))
    },
    logout() {
      this.token = ''
      this.user = null
      this.menus = []
      this._menusLoaded = false
      localStorage.removeItem('aiagent_token')
      localStorage.removeItem('aiagent_user')
      localStorage.removeItem('aiagent_menus')
    }
  }
})
