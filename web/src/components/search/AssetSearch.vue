<template>
  <div class="asset-search">
    <!-- 搜索栏（类型固定时隐藏切换器） -->
    <div class="search-header">
      <el-radio-group v-if="switchable" v-model="innerType" size="large" @change="onTypeChange">
        <el-radio-button value="camera">📷 摄像头事件</el-radio-button>
        <el-radio-button value="video">🎬 视频片段</el-radio-button>
        <el-radio-button value="doc">📄 文档</el-radio-button>
      </el-radio-group>
      <div v-else class="fixed-type">
        <span class="fixed-icon">{{ typeIcon }}</span>
        <span class="fixed-label">{{ resultTitle }}</span>
      </div>

      <div class="search-bar">
        <el-input
          v-model="query"
          :placeholder="placeholder"
          size="large"
          clearable
          @keydown.enter="handleSearch"
        >
          <template #append>
            <el-button type="primary" :loading="loading" class="search-btn" @click="handleSearch">
              <el-icon><Search /></el-icon> 检索
            </el-button>
          </template>
        </el-input>
      </div>
    </div>

    <!-- 主体：左列表 + 右预览 -->
    <div class="search-body">
      <div class="result-panel">
        <!-- 检索范围：该智能体可检索的全部知识库 -->
        <div class="kb-scope">
          <div class="kb-scope-head">
            <span class="kb-scope-title">📚 检索范围</span>
            <span class="kb-scope-sub">{{ scopeHint }}</span>
          </div>
          <div class="kb-chips">
            <span
              class="kb-chip"
              :class="{ active: knowledgeId === null }"
              @click="selectScope(null)"
            >全部</span>
            <span
              v-for="kb in knowledgeScopes"
              :key="kb.id"
              class="kb-chip"
              :class="{ active: knowledgeId === kb.id, disabled: !scopeFilterable }"
              :title="scopeFilterable ? kb.name : kb.name + '（该类型按智能体绑定数据源检索）'"
              @click="selectScope(kb.id)"
            >{{ kb.name }}</span>
            <span v-if="!knowledgeScopes.length" class="kb-empty">未绑定知识库</span>
          </div>
        </div>

        <div class="panel-head">
          <span class="panel-title">
            {{ resultTitle }}
            <el-tag v-if="searched" size="small" type="info">{{ results.length }} 条</el-tag>
          </span>
        </div>

        <div v-loading="loading" class="result-body">
          <el-empty v-if="!searched" :description="emptyHint" :image-size="80" />
          <el-empty v-else-if="!loading && results.length === 0" description="未找到匹配结果，换个说法试试" :image-size="80" />

          <div v-else class="result-list">
          <div
            v-for="(item, idx) in results"
            :key="idx"
            class="result-card"
            :class="{ active: current?.key === item.key }"
            @click="selectItem(item)"
          >
            <div class="card-thumb">
              <img v-if="item.thumb" :src="item.thumb" :alt="item.title" @error="onImgError" />
              <span v-else class="thumb-icon">{{ typeIcon }}</span>
              <span v-if="item.duration" class="thumb-time">{{ item.duration }}</span>
            </div>
            <div class="card-info">
              <div class="card-title">{{ item.title }}</div>
              <div class="card-desc">{{ item.content }}</div>
              <div class="card-meta">
                <el-tag size="small" type="primary">相似度 {{ (item.score * 100).toFixed(0) }}%</el-tag>
                <span v-if="item.time" class="card-time">{{ item.time }}</span>
              </div>
            </div>
          </div>
          </div>
        </div>
      </div>

      <div class="preview-panel">
        <div v-if="!current" class="preview-empty">
          <el-empty description="选择左侧结果查看预览" :image-size="90" />
        </div>
        <div v-else class="preview-content">
          <div class="preview-player">
            <VideoPlayer
              v-if="current.videoSrc"
              :video-src="current.videoSrc"
              :start-time="current.startTime"
            />
            <div v-else class="preview-text">
              <div class="preview-doc-title">{{ current.title }}</div>
              <div class="preview-doc-content">{{ current.content }}</div>
              <el-button
                v-if="current.docFileId != null"
                class="locate-btn"
                size="small"
                type="primary"
                :icon="View"
                @click="openSource"
              >在原文中定位</el-button>
            </div>
          </div>

          <div class="preview-info">
            <h3>{{ current.title }}</h3>
            <div class="preview-desc">{{ current.content }}</div>
            <div class="preview-tags">
              <el-tag size="small" type="primary">相似度 {{ (current.score * 100).toFixed(0) }}%</el-tag>
              <el-tag v-for="t in current.tags" :key="t" size="small">{{ t }}</el-tag>
            </div>
            <el-descriptions v-if="current.details?.length" :column="1" size="small" border class="preview-table">
              <el-descriptions-item v-for="d in current.details" :key="d.label" :label="d.label">
                {{ d.value }}
              </el-descriptions-item>
            </el-descriptions>
          </div>
        </div>
      </div>
    </div>

    <!-- 原文定位：打开文件切片，定位到命中分块所在页并高亮 -->
    <el-drawer v-model="showSource" title="原文定位" size="60%">
      <div class="src-head" v-if="sourceFileId">
        <span class="src-file">{{ current?.title || '文档' }}</span>
        <span class="muted">共 {{ sourceTotal }} 个分块</span>
      </div>
      <el-empty v-if="!sourceLoading && !sourceChunks.length" description="暂无切片，文件可能尚未完成索引" />
      <div v-else class="src-list">
        <div v-for="c in sourceChunks" :key="c.id" class="src-chunk" :class="{ active: c.id === sourceTargetId }">
          <div class="src-chunk-head">
            <el-tag size="small" :type="c.id === sourceTargetId ? 'primary' : 'info'">#{{ c.chunkIndex }}</el-tag>
            <span v-if="chunkSourceText(c.metadata)" class="muted">{{ chunkSourceText(c.metadata) }}</span>
            <span v-if="c.id === sourceTargetId" class="src-hit">命中</span>
          </div>
          <pre class="src-chunk-content">{{ c.content }}</pre>
        </div>
      </div>
      <div class="src-pager" v-if="sourceTotal > 0">
        <el-pagination
          layout="prev, pager, next"
          :page-size="sourcePageSize"
          :current-page="sourcePage"
          :total="sourceTotal"
          @current-change="onSourcePage"
        />
      </div>
    </el-drawer>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, watch } from 'vue'
import { Search } from '@element-plus/icons-vue'
import { searchCameraEvents, getCameraStreamUrl } from '@/api/camera'
import { searchVideos, getVideoStreamUrl, getFrameUrl } from '@/api/video'
import { listKnowledgeBases, listFileChunks } from '@/api/file'
import { View } from '@element-plus/icons-vue'
import { getAgentResources } from '@/api/agent'
import request from '@/api/request'
import VideoPlayer from '@/components/VideoPlayer.vue'
import { getAgentType } from '@/constants/agentTypes'

const props = defineProps({
  // 固定检索类型；为空表示可切换（全局检索页）
  category: { type: String, default: '' },
  // 绑定智能体：摄像头/视频检索时按 agentId 过滤，对话时作为上下文
  agentId: { type: Number, default: 0 },
  // 是否显示类型切换器
  switchable: { type: Boolean, default: false }
})

const innerType = ref(props.category || 'camera')
const query = ref('')
const knowledgeId = ref(null)
// 左侧「检索范围」：该智能体可检索的全部知识库
const knowledgeScopes = ref([])
const loading = ref(false)
const searched = ref(false)
const results = ref([])
const current = ref(null)

// 文案与图标统一从类型注册表读取，新增类型无需改本组件
const currentType = computed(() => getAgentType(innerType.value))
const placeholder = computed(() => currentType.value.placeholder || '输入检索内容…')
const typeIcon = computed(() => currentType.value.icon)
const resultTitle = computed(() => `${currentType.value.icon} ${currentType.value.label}结果`)
const emptyHint = computed(() => currentType.value.emptyHint || '输入内容开始检索')

// 文档 / 视频 / 摄像头均支持按知识库过滤（camera/search、video/search 都带 knowledgeId）
const scopeFilterable = computed(() => ['doc', 'video', 'camera'].includes(innerType.value))
const scopeHint = computed(() => {
  if (!knowledgeScopes.value.length) return props.agentId ? '未绑定' : '全部'
  if (knowledgeId.value === null) return `全部 ${knowledgeScopes.value.length} 个`
  const hit = knowledgeScopes.value.find(k => k.id === knowledgeId.value)
  return hit ? hit.name : '全部'
})

// 选择检索范围：文档类型立即按该知识库重新检索
async function selectScope(id) {
  if (!scopeFilterable.value) return
  knowledgeId.value = id
  if (searched.value && query.value.trim()) {
    await handleSearch()
  }
}

// 加载检索范围：优先取智能体绑定的知识库，未绑定时回退为全部知识库
async function loadScopes() {
  try {
    if (props.agentId) {
      const res = await getAgentResources(props.agentId)
      const bound = res?.data?.knowledgeBases || []
      if (bound.length) {
        knowledgeScopes.value = bound.map(k => ({ id: k.id, name: k.name }))
        return
      }
    }
    const res = await listKnowledgeBases()
    knowledgeScopes.value = (res.data || []).map(k => ({ id: k.id, name: k.name }))
  } catch (e) { /* 范围展示失败不影响检索，忽略 */ }
}

onMounted(async () => {
  await loadScopes()
})

// 外部传入的 category 变化时同步（切换智能体工作区时生效）
watch(() => props.category, (v) => {
  if (v) {
    innerType.value = v
    knowledgeId.value = null
    resetResult()
  }
})

// 切换智能体时重新拉取该智能体的检索范围
watch(() => props.agentId, () => {
  knowledgeId.value = null
  loadScopes()
})

function resetResult() {
  results.value = []
  current.value = null
  searched.value = false
}

function onTypeChange() {
  resetResult()
}

function onImgError(e) {
  if (e?.target) e.target.style.display = 'none'
}

async function handleSearch() {
  const q = query.value.trim()
  if (!q || loading.value) return

  loading.value = true
  searched.value = true
  results.value = []
  current.value = null

  try {
    let items = []
    if (innerType.value === 'camera') {
      items = await searchCamera(q)
    } else if (innerType.value === 'video') {
      items = await searchVideo(q)
    } else {
      items = await searchDoc(q)
    }
    results.value = items
    if (items.length > 0) selectItem(items[0])
  } catch (e) {
    console.error('search failed:', e)
  } finally {
    loading.value = false
  }
}

async function searchCamera(q) {
  const res = await searchCameraEvents({
    query: q,
    agentId: props.agentId || 0,
    knowledgeId: knowledgeId.value || 0,
    topK: 5,
    threshold: 0.45
  })
  const list = res.data?.results || []
  return list.map(r => {
    // 视频内事件区间（视觉模型按抽帧时间锚点估计，0 表示未知/从头播放）
    const inSec = r.eventStartSec || 0
    const inEnd = r.eventEndSec || 0
    const inRange = inSec > 0 ? formatTime(inSec) + (inEnd > inSec ? ' - ' + formatTime(inEnd) : ' 起') : ''
    const details = [
      { label: '摄像头', value: r.cameraName || '-' },
      { label: '事件时间', value: (r.eventTime || '').slice(0, 16).replace('T', ' ') || '-' }
    ]
    if (inRange) details.push({ label: '视频内时间', value: inRange })
    if (r.duration > 0) details.push({ label: '片段时长', value: formatTime(r.duration) })
    details.push({ label: '区域', value: zoneText(r.zone) || '-' })
    details.push({ label: '动作', value: actionText(r.action) || '-' })
    return {
      key: 'c' + r.id,
      title: r.cameraName || '摄像头' + r.cameraId,
      content: r.summary || '',
      score: r.score || 0,
      thumb: r.thumbnailPath || '',
      time: (r.eventTime || r.createdAt || '').slice(0, 16).replace('T', ' '),
      videoSrc: getCameraStreamUrl(r.id),
      startTime: inSec, // 跳转到事件在视频内的起始点播放
      duration: inRange,
      tags: cameraTags(r),
      details
    }
  })
}

async function searchVideo(q) {
  const res = await searchVideos({
    query: q,
    agentId: props.agentId || 0,
    knowledgeId: knowledgeId.value || 0,
    topK: 5,
    threshold: 0.45
  })
  const list = res.data || []
  return list.map(r => {
    const tags = ['片段 #' + ((r.chunkIndex ?? 0) + 1)]
    if (r.hitScenes > 1) tags.push(`共 ${r.hitScenes} 处相关`)
    return {
      key: 'v' + r.chunkId,
      title: r.videoTitle || r.fileName || '视频片段',
      content: r.content || '',
      score: r.score || 0,
      thumb: getFrameUrl(r.videoId, r.startTime),
      duration: formatTime(r.startTime) + ' - ' + formatTime(r.endTime),
      videoSrc: getVideoStreamUrl(r.videoId),
      startTime: r.startTime || 0,
      tags,
      details: [
        { label: '来源视频', value: r.videoTitle || r.fileName || '-' },
        { label: '时间区间', value: formatTime(r.startTime) + ' - ' + formatTime(r.endTime) }
      ]
    }
  })
}

async function searchDoc(q) {
  const res = await request.post('/files/search', {
    query: q,
    agentId: props.agentId || 0,
    knowledgeId: knowledgeId.value || 0,
    topK: 5,
    threshold: 0.45
  })
  const list = res.data || []
  return list.map(r => {
    // 出处锚点：PDF 页码 / Excel 工作表与行号 / 知识库视频帧的时间区间
    const src = chunkSourceText(r.metadata)
    const tags = ['分块 #' + ((r.chunkIndex ?? 0) + 1)]
    if (src) tags.push(src)
    const details = [{ label: '来源文件', value: r.fileName || '-' }]
    if (src) details.push({ label: '出处', value: src })
    return {
      key: 'd' + r.chunkId,
      title: r.fileName || '文档',
      content: r.content || '',
      score: r.score || 0,
      thumb: '',
      time: '',
      videoSrc: '',
      startTime: 0,
      tags,
      details,
      // 原文定位数据
      docFileId: r.fileId,
      docChunkId: r.chunkId,
      docChunkIndex: r.chunkIndex ?? 0
    }
  })
}

// chunkSourceText 解析分块 metadata，翻译成人话出处。
// 文档：页码 / 工作表 / 行号；知识库视频帧：时间区间。
function chunkSourceText(metadata) {
  if (!metadata) return ''
  try {
    const obj = typeof metadata === 'string' ? JSON.parse(metadata) : metadata
    const parts = []
    if (obj.media === 'video' && obj.start != null) {
      parts.push(`视频 ${formatTime(obj.start)} - ${formatTime(obj.end)}`)
    } else {
      if (obj.page != null) parts.push(`第 ${obj.page} 页`)
      if (obj.sheet) parts.push(obj.sheet)
      if (obj.row != null) parts.push(`第 ${obj.row} 行`)
    }
    return parts.join(' / ')
  } catch (e) {
    return ''
  }
}

// ---------- 原文定位 ----------
const showSource = ref(false)
const sourceLoading = ref(false)
const sourceChunks = ref([])
const sourceTotal = ref(0)
const sourcePage = ref(1)
const sourcePageSize = 20
const sourceTargetId = ref(null)
const sourceFileId = ref(null)

async function openSource() {
  const it = current.value
  if (!it || it.docFileId == null) return
  sourceFileId.value = it.docFileId
  sourceTargetId.value = it.docChunkId
  sourcePage.value = 1
  showSource.value = true
  await loadSource(true)
}

// 首次打开时按 chunkIndex 定位到命中块所在页；翻页按 offset 正常分页
async function loadSource(locate = false) {
  const it = current.value
  if (!it) return
  sourceLoading.value = true
  try {
    const params = { limit: sourcePageSize, offset: (sourcePage.value - 1) * sourcePageSize }
    if (locate) params.chunkIndex = it.docChunkIndex
    const res = await listFileChunks(sourceFileId.value, params)
    sourceChunks.value = res.data || []
    sourceTotal.value = res.total || 0
  } catch (e) {
    sourceChunks.value = []
  } finally {
    sourceLoading.value = false
  }
}

function onSourcePage(page) {
  sourcePage.value = page
  loadSource(false)
}

function selectItem(item) {
  current.value = item
}

function cameraTags(r) {
  const tags = []
  if (r.hasPerson) tags.push('👤 有人')
  if (r.hasVehicle) tags.push('🚗 ' + (r.vehicleType || '车辆'))
  if (r.hasPackage) tags.push('📦 有包裹')
  if (r.hasPet) tags.push('🐾 ' + (r.petType || '宠物'))
  if (r.action) tags.push(actionText(r.action))
  if (r.zone) tags.push(zoneText(r.zone))
  return tags
}

function actionText(a) {
  const m = { walking: '🚶 走路', running: '🏃 跑步', stopped: '🧍 停留', picking_up: '📦 取件', delivering: '📬 投递', entering: '🚪 进入', leaving: '🚶 离开', none: '无' }
  return m[a] || a || ''
}
function zoneText(z) {
  const m = { entrance: '入口', yard: '院子', gate: '大门', front_door: '前门', driveway: '车道', indoor: '室内' }
  return m[z] || z || ''
}
function formatTime(seconds) {
  if (!seconds && seconds !== 0) return '--:--'
  if (isNaN(seconds)) return '--:--'
  const mins = Math.floor(seconds / 60)
  const secs = Math.floor(seconds % 60)
  return `${String(mins).padStart(2, '0')}:${String(secs).padStart(2, '0')}`
}
</script>

<style scoped>
.asset-search {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
}

.search-header {
  padding: 20px 24px 16px;
  background: #fff;
  border-bottom: 1px solid var(--border);
}

.fixed-type {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 16px;
  font-weight: 600;
}

.fixed-icon {
  font-size: 20px;
}

.search-bar {
  display: flex;
  gap: 12px;
  margin-top: 14px;
}

.search-bar :deep(.el-input) {
  flex: 1;
}

/* 检索按钮：加大左右内边距，与输入框留出呼吸感 */
.search-bar :deep(.el-input-group__append) {
  padding: 0;
  background: transparent;
  border: none;
}
.search-btn {
  margin-left: 10px;
  padding: 0 22px;
  height: 40px;
  border-radius: var(--radius-sm, 8px);
  font-weight: 500;
}
.search-btn :deep(.el-icon) { margin-right: 4px; }

.search-body {
  flex: 1;
  display: flex;
  min-height: 0;
  padding: 16px 24px 24px;
  gap: 16px;
}

.result-panel {
  width: 380px;
  flex-shrink: 0;
  background: #fff;
  border-radius: 12px;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

/* ---- 检索范围（智能体可检索的全部知识库） ---- */
.kb-scope {
  padding: 12px 14px 10px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-subtle, #fafbfc);
}

.kb-scope-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 8px;
}

.kb-scope-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--text);
}

.kb-scope-sub {
  font-size: 11px;
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 55%;
}

.kb-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.kb-chip {
  padding: 3px 10px;
  font-size: 12px;
  border-radius: var(--radius-full, 999px);
  border: 1px solid var(--border);
  background: #fff;
  color: var(--text-secondary);
  cursor: pointer;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  transition: all 0.2s;
}

.kb-chip:hover {
  border-color: var(--primary);
  color: var(--primary);
}

.kb-chip.active {
  background: var(--primary-gradient);
  border-color: transparent;
  color: #fff;
}

/* 视频/摄像头类型：范围仅作展示，不参与过滤 */
.kb-chip.disabled {
  cursor: default;
  opacity: 0.75;
}
.kb-chip.disabled:hover {
  border-color: var(--border);
  color: var(--text-secondary);
}
.kb-chip.disabled.active {
  background: var(--primary-gradient);
  color: #fff;
}

.kb-empty {
  font-size: 12px;
  color: var(--text-muted);
}

.panel-head {
  padding: 14px 16px;
  border-bottom: 1px solid var(--border);
}

.panel-title {
  font-size: 14px;
  font-weight: 600;
  display: flex;
  align-items: center;
  gap: 8px;
}

.result-body {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  position: relative;
  overflow: hidden;
}

.result-list {
  flex: 1;
  overflow-y: auto;
  padding: 12px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.result-card {
  display: flex;
  gap: 10px;
  padding: 10px;
  border: 1px solid var(--border);
  border-radius: 10px;
  cursor: pointer;
  transition: all 0.2s;
}

.result-card:hover {
  border-color: var(--primary);
  box-shadow: 0 2px 8px rgba(79, 110, 247, 0.12);
}

.result-card.active {
  border-color: var(--primary);
  background: rgba(79, 110, 247, 0.06);
}

.card-thumb {
  position: relative;
  width: 96px;
  height: 60px;
  flex-shrink: 0;
  background: linear-gradient(135deg, #eef1f8, #e4e9f5);
  border-radius: 8px;
  overflow: hidden;
  display: flex;
  align-items: center;
  justify-content: center;
}

.card-thumb img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.thumb-icon { font-size: 24px; }

.thumb-time {
  position: absolute;
  bottom: 2px;
  right: 4px;
  background: rgba(0, 0, 0, 0.65);
  color: #fff;
  font-size: 10px;
  padding: 1px 4px;
  border-radius: 3px;
}

.card-info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.card-title {
  font-size: 13px;
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.card-desc {
  font-size: 12px;
  color: var(--text-secondary);
  line-height: 1.5;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.card-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: auto;
}

.card-time { font-size: 11px; color: var(--text-secondary); }

.preview-panel {
  flex: 1;
  background: #fff;
  border-radius: 12px;
  overflow: hidden;
  display: flex;
  min-width: 0;
}

.preview-empty {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
}

.preview-content {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
  overflow-y: auto;
}

.preview-player {
  background: #000;
  min-height: 320px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.preview-text {
  background: #fff;
  width: 100%;
  height: 100%;
  padding: 28px 32px;
  overflow-y: auto;
  max-height: 420px;
}

.preview-doc-title { font-size: 16px; font-weight: 600; margin-bottom: 12px; }

.preview-doc-content {
  font-size: 14px;
  line-height: 1.9;
  color: var(--text);
  white-space: pre-wrap;
}

.preview-info { padding: 20px 24px 24px; }
.preview-info h3 { font-size: 16px; margin-bottom: 8px; }

.preview-desc {
  font-size: 13px;
  color: var(--text-secondary);
  line-height: 1.7;
  margin-bottom: 12px;
}

.preview-tags { display: flex; flex-wrap: wrap; gap: 6px; margin-bottom: 16px; }
.preview-table { max-width: 520px; }

/* ---- 原文定位抽屉 ---- */
.locate-btn { margin-top: 16px; }

.src-head {
  display: flex;
  align-items: baseline;
  gap: 10px;
  margin-bottom: 14px;
}
.src-file { font-size: 14px; font-weight: 600; color: var(--text); }

.src-list { display: flex; flex-direction: column; gap: 10px; }

.src-chunk {
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 10px 12px;
  background: #fff;
}
.src-chunk.active {
  border-color: var(--primary);
  background: rgba(79, 110, 247, 0.05);
  box-shadow: 0 0 0 1px var(--primary);
}
.src-chunk-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
}
.src-hit {
  font-size: 11px;
  color: var(--primary);
  font-weight: 600;
}
.src-chunk-content {
  margin: 0;
  font-size: 13px;
  line-height: 1.7;
  color: var(--text);
  white-space: pre-wrap;
  word-break: break-word;
  max-height: 200px;
  overflow-y: auto;
}
.src-pager {
  display: flex;
  justify-content: flex-end;
  margin-top: 14px;
}
.muted { font-size: 12px; color: var(--text-secondary); }
</style>
