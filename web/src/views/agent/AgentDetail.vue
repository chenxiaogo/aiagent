<template>
  <div v-loading="loading" class="agent-detail">
    <!-- 头部 -->
    <div class="detail-head app-card">
      <div class="head-main">
        <div class="agent-avatar ds-icon-badge ds-icon-badge--lg">
          {{ agent.avatar || agentType.icon }}
        </div>
        <div class="head-info">
          <div class="head-title">
            <span class="name">{{ agent.name || '智能体' }}</span>
            <el-tag :type="statusType(agent.status)" size="small" effect="light">
              {{ statusText(agent.status) }}
            </el-tag>
            <el-tag size="small" effect="plain">{{ agentType.icon }} {{ agentType.label }}</el-tag>
            <el-tag v-if="agent.slug" size="small" type="info" effect="plain">
              slug: {{ agent.slug }}
            </el-tag>
            <!-- 未发布改动：提醒当前线上用的是旧版本，编辑态的改动还没上线 -->
            <el-tag v-if="releaseStatus.hasUnpublishedChanges" size="small" type="warning" effect="light">
              {{ releaseStatus.published ? '有未发布改动' : '尚未发布' }}
            </el-tag>
            <el-tag v-else-if="releaseStatus.published" size="small" type="success" effect="plain">
              线上 {{ releaseStatus.currentVersion }}
            </el-tag>
          </div>
          <div class="head-desc">{{ agent.description || '暂无描述' }}</div>
        </div>
      </div>
      <!-- 发布入口统一在这里；版本 Tab 只展示版本信息，不再重复放发布按钮 -->
      <div class="head-actions">
        <el-button :icon="Promotion" @click="enterWorkspace">进入工作台</el-button>
        <el-badge :value="releaseStatus.pendingCount" :hidden="!releaseStatus.pendingCount" :offset="[-6, 6]">
          <el-button type="primary" :icon="Upload" @click="openPublish">发布新版本</el-button>
        </el-badge>
      </div>
    </div>

    <!-- Tab 导航（基础 Tab + 按智能体类型挂载的数据类 Tab） -->
    <div class="detail-tabs app-card">
      <el-tabs :model-value="route.name" @tab-change="onTabChange">
        <el-tab-pane v-for="t in tabs" :key="t.name" :label="t.label" :name="t.name" />
      </el-tabs>
      <router-view v-slot="{ Component }">
        <component
          :is="Component"
          ref="tabRef"
          :agent-id="agentId"
          @saved="loadReleaseStatus"
        />
      </router-view>
    </div>

    <!-- 发布新版本：入口在头部，弹窗也放在这一层，避免跨组件事件在 Tab 未挂载时丢失 -->
    <el-dialog v-model="publishVisible" title="发布新版本" width="620px">
      <el-alert
        type="info"
        :closable="false"
        show-icon
        title="发布会冻结当前配置为不可变快照"
        description="发布后站内对话与对外 MCP 同时生效；已钉住旧版本的客户不受影响，可在「发布与交付 → 客户授权」中单独升级。"
        style="margin-bottom: 16px"
      />

      <!-- 待发布改动：发布前明确列出这次改了什么，避免只靠一句 changelog -->
      <div class="diff-block">
        <div class="diff-title">
          本次将发布 {{ releaseStatus.pendingCount }} 项改动
          <span v-if="releaseStatus.published" class="diff-base">
            基于线上 {{ releaseStatus.currentVersion }}
          </span>
          <span v-else class="diff-base">首次发布</span>
        </div>
        <el-empty
          v-if="!releaseStatus.pendingCount"
          description="当前配置与线上版本一致，没有需要发布的改动"
          :image-size="60"
        />
        <ul v-else class="diff-list">
          <li v-for="(c, i) in releaseStatus.pendingChanges" :key="c.field + c.label + i">
            <span class="diff-kind" :class="c.kind">{{ changeKindText(c.kind) }}</span>
            <span class="diff-label">{{ c.label }}</span>
            <span class="diff-detail">
              <template v-if="c.kind === 'added'">{{ c.new }}</template>
              <template v-else-if="c.kind === 'removed'">{{ c.old }}</template>
              <template v-else>{{ c.old }} → {{ c.new }}</template>
            </span>
          </li>
        </ul>
      </div>

      <el-input
        v-model="changelog"
        type="textarea"
        :rows="3"
        maxlength="200"
        show-word-limit
        placeholder="本次变更说明，例如：优化视频检索提示词，提升夜间场景命中率"
      />
      <template #footer>
        <el-button @click="publishVisible = false">取消</el-button>
        <el-button
          type="primary"
          :loading="publishing"
          :disabled="!releaseStatus.pendingCount"
          @click="handlePublish"
        >
          确认发布
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, watch, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Promotion, Upload } from '@element-plus/icons-vue'
import { getAgent, publishAgentVersion, getAgentReleaseStatus } from '@/api/agent'
import { getAgentType } from '@/constants/agentTypes'

const route = useRoute()
const router = useRouter()

const loading = ref(false)
const agent = ref({})
const agentType = computed(() => getAgentType(agent.value.category))

// 固定基础 Tab
const baseTabs = [
  { label: '基础配置', name: 'AgentConfig' },
  { label: '模型', name: 'AgentModels' },
  { label: '技能', name: 'AgentSkills' },
  { label: '预设问题', name: 'AgentPreset' },
  { label: '资源', name: 'AgentResources' },
  { label: '版本', name: 'AgentVersions' },
  { label: '发布与交付', name: 'AgentDelivery' }
]
// 数据类功能（视频源/摄像头/知识库/文件）统一收归到「资源」Tab 内管理，
// 不再作为独立 Tab 出现（见 AgentResources 内部的分段）。
const tabs = computed(() => baseTabs)

// 动态设置浏览器标签页标题和面包屑
const pageTitle = computed(() => agent.value.name || '智能体详情')
watch(pageTitle, (val) => {
  document.title = `${val} - AI Agent 平台`
  // 写到父路由（agents/:id）的 meta，供 MainLayout 面包屑读取
  const parent = route.matched.find(r => r.path.endsWith('agents/:id'))
  if (parent) parent.meta.title = val
}, { immediate: true })

const agentId = computed(() => Number(route.params.id))

// 当前 Tab 组件实例：发布成功后用于刷新版本列表（版本页对外暴露 load）
const tabRef = ref(null)
const publishVisible = ref(false)
const changelog = ref('')
const publishing = ref(false)

// 发布状态：当前线上版本 + 有无待发布改动（头部徽标与发布弹窗共用）
const releaseStatus = ref({
  published: false,
  currentVersion: '',
  hasUnpublishedChanges: false,
  pendingCount: 0,
  pendingChanges: []
})

async function loadReleaseStatus() {
  if (!agentId.value) return
  try {
    const res = await getAgentReleaseStatus(agentId.value)
    if (res.code === 0) {
      releaseStatus.value = {
        published: !!res.data.published,
        currentVersion: res.data.currentVersion || '',
        hasUnpublishedChanges: !!res.data.hasUnpublishedChanges,
        pendingCount: res.data.pendingCount || 0,
        pendingChanges: res.data.pendingChanges || []
      }
    }
  } catch (e) {
    // 状态拉取失败不影响页面主流程，徽标不显示即可
  }
}

async function loadAgent() {
  if (!agentId.value) return
  loading.value = true
  try {
    const res = await getAgent(agentId.value)
    if (res.code === 0) agent.value = res.data || {}
  } finally {
    loading.value = false
  }
}

// 打开发布弹窗前刷新一次状态，避免基于过期的改动清单发布
async function openPublish() {
  changelog.value = ''
  await loadReleaseStatus()
  publishVisible.value = true
}

function onTabChange(name) {
  router.push({ name, params: { id: agentId.value } })
  // 切 Tab 时刷新发布状态：在别的 Tab 里改了配置（技能/资源/MCP）也能及时看到未发布提示
  loadReleaseStatus()
}

function enterWorkspace() {
  const url = router.resolve(`/agent/${agentId.value}`).href
  window.open(url, '_blank')
}

async function handlePublish() {
  publishing.value = true
  try {
    const res = await publishAgentVersion(agentId.value, changelog.value)
    if (res.code !== 0) {
      ElMessage.error(res.message || '发布失败')
      return
    }
    ElMessage.success('已发布 ' + res.data.version + '，配置已生效')
    publishVisible.value = false
    changelog.value = ''
    loadAgent()
    await loadReleaseStatus()
    // 在版本页就直接刷新列表；不在则切过去（组件挂载时自己会加载）
    if (route.name === 'AgentVersions') tabRef.value?.load?.()
    else router.push({ name: 'AgentVersions', params: { id: agentId.value } })
  } finally {
    publishing.value = false
  }
}

function changeKindText(kind) {
  return { added: '新增', removed: '移除', changed: '修改' }[kind] || '修改'
}

function statusText(s) {
  return { draft: '草稿', published: '已发布', offline: '已下线' }[s] || s
}
function statusType(s) {
  return { draft: 'info', published: 'success', offline: 'danger' }[s] || 'info'
}

onMounted(() => {
  loadAgent()
  loadReleaseStatus()
})
watch(agentId, () => {
  loadAgent()
  loadReleaseStatus()
})
</script>

<style scoped>
.agent-detail { display: flex; flex-direction: column; gap: 16px; }

.detail-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 20px 24px;
}
.head-main { display: flex; align-items: center; gap: 14px; min-width: 0; }
.head-info { min-width: 0; }
.head-title { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.name { font-size: 18px; font-weight: 600; color: var(--text); }
.head-desc {
  margin-top: 6px;
  font-size: 13px;
  color: var(--text-secondary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.head-actions { display: flex; gap: 8px; flex-shrink: 0; }

.detail-tabs { padding: 8px 24px 24px; }
.detail-tabs :deep(.el-tabs__header) { margin-bottom: 16px; }

/* ---------- 发布弹窗：待发布改动清单 ---------- */
.diff-block {
  margin-bottom: 16px;
  padding: 12px 14px;
  border: 1px solid var(--border-light);
  border-radius: 8px;
  background: var(--bg-soft, #fafbfc);
}
.diff-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  font-weight: 600;
  color: var(--text);
  margin-bottom: 10px;
}
.diff-base { font-weight: 400; font-size: 12px; color: var(--text-muted); }
.diff-list {
  list-style: none;
  margin: 0;
  padding: 0;
  max-height: 240px;
  overflow: auto;
}
.diff-list li {
  display: flex;
  align-items: baseline;
  gap: 8px;
  padding: 5px 0;
  font-size: 12px;
  border-top: 1px dashed var(--border-light);
}
.diff-list li:first-child { border-top: none; }
.diff-kind {
  flex-shrink: 0;
  padding: 1px 6px;
  border-radius: 4px;
  font-size: 11px;
  font-weight: 600;
}
.diff-kind.added { background: #e6f7ee; color: #1f9d6b; }
.diff-kind.removed { background: #fdecea; color: #d64545; }
.diff-kind.changed { background: #eaf0fe; color: #4f6ef7; }
.diff-label { flex-shrink: 0; color: var(--text); font-weight: 500; }
.diff-detail {
  color: var(--text-secondary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
