import { useUserStore } from '@/stores/user'
import { useRoute } from 'vue-router'

// v-btn="'add'"：判断当前路由对应菜单下，当前角色是否拥有该按钮。
// 依据登录时拉取的菜单树（每个菜单带 btns 按钮授权）判断。
export default {
  mounted(el, binding) {
    const btnName = binding.value
    if (!btnName) return
    const userStore = useUserStore()
    if (userStore.isAdmin) return
    const route = useRoute()
    const menu = findMenu(userStore.menus, route.path)
    if (!menu) {
      el.parentNode?.removeChild(el)
      return
    }
    const has = (menu.btns || []).some((b) => b.name === btnName)
    if (!has) {
      el.parentNode?.removeChild(el)
    }
  }
}

function findMenu(menus, path) {
  for (const m of menus) {
    if (m.path === path) return m
    if (m.children?.length) {
      const found = findMenu(m.children, path)
      if (found) return found
    }
  }
  return null
}
