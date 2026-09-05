<template>
  <div class="main-layout">
    <!-- 侧边栏 -->
    <div class="sidebar" :class="{ collapsed }">
      <div class="logo">
        <span class="logo-icon">🤖</span>
        <span v-show="!collapsed" class="logo-text">AI Agent 平台</span>
      </div>
      <el-menu
        :default-active="activeMenu"
        :collapse="collapsed"
        :collapse-transition="false"
        background-color="var(--sidebar-bg)"
        text-color="#a0aec0"
        active-text-color="#fff"
        router
      >
        <template v-for="item in menuItems" :key="item.path">
          <!-- 有可见子菜单 → 展开子菜单（目录节点不注册路由，仅做分组） -->
          <el-sub-menu v-if="visibleChildren(item).length" :index="item.path">
            <template #title>
              <el-icon><component :is="item.icon || 'Menu'" /></el-icon>
              <span>{{ item.title }}</span>
            </template>
            <el-menu-item v-for="child in visibleChildren(item)" :key="child.path" :index="child.path">
              <span>{{ child.title }}</span>
            </el-menu-item>
          </el-sub-menu>
          <!-- 叶子菜单 -->
          <el-menu-item v-else-if="!item.hidden" :index="item.path">
            <el-icon><component :is="item.icon || 'Menu'" /></el-icon>
            <span>{{ item.title }}</span>
          </el-menu-item>
        </template>

      </el-menu>
      <div class="sidebar-footer">
        <el-button
          :icon="collapsed ? 'Expand' : 'Fold'"
          text
          @click="collapsed = !collapsed"
          style="color: #a0aec0"
        />
      </div>
    </div>

    <!-- 主内容 -->
    <div class="main-content">
      <!-- 顶栏 -->
      <div class="topbar">
        <div class="topbar-left">
          <el-breadcrumb separator="/">
            <el-breadcrumb-item :to="{ path: '/agents' }">首页</el-breadcrumb-item>
            <el-breadcrumb-item v-for="c in crumbs" :key="c">{{ c }}</el-breadcrumb-item>
          </el-breadcrumb>
        </div>
        <div class="topbar-right">
          <el-dropdown trigger="click">
            <span class="user-info">
              <el-avatar :size="32" style="background: var(--primary-gradient)">{{ nickname[0] }}</el-avatar>
              <span class="user-meta">
                <span class="username">{{ nickname }}</span>
                <span v-if="roleName" class="user-role">{{ roleName }}</span>
              </span>
            </span>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item disabled>{{ nickname }}</el-dropdown-item>
                <el-dropdown-item divided @click="handleLogout">退出登录</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
      </div>

      <!-- 页面内容 -->
      <div class="page-content">
        <router-view />
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useUserStore } from '@/stores/user'

const route = useRoute()
const router = useRouter()
const userStore = useUserStore()

const collapsed = ref(false)
const nickname = computed(() => userStore.nickname)
const roleName = computed(() => userStore.roleName)
const activeMenu = computed(() => route.path)

// 侧边栏菜单来自后端菜单树（已按角色过滤，见 GET /api/menus/my）。
// 图标名由后端菜单表的 icon 字段给出，Element Plus 图标已在 main.js 全局注册，直接用组件名即可。
const menuItems = computed(() => userStore.menus || [])

// 仅返回可见（未隐藏）的子菜单，用于判断渲染成子菜单还是叶子菜单
function visibleChildren(item) {
  return (item.children || []).filter((c) => !c.hidden)
}

// 面包屑：先取分组（数据 / 系统设置），再取页面标题，两级菜单下显示「数据 / 视频源」
const crumbs = computed(() => {
  const list = []
  for (const r of route.matched) {
    if (r.meta?.group) list.push(r.meta.group)
    if (r.meta?.title) list.push(r.meta.title)
  }
  return list.filter((v, i) => list.indexOf(v) === i)
})

function handleLogout() {
  userStore.logout()
  router.push('/login')
}
</script>

<style scoped>
.main-layout {
  display: flex;
  height: 100vh;
}

.sidebar {
  width: 220px;
  background: var(--sidebar-bg);
  display: flex;
  flex-direction: column;
  transition: width 0.3s;
  overflow: hidden;
}

.sidebar.collapsed {
  width: 64px;
}

.logo {
  height: 60px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 0 16px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.08);
}

.logo-icon {
  font-size: 24px;
}

.logo-text {
  font-size: 16px;
  font-weight: 700;
  color: #fff;
  white-space: nowrap;
  background: var(--primary-gradient);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
}

.sidebar :deep(.el-menu) {
  flex: 1;
  border-right: none;
  overflow-y: auto;
}

.sidebar :deep(.el-menu-item) {
  margin: 4px 8px;
  border-radius: 8px;
}

.sidebar :deep(.el-menu-item.is-active) {
  background: var(--primary-gradient) !important;
}

.sidebar-footer {
  height: 48px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-top: 1px solid rgba(255, 255, 255, 0.08);
}

.main-content {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.topbar {
  height: 56px;
  background: #fff;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 24px;
  box-shadow: 0 1px 4px rgba(0, 0, 0, 0.06);
  z-index: 10;
}

.topbar-left {
  display: flex;
  align-items: center;
}

.topbar-right {
  display: flex;
  align-items: center;
}

.user-info {
  display: flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
}

.user-meta {
  display: flex;
  flex-direction: column;
  line-height: 1.2;
}

.username {
  font-size: 14px;
  color: var(--text);
}

.user-role {
  font-size: 11px;
  color: var(--text-secondary, #909399);
}

.page-content {
  flex: 1;
  padding: 24px;
  overflow-y: auto;
  background: var(--bg);
}
</style>