<template>
  <div class="reindex-page">
    <div class="app-card">
      <div class="card-header">
        <div class="card-title">
          <el-icon color="var(--primary-color)"><RefreshRight /></el-icon>
          <span>索引重建</span>
        </div>
        <div class="card-actions">
          <el-button :icon="Refresh" @click="loadStats">刷新统计</el-button>
        </div>
      </div>

      <el-alert type="info" :closable="false" show-icon class="tip-block">
        <template #title>
          解析链路升级或换向量模型后，存量向量不会自动更新，需要在这里按类型重建。
        </template>
      </el-alert>

      <div class="stats">
        <div class="stat-item">
          <div class="stat-value">{{ stats.files ?? '-' }}</div>
          <div class="stat-label">知识库文件</div>
        </div>
        <div class="stat-item">
          <div class="stat-value">{{ stats.docChunks ?? '-' }}</div>
          <div class="stat-label">文档分块</div>
        </div>
        <div class="stat-item">
          <div class="stat-value">{{ stats.videoScenes ?? '-' }}</div>
          <div class="stat-label">视频场景</div>
        </div>
        <div class="stat-item">
          <div class="stat-value">{{ stats.cameraEvents ?? '-' }}</div>
          <div class="stat-label">摄像头事件</div>
        </div>
      </div>

      <el-form label-width="110px" class="form-block">
        <el-form-item label="重建范围">
          <el-checkbox-group v-model="types">
            <el-checkbox value="files">知识库文档</el-checkbox>
            <el-checkbox value="videos">视频场景</el-checkbox>
            <el-checkbox value="cameras">摄像头事件</el-checkbox>
          </el-checkbox-group>
        </el-form-item>
        <el-form-item label="限定智能体">
          <el-input-number v-model="agentId" :min="0" placeholder="0 = 全部" />
          <span class="unit">0 表示不限（仅视频 / 摄像头生效）</span>
        </el-form-item>
        <el-form-item label="限定知识库">
          <el-input-number v-model="knowledgeId" :min="0" placeholder="0 = 全部" />
          <span class="unit">0 表示不限（仅文档生效）</span>
        </el-form-item>
      </el-form>

      <div class="actions">
        <el-button type="primary" :loading="running" :disabled="!types.length" @click="start">
          {{ running ? '重建中…' : '开始重建' }}
        </el-button>
        <el-button v-if="running" @click="stopPoll">停止刷新进度</el-button>
      </div>

      <el-alert v-if="running" type="warning" :closable="false" class="tip-block">
        重建在后台执行，可离开本页。大量数据时会调用较多 Embedding 接口，请留意配额。
      </el-alert>

      <el-table v-if="results.length" :data="results" class="result-table">
        <el-table-column prop="type" label="类型" width="130">
          <template #default="{ row }">{{ typeText(row.type) }}</template>
        </el-table-column>
        <el-table-column prop="total" label="总数" width="80" />
        <el-table-column prop="succeeded" label="成功" width="80">
          <template #default="{ row }">
            <span class="ok">{{ row.succeeded }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="failed" label="失败" width="80">
          <template #default="{ row }">
            <span :class="{ err: row.failed > 0 }">{{ row.failed }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="skipped" label="跳过" width="80">
          <template #default="{ row }">{{ row.skipped || 0 }}</template>
        </el-table-column>
        <el-table-column prop="items" label="写入条数" width="100" />
        <el-table-column prop="message" label="说明" min-width="160">
          <template #default="{ row }">{{ row.message || '—' }}</template>
        </el-table-column>
      </el-table>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import { Refresh, RefreshRight } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { reindexStats, runReindex, reindexStatus } from '@/api/reindex'

const stats = ref({})
const types = ref(['files', 'videos', 'cameras'])
const agentId = ref(0)
const knowledgeId = ref(0)
const running = ref(false)
const results = ref([])

let timer = null

function typeText(t) {
  return { files: '知识库文档', videos: '视频场景', cameras: '摄像头事件' }[t] || t
}

async function loadStats() {
  try {
    const res = await reindexStats()
    stats.value = res.data || {}
  } catch (e) {
    stats.value = {}
  }
}

async function pollStatus() {
  try {
    const res = await reindexStatus()
    const d = res.data || {}
    running.value = !!d.running
    results.value = d.results || []
    if (!d.running) stopPoll()
  } catch (e) {
    stopPoll()
  }
}

function startPoll() {
  stopPoll()
  timer = setInterval(pollStatus, 2000)
}

function stopPoll() {
  if (timer) {
    clearInterval(timer)
    timer = null
  }
}

async function start() {
  if (!types.value.length) return ElMessage.warning('请至少选择一种类型')
  try {
    const res = await runReindex({
      types: types.value,
      agentId: agentId.value || 0,
      knowledgeId: knowledgeId.value || 0
    })
    if (res.code !== 0) return ElMessage.error(res.message || '启动失败')
    ElMessage.success('重建任务已启动')
    results.value = []
    running.value = true
    startPoll()
  } catch (e) {
    ElMessage.error('启动失败')
  }
}

onMounted(() => {
  loadStats()
  // 进入页面时若已有任务在跑，接着显示其进度
  pollStatus()
})

onUnmounted(stopPoll)
</script>

<style scoped>
.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}
.card-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 16px;
  font-weight: 600;
  color: #303133;
}
.card-actions {
  display: flex;
  gap: 8px;
}
.tip-block {
  margin-bottom: 16px;
}
.stats {
  display: flex;
  gap: 12px;
  margin-bottom: 20px;
  flex-wrap: wrap;
}
.stat-item {
  flex: 1;
  min-width: 120px;
  background: #f7f8fa;
  border-radius: 8px;
  padding: 14px 16px;
}
.stat-value {
  font-size: 22px;
  font-weight: 600;
  color: #303133;
}
.stat-label {
  font-size: 12px;
  color: #909399;
  margin-top: 4px;
}
.form-block {
  max-width: 640px;
}
.unit {
  font-size: 12px;
  color: #909399;
  margin-left: 10px;
}
.actions {
  display: flex;
  gap: 10px;
  margin-bottom: 16px;
}
.result-table {
  margin-top: 8px;
}
.ok {
  color: #67c23a;
  font-weight: 600;
}
.err {
  color: #f56c6c;
  font-weight: 600;
}
</style>
