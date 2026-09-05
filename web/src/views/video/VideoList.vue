<template>
  <div class="video-page">
    <div class="page-header">
      <h2>视频数据源</h2>
      <div class="header-actions">
        <!-- 知识库下钻时内容已按知识库隔离，再按智能体筛会把不属于该智能体的视频过滤掉 -->
        <el-select
          v-if="!agentId && !knowledgeId"
          v-model="selectedAgent"
          placeholder="选择智能体"
          style="width: 200px; margin-right: 12px"
          @change="loadList"
        >
          <el-option v-for="a in agents" :key="a.id" :label="a.name" :value="a.id" />
        </el-select>
        <el-select v-if="!knowledgeId" v-model="uploadKB" placeholder="选择视频知识库" style="width: 200px; margin-right: 12px" :loading="kbLoading">
          <el-option v-for="k in videoKBs" :key="k.id" :label="k.name" :value="k.id" />
        </el-select>
        <el-upload
          :show-file-list="false"
          :before-upload="handleBeforeUpload"
          :http-request="handleUpload"
          accept="video/*"
        >
          <el-button type="primary" :icon="Upload">上传视频</el-button>
        </el-upload>
      </div>
    </div>

    <!-- 筛选 -->
    <div class="app-card filter-bar">
      <el-form :inline="true" :model="filter">
        <el-form-item label="状态">
          <el-select v-model="filter.status" placeholder="全部" clearable style="width: 140px" @change="loadList">
            <el-option label="待处理" value="pending" />
            <el-option label="处理中" value="processing" />
            <el-option label="已就绪" value="ready" />
            <el-option label="失败" value="failed" />
          </el-select>
        </el-form-item>
        <el-form-item label="搜索">
          <el-input v-model="filter.keyword" placeholder="标题/文件名" clearable style="width: 220px" @keyup.enter="loadList" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :icon="Search" @click="loadList">搜索</el-button>
        </el-form-item>
      </el-form>
    </div>

    <!-- 视频列表 -->
    <el-table :data="list" v-loading="loading" class="app-card" style="width: 100%">
      <el-table-column prop="id" label="ID" width="80" />
      <el-table-column prop="title" label="标题" min-width="200">
        <template #default="{ row }">
          <div class="video-title">
            <span class="video-icon">🎬</span>
            <span>{{ row.title }}</span>
          </div>
        </template>
      </el-table-column>
      <el-table-column prop="fileName" label="文件名" min-width="180" show-overflow-tooltip />
      <el-table-column prop="fileSize" label="大小" width="120">
        <template #default="{ row }">{{ formatSize(row.fileSize) }}</template>
      </el-table-column>
      <el-table-column prop="duration" label="时长" width="100">
        <template #default="{ row }">{{ row.duration ? formatDuration(row.duration) : '-' }}</template>
      </el-table-column>
      <el-table-column prop="status" label="状态" width="100">
        <template #default="{ row }">
          <el-tag :type="statusType(row.status)" size="small">{{ statusText(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="sceneCount" label="场景数" width="90" />
      <el-table-column prop="uploaderName" label="上传者" width="100" />
      <el-table-column prop="createdAt" label="上传时间" width="170" />
      <el-table-column label="操作" width="240" fixed="right">
        <template #default="{ row }">
          <el-button size="small" :icon="View" @click="handleDetail(row)">详情</el-button>
          <el-button size="small" :icon="RefreshRight" @click="handleReprocess(row)">重处理</el-button>
          <el-button size="small" type="danger" :icon="Delete" @click="handleDelete(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <!-- 分页 -->
    <div class="pagination-wrap">
      <el-pagination
        v-model:current-page="filter.page"
        v-model:page-size="filter.pageSize"
        :total="total"
        :page-sizes="[10, 20, 50]"
        layout="total, sizes, prev, pager, next, jumper"
        @size-change="loadList"
        @current-change="loadList"
      />
    </div>

    <!-- 详情抽屉 -->
    <el-drawer v-model="detailVisible" title="视频详情" size="600px">
      <div v-if="currentVideo" class="video-detail">
        <div class="detail-section">
          <h4>基本信息</h4>
          <el-descriptions :column="1" border size="small">
            <el-descriptions-item label="标题">{{ currentVideo.title }}</el-descriptions-item>
            <el-descriptions-item label="文件名">{{ currentVideo.fileName }}</el-descriptions-item>
            <el-descriptions-item label="大小">{{ formatSize(currentVideo.fileSize) }}</el-descriptions-item>
            <el-descriptions-item label="时长">{{ currentVideo.duration ? formatDuration(currentVideo.duration) : '-' }}</el-descriptions-item>
            <el-descriptions-item label="分辨率">{{ currentVideo.resolution || '-' }}</el-descriptions-item>
            <el-descriptions-item label="帧率">{{ currentVideo.fps || '-' }}</el-descriptions-item>
            <el-descriptions-item label="状态">
              <el-tag :type="statusType(currentVideo.status)" size="small">{{ statusText(currentVideo.status) }}</el-tag>
            </el-descriptions-item>
            <el-descriptions-item label="上传时间">{{ currentVideo.createdAt }}</el-descriptions-item>
          </el-descriptions>
        </div>

        <div class="detail-section">
          <h4>视频摘要</h4>
          <div class="summary-box">{{ currentVideo.summary || '暂无摘要' }}</div>
        </div>

        <div class="detail-section">
          <h4>语音转文字</h4>
          <div class="transcript-box">{{ currentVideo.transcript || '暂无文字' }}</div>
        </div>

        <div v-if="scenes.length" class="detail-section">
          <h4>场景列表 ({{ scenes.length }})</h4>
          <div class="scene-list">
            <div v-for="s in scenes" :key="s.id" class="scene-item">
              <div class="scene-time">{{ formatTime(s.startTime) }} - {{ formatTime(s.endTime) }}</div>
              <div class="scene-desc">{{ s.description || '暂无描述' }}</div>
            </div>
          </div>
        </div>
      </div>
    </el-drawer>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Upload, Search, View, RefreshRight, Delete } from '@element-plus/icons-vue'
import { getVideoList, deleteVideo, getVideo, getVideoScenes, reprocessVideo, uploadVideo } from '@/api/video'
import { getAgentList } from '@/api/agent'
import { listKnowledgeBases } from '@/api/file'

const props = defineProps({
  // 归属智能体：传入后强制按该智能体隔离（Agent 详情「资源」Tab），并隐藏智能体选择框
  agentId: { type: Number, default: 0 },
  // 归属知识库：传入后按该知识库隔离内容（知识库详情页），并锁定上传目标
  knowledgeId: { type: Number, default: 0 }
})

const list = ref([])
const total = ref(0)
const loading = ref(false)
const agents = ref([])
const selectedAgent = ref(null)
// 视频源归属知识库：上传时需选择目标知识库（仅视频类型）
const knowledgeBases = ref([])
const kbLoading = ref(false)
const uploadKB = ref(null)
const videoKBs = computed(() => knowledgeBases.value.filter(k => k.type === 'video'))
// 优先使用传入的智能体（嵌入 Agent 详情时），否则使用页面选择框
const effectiveAgent = computed(() => props.agentId || selectedAgent.value)
const filter = reactive({
  status: '',
  keyword: '',
  page: 1,
  pageSize: 20
})

const detailVisible = ref(false)
const currentVideo = ref(null)
const scenes = ref([])

async function loadAgents() {
  try {
    const res = await getAgentList({ page: 1, pageSize: 100 })
    if (res.code === 0) {
      agents.value = res.data.list
      if (agents.value.length && !selectedAgent.value) {
        selectedAgent.value = agents.value[0].id
      }
    }
  } catch (e) {}
}

async function loadList() {
  loading.value = true
  try {
    const params = { ...filter }
    // 只有非知识库模式才带 agentId：知识库已是完整隔离维度，
    // 叠加 agentId 会让「该知识库下、但归属其它智能体」的视频凭空消失
    if (!props.knowledgeId && effectiveAgent.value) params.agentId = effectiveAgent.value
    if (props.knowledgeId) params.knowledgeId = props.knowledgeId
    const res = await getVideoList(params)
    if (res.code === 0) {
      list.value = res.data.list
      total.value = res.data.total
    }
  } catch (e) {
    ElMessage.error('加载失败')
  } finally {
    loading.value = false
  }
}

function handleBeforeUpload(file) {
  // 知识库模式：上传目标就是当前知识库，不需要智能体与知识库选择
  if (!props.knowledgeId && !effectiveAgent.value) {
    ElMessage.warning('请先选择智能体')
    return false
  }
  if (!props.knowledgeId && !uploadKB.value) {
    ElMessage.warning('请先选择知识库')
    return false
  }
  const isVideo = file.type.startsWith('video/')
  if (!isVideo) {
    ElMessage.error('只能上传视频文件')
    return false
  }
  return true
}

async function handleUpload(options) {
  const formData = new FormData()
  formData.append('file', options.file)
  // 知识库模式下不写 agentId，避免把上传的视频绑到某个智能体上
  if (!props.knowledgeId && effectiveAgent.value) formData.append('agentId', effectiveAgent.value)
  formData.append('knowledgeId', props.knowledgeId || uploadKB.value)
  formData.append('title', options.file.name)

  try {
    const res = await uploadVideo(formData)
    if (res.code === 0) {
      ElMessage.success('上传成功，正在处理...')
      loadList()
    } else {
      ElMessage.error(res.message || '上传失败')
    }
  } catch (e) {
    ElMessage.error('上传失败')
  }
}

async function handleDetail(row) {
  currentVideo.value = row
  detailVisible.value = true
  try {
    const [videoRes, scenesRes] = await Promise.all([
      getVideo(row.id),
      getVideoScenes(row.id)
    ])
    if (videoRes.code === 0) currentVideo.value = videoRes.data
    if (scenesRes.code === 0) scenes.value = scenesRes.data
  } catch (e) {}
}

async function handleReprocess(row) {
  try {
    await reprocessVideo(row.id)
    ElMessage.success('已触发重新处理')
    loadList()
  } catch (e) {
    ElMessage.error('操作失败')
  }
}

async function handleDelete(row) {
  await ElMessageBox.confirm(`确定删除视频「${row.title}」？`, '提示', { type: 'warning' })
  try {
    await deleteVideo(row.id)
    ElMessage.success('删除成功')
    loadList()
  } catch (e) {
    ElMessage.error('删除失败')
  }
}

function statusText(s) {
  return { pending: '待处理', processing: '处理中', ready: '已就绪', failed: '失败' }[s] || s
}
function statusType(s) {
  return { pending: 'info', processing: 'warning', ready: 'success', failed: 'danger' }[s] || 'info'
}
function formatSize(bytes) {
  if (!bytes) return '-'
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  if (bytes < 1024 * 1024 * 1024) return (bytes / 1024 / 1024).toFixed(1) + ' MB'
  return (bytes / 1024 / 1024 / 1024).toFixed(2) + ' GB'
}
function formatDuration(sec) {
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  const s = Math.floor(sec % 60)
  if (h > 0) return `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
  return `${m}:${String(s).padStart(2, '0')}`
}
function formatTime(sec) {
  return formatDuration(sec)
}

onMounted(() => {
  // 知识库模式不需要智能体列表（选择器已隐藏），跳过无谓请求
  if (!props.knowledgeId) loadAgents()
  loadKBs()
})

async function loadKBs() {
  if (!props.agentId) return
  kbLoading.value = true
  try {
    const res = await listKnowledgeBases({ agentId: props.agentId })
    knowledgeBases.value = res.data || []
  } catch (e) { /* */ } finally {
    kbLoading.value = false
  }
}
</script>

<style scoped>
.filter-bar {
  padding: 16px 24px;
}

.header-actions {
  display: flex;
  align-items: center;
}

.video-title {
  display: flex;
  align-items: center;
  gap: 8px;
}

.video-icon {
  font-size: 18px;
}

.pagination-wrap {
  display: flex;
  justify-content: center;
  margin-top: 24px;
}

.video-detail {
  padding: 0 8px;
}

.detail-section {
  margin-bottom: 24px;
}

.detail-section h4 {
  font-size: 15px;
  font-weight: 600;
  margin-bottom: 12px;
  color: var(--text);
}

.summary-box, .transcript-box {
  padding: 12px;
  background: #f5f7fa;
  border-radius: 8px;
  font-size: 13px;
  line-height: 1.6;
  color: var(--text-secondary);
  max-height: 200px;
  overflow-y: auto;
  white-space: pre-wrap;
}

.scene-list {
  max-height: 300px;
  overflow-y: auto;
}

.scene-item {
  padding: 10px 12px;
  background: #f5f7fa;
  border-radius: 6px;
  margin-bottom: 8px;
}

.scene-time {
  font-size: 12px;
  color: var(--primary);
  font-weight: 600;
  margin-bottom: 4px;
}

.scene-desc {
  font-size: 13px;
  color: var(--text-secondary);
  line-height: 1.5;
}
</style>
