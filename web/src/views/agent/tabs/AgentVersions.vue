<template>
  <div v-loading="loading">
    <!-- 当前版本 -->
    <div class="app-card current-card">
      <div class="current-left">
        <div class="label">当前版本</div>
        <div class="version">{{ currentVersion || '未发布' }}</div>
        <div class="meta">
          发布于 {{ formatTime(currentRelease?.publishedAt) }}
          <template v-if="currentRelease?.publishedBy"> · 由 {{ currentRelease.publishedBy }} 发布</template>
          <template v-if="currentRelease"> · 暴露 {{ currentRelease.toolCount }} 个工具</template>
        </div>
      </div>
      <div class="current-right">
        <el-alert
          v-if="hasChanges"
          type="warning"
          :closable="false"
          show-icon
          title="配置已修改，尚未发布"
          description="当前线上客户与站内对话仍在用已发布版本，发布后才会生效。"
        />
        <el-alert
          v-else
          type="success"
          :closable="false"
          show-icon
          title="线上版本与当前配置一致"
        />
        <span class="publish-hint">发布入口在页面右上角「发布新版本」</span>
      </div>
    </div>

    <!-- 待发布改动清单：这次点发布到底改了什么 -->
    <div v-if="hasChanges" class="app-card pending-card">
      <div class="card-title">
        待发布改动（{{ pendingChanges.length }} 项）
        <span v-if="currentVersion" class="base-v">基于线上 {{ currentVersion }}</span>
        <span v-else class="base-v">首次发布</span>
      </div>
      <ul class="pending-list">
        <li v-for="(c, i) in pendingChanges" :key="c.field + c.label + i">
          <span class="kind" :class="c.kind">{{ changeKindText(c.kind) }}</span>
          <span class="p-label">{{ c.label }}</span>
          <span class="p-detail">
            <template v-if="c.kind === 'added'">{{ c.new }}</template>
            <template v-else-if="c.kind === 'removed'">{{ c.old }}</template>
            <template v-else>{{ c.old }} → {{ c.new }}</template>
          </span>
        </li>
      </ul>
    </div>

    <!-- 发布前校验 -->
    <div class="app-card check-card">
      <div class="card-title">发布前校验</div>
      <div class="check-list">
        <div v-for="item in checklist.items" :key="item.key" class="check-item" :class="{ failed: !item.passed }">
          <span class="check-icon">{{ item.passed ? '✓' : '!' }}</span>
          <div>
            <div class="check-label">{{ item.label }}</div>
            <div v-if="!item.passed" class="check-hint">{{ item.hint }}</div>
          </div>
        </div>
      </div>
    </div>

    <!-- 版本历史 -->
    <div class="app-card">
      <div class="card-title">版本历史</div>
      <el-table :data="releases" style="width: 100%">
        <el-table-column prop="version" label="版本" width="100">
          <template #default="{ row }">
            <span class="version-tag">{{ row.version }}</span>
            <el-tag v-if="row.isDefault" type="success" size="small" effect="light">当前</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="status" label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="row.status === 'published' ? 'success' : 'info'" size="small" effect="plain">
              {{ row.status === 'published' ? '已发布' : '已归档' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="toolCount" label="工具数" width="90" />
        <el-table-column prop="changelog" label="变更说明" min-width="200" show-overflow-tooltip>
          <template #default="{ row }">
            <span>{{ row.changelog || '—' }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="publishedBy" label="发布人" width="110" />
        <el-table-column prop="publishedAt" label="发布时间" width="180">
          <template #default="{ row }">{{ formatTime(row.publishedAt) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="140" fixed="right">
          <template #default="{ row }">
            <el-button
              v-if="!row.isDefault"
              size="small"
              type="warning"
              :icon="RefreshLeft"
              @click="handleRollback(row)"
            >
              回滚到此版本
            </el-button>
            <span v-else class="muted">线上生效中</span>
          </template>
        </el-table-column>
      </el-table>
    </div>

  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { RefreshLeft } from '@element-plus/icons-vue'
import { getAgentVersions, rollbackAgentVersion } from '@/api/agent'

const route = useRoute()
const agentId = computed(() => Number(route.params.id))

const loading = ref(false)
const releases = ref([])
const hasChanges = ref(false)
const pendingChanges = ref([])
const currentReleaseId = ref(0)
const checklist = ref({ items: [], canPublish: false })

const currentRelease = computed(() => releases.value.find(r => r.id === currentReleaseId.value))
const currentVersion = computed(() => currentRelease.value?.version || '')

async function load() {
  loading.value = true
  try {
    const res = await getAgentVersions(agentId.value)
    if (res.code === 0) {
      releases.value = res.data.releases || []
      hasChanges.value = !!res.data.hasUnpublishedChanges
      pendingChanges.value = res.data.pendingChanges || []
      currentReleaseId.value = res.data.currentReleaseId || 0
      checklist.value = res.data.checklist || { items: [], canPublish: false }
    }
  } finally {
    loading.value = false
  }
}

function changeKindText(kind) {
  return { added: '新增', removed: '移除', changed: '修改' }[kind] || '修改'
}

async function handleRollback(row) {
  await ElMessageBox.confirm(
    `确定将线上版本回滚到 ${row.version}？回滚后未钉版本的客户将立即使用 ${row.version}。`,
    '回滚确认',
    { type: 'warning' }
  )
  const res = await rollbackAgentVersion(agentId.value, row.id)
  if (res.code === 0) {
    ElMessage.success('已回滚到 ' + row.version)
    load()
  }
}

function formatTime(t) {
  if (!t) return '—'
  const d = new Date(t)
  if (Number.isNaN(d.getTime())) return '—'
  const p = n => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

onMounted(load)

// 供详情页头部发布成功后刷新列表
defineExpose({ load })
</script>

<style scoped>
.current-card {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 24px;
  padding: 20px 24px;
}
.label { font-size: 13px; color: var(--text-secondary); }
.version { font-size: 30px; font-weight: 700; color: var(--text); line-height: 1.2; }
.meta { margin-top: 6px; font-size: 12px; color: var(--text-muted); }
.current-right { display: flex; flex-direction: column; gap: 12px; align-items: flex-end; flex: 1; }

.check-card { padding: 16px 24px; }
.card-title { font-size: 15px; font-weight: 600; margin-bottom: 12px; color: var(--text); }
.check-list { display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 12px; }
.check-item { display: flex; gap: 10px; align-items: flex-start; }
.check-icon {
  width: 20px; height: 20px; border-radius: 50%;
  display: inline-flex; align-items: center; justify-content: center;
  font-size: 12px; font-weight: 700; flex-shrink: 0;
  background: var(--success-soft, #e6f7ee); color: #1f9d6b;
}
.check-item.failed .check-icon { background: #fdecea; color: #d64545; }
.publish-hint { font-size: 12px; color: var(--text-muted); }
.check-label { font-size: 13px; color: var(--text); }
.check-hint { font-size: 12px; color: var(--text-muted); margin-top: 2px; }

.version-tag { font-weight: 600; margin-right: 6px; }
.muted { color: var(--text-muted); font-size: 12px; }

/* ---- 待发布改动清单 ---- */
.pending-card { padding: 16px 24px; }
.base-v { font-weight: 400; font-size: 12px; color: var(--text-muted); margin-left: 8px; }
.pending-list { list-style: none; margin: 8px 0 0; padding: 0; }
.pending-list li { display: flex; align-items: baseline; gap: 8px; padding: 5px 0; font-size: 12px; border-top: 1px dashed var(--border-light); }
.pending-list li:first-child { border-top: none; }
.kind { flex-shrink: 0; padding: 1px 6px; border-radius: 4px; font-size: 11px; font-weight: 600; }
.kind.added { background: #e6f7ee; color: #1f9d6b; }
.kind.removed { background: #fdecea; color: #d64545; }
.kind.changed { background: #eaf0fe; color: #4f6ef7; }
.p-label { flex-shrink: 0; color: var(--text); font-weight: 500; }
.p-detail { color: var(--text-secondary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
