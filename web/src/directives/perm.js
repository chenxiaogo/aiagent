import { useUserStore } from '@/stores/user'

// v-perm="'task:create'"：无该权限码则移除元素。
// 权限码同时覆盖菜单(menu)与按钮(button)级权限。
export default {
  mounted(el, binding) {
    const perm = binding.value
    if (!perm) return
    const userStore = useUserStore()
    if (!userStore.hasPerm(perm)) {
      el.parentNode?.removeChild(el)
    }
  }
}
