<template>
  <div class="workspace">
    <!-- 顶栏：沉浸式渐变头部，突出智能体本身 -->
    <header class="ws-topbar" :class="`ws-topbar--${type.color}`">
      <div class="topbar-left">
        <button class="back-btn" title="返回智能体管理" @click="$router.push('/agents')">
          <el-icon><ArrowLeft /></el-icon>
        </button>
        <div class="agent-badge ds-icon-badge ds-icon-badge--md">
          {{ agent?.avatar || type.icon }}
        </div>
        <div class="agent-meta">
          <div class="agent-name">
            {{ agent?.name || '智能体' }}
            <span class="type-tag" :class="`type-tag--${type.color}`">
              {{ type.icon }} {{ type.label }}
            </span>
            <span v-if="agent && agent.status !== 'published'" class="status-dot" :title="statusText" />
          </div>
          <div class="agent-desc">{{ agent?.description || type.desc }}</div>
        </div>
      </div>

      <div class="topbar-right">
        <!-- 配置统一在管理平台设置页完成，工作台只负责运行 -->
        <button class="icon-btn" title="在平台设置页配置" @click="openConsoleSettings">
          <el-icon><Setting /></el-icon>
        </button>
      </div>
    </header>

    <!-- 未发布改动提示：工作台跑的是线上已发布版本，编辑态的改动尚未生效 -->
    <div v-if="releaseStatus.hasUnpublishedChanges" class="ws-banner">
      <el-icon><WarningFilled /></el-icon>
      <span>
        当前运行的是线上版本 <b>{{ releaseStatus.currentVersion || '未发布' }}</b>，
        有 <b>{{ releaseStatus.pendingCount }}</b> 项改动未发布（不会生效）；
        如需让客户用上最新配置，请到设置页「发布新版本」。
      </span>
      <button class="banner-btn" @click="goToConsole">前往发布</button>
    </div>

    <!-- 主区：沉浸，按类型渲染 -->
    <main class="ws-main">
      <Suspense>
        <component
          :is="workspaceComp"
          v-if="agent"
          :key="`${agentId}-${type.value}`"
          :category="type.value"
          :agent-id="agentId"
          :agent="agent"
        />
        <div v-else class="ds-empty">
          <div class="ds-empty__icon">🤖</div>
          <div class="ds-empty__title">智能体不存在或已删除</div>
        </div>
      </Suspense>
    </main>

  </div>
</template>

<script setup>
import { ref, computed, shallowRef, watch, onMounted, defineAsyncComponent } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { WarningFilled } from '@element-plus/icons-vue'
import { getAgent, getAgentReleaseStatus } from '@/api/agent'
import { getAgentType, getWorkspaceLoader } from '@/constants/agentTypes'

const route = useRoute()
const router = useRouter()
const agentId = computed(() => Number(route.params.id) || 0)

// 所有配置（基础信息 / 模型 / 技能 / 预设问题 / 资源 / 版本发布）都在管理平台设置页完成，
// 工作台只负责运行，因此这里直接跳过去，不再维护第二套设置界面。
function openConsoleSettings() {
  const url = router.resolve({ name: 'AgentConfig', params: { id: agentId.value } }).href
  window.open(url, '_blank')
}

// 未发布改动横幅：「前往发布」跳到管理平台的智能体详情页（发布入口在详情页头部）
function goToConsole() {
  router.push({ path: `/agents/${agentId.value}` })
}

const agent = ref(null)

// 当前类型配置（由注册表驱动）
const type = computed(() => getAgentType(agent.value?.category))

// 按类型动态加载工作区组件
const workspaceComp = shallowRef(null)
watch(
  type,
  async (t) => {
    const loader = getWorkspaceLoader(t.value)
    workspaceComp.value = defineAsyncComponent(loader)
  },
  { immediate: true }
)

const statusText = computed(() => ({
  draft: '草稿', published: '已发布', offline: '已下线'
}[agent.value?.status] || agent.value?.status || ''))

// 发布状态：工作台运行的是线上已发布版本，需提示未发布改动
const releaseStatus = ref({
  published: false,
  currentVersion: '',
  hasUnpublishedChanges: false,
  pendingCount: 0
})

async function loadAgent() {
  if (!agentId.value) return
  try {
    const res = await getAgent(agentId.value)
    agent.value = res.data || res
    const st = await getAgentReleaseStatus(agentId.value)
    if (st.code === 0) {
      releaseStatus.value = {
        published: !!st.data.published,
        currentVersion: st.data.currentVersion || '',
        hasUnpublishedChanges: !!st.data.hasUnpublishedChanges,
        pendingCount: st.data.pendingCount || 0
      }
    }
  } catch (e) {
    agent.value = null
  }
}

onMounted(loadAgent)
watch(agentId, loadAgent)
</script>

<style scoped>
/* 独立全屏应用：不套管理平台布局 */
.workspace {
  position: fixed;
  inset: 0;
  z-index: var(--z-overlay);
  display: flex;
  flex-direction: column;
  height: 100vh;
  background: var(--bg);
  font-family: var(--font-sans);
}

/* ---- 顶栏：沉浸式渐变（按类型着色） ---- */
.ws-topbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-4);
  padding: var(--space-3) var(--space-5);
  position: relative;
  border-bottom: 1px solid rgba(255, 255, 255, 0.18);
  flex-shrink: 0;
  overflow: hidden;
  background: var(--primary-gradient);
  color: #fff;
}
/* 类型色变体 */
.ws-topbar--warning { background: linear-gradient(135deg, #f1b44c, #f6d365); }
.ws-topbar--success { background: linear-gradient(135deg, #34c38f, #4ade80); }
.ws-topbar--info { background: linear-gradient(135deg, #50a5f1, #7cc4ff); }

/* 顶部柔光 orb，增加层次 */
.ws-topbar::before {
  content: "";
  position: absolute;
  top: -60px;
  right: -40px;
  width: 220px;
  height: 220px;
  background: radial-gradient(circle, rgba(255, 255, 255, 0.28), transparent 70%);
  pointer-events: none;
}
.ws-topbar::after {
  content: "";
  position: absolute;
  bottom: -80px;
  left: 120px;
  width: 180px;
  height: 180px;
  background: radial-gradient(circle, rgba(255, 255, 255, 0.16), transparent 70%);
  pointer-events: none;
}

.topbar-left { position: relative; z-index: 1; }

.back-btn { color: rgba(255, 255, 255, 0.92); }
.back-btn:hover { background: rgba(255, 255, 255, 0.18); color: #fff; }

.icon-btn { color: rgba(255, 255, 255, 0.92); }
.icon-btn:hover { background: rgba(255, 255, 255, 0.18); color: #fff; }

.agent-name { color: #fff; }
.agent-desc { color: rgba(255, 255, 255, 0.82); }

/* 类型标签在彩色头部上用白底半透明 */
.type-tag { background: rgba(255, 255, 255, 0.22); color: #fff; }

/* 状态点：未发布用白色描边提示 */
.status-dot { background: #fff; box-shadow: 0 0 0 2px rgba(255, 255, 255, 0.4); }

.topbar-left {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  min-width: 0;
}

.back-btn,
.icon-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 16px;
  transition: background var(--duration-fast) var(--ease), color var(--duration-fast) var(--ease);
}

.back-btn:hover,
.icon-btn:hover {
  background: var(--primary-soft);
  color: var(--primary);
}

.agent-meta { min-width: 0; }

.agent-name {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: 15px;
  font-weight: 600;
  color: var(--text);
}

/* 类型标签：按类型着色 */
.type-tag {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 8px;
  border-radius: var(--radius-full);
  font-size: 11px;
  font-weight: 500;
}

.type-tag--primary { background: var(--primary-soft); color: var(--primary); }
.type-tag--warning { background: var(--warning-soft); color: #b8860b; }
.type-tag--success { background: var(--success-soft); color: #1f9d6b; }
.type-tag--info { background: var(--info-soft); color: #2f7fc4; }

.status-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--warning);
}

.agent-desc {
  font-size: 12px;
  color: var(--text-secondary);
  margin-top: 2px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 480px;
}

.topbar-right { display: flex; gap: var(--space-1); flex-shrink: 0; }

/* 未发布改动横幅：固定在顶栏与主区之间，黄色提示 */
.ws-banner {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 20px;
  background: #fff7e6;
  border-bottom: 1px solid #ffe1a8;
  color: #8a5a00;
  font-size: 13px;
  flex-shrink: 0;
  z-index: 2;
}
.ws-banner .el-icon { color: #e6a23c; font-size: 16px; flex-shrink: 0; }
.ws-banner b { color: #b8860b; }
.banner-btn {
  margin-left: auto;
  flex-shrink: 0;
  border: none;
  border-radius: var(--radius-sm);
  background: #e6a23c;
  color: #fff;
  font-size: 12px;
  font-weight: 600;
  padding: 5px 12px;
  cursor: pointer;
}
.banner-btn:hover { background: #d4922c; }

/* ---- 主区 ---- */
.ws-main {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  background:
    radial-gradient(1200px 400px at 80% -10%, rgba(79, 110, 247, 0.06), transparent 60%),
    var(--bg);
  padding: var(--space-4);
  gap: var(--space-4);
}

/* 子组件统一装进白底圆角容器 */
.ws-main > :deep(.asset-search),
.ws-main > :deep(.chat-panel) {
  flex: 1;
  min-height: 0;
  background: var(--card-bg);
  border: 1px solid var(--border-light);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-sm);
  overflow: hidden;
}

/* 运维工作台自带三栏骨架，只需要撑满主区，不要再套一层内边距 */
.ws-main > :deep(.ops-workspace) {
  flex: 1;
  min-height: 0;
  background: var(--card-bg);
  border: 1px solid var(--border-light);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-sm);
  overflow: hidden;
}

</style>
