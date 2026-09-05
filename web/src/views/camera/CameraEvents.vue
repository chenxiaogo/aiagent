<template>
  <div>
    <div class="page-header">
      <div class="header-left">
        <h2>摄像头事件</h2>
        <span class="header-sub">接入摄像头片段 → AI 视觉分析 → 结构化事件 → 供智能体检索</span>
      </div>
      <el-button type="primary" :icon="UploadFilled" @click="openUpload">上传事件视频</el-button>
    </div>

    <div class="app-card filter-bar">
      <el-form :inline="true" :model="filter">
        <el-form-item label="处理状态">
          <el-select v-model="filter.processed" clearable style="width: 140px" @change="loadList">
            <el-option label="已分析" value="true" />
            <el-option label="未分析" value="false" />
          </el-select>
        </el-form-item>
        <el-form-item label="摄像头 ID">
          <el-input v-model="filter.cameraId" clearable style="width: 140px" placeholder="留空全部" @keyup.enter="loadList" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :icon="Search" @click="loadList">查询</el-button>
        </el-form-item>
      </el-form>
    </div>

    <el-table :data="list" v-loading="loading" class="app-card" style="width: 100%">
      <el-table-column prop="eventTime" label="事件时间" width="180">
        <template #default="{ row }">{{ formatTime(row.eventTime) }}</template>
      </el-table-column>
      <el-table-column prop="cameraName" label="摄像头" width="160">
        <template #default="{ row }">
          <span>{{ row.cameraName || ('#' + row.cameraId) }}</span>
        </template>
      </el-table-column>
      <el-table-column prop="summary" label="事件描述" min-width="380">
        <template #default="{ row }">
          <div class="event-summary">{{ row.summary || '—' }}</div>
        </template>
      </el-table-column>
      <el-table-column label="标签" min-width="220">
        <template #default="{ row }">
          <el-tag v-for="t in tagsOf(row)" :key="t" size="small" effect="plain" style="margin-right:4px">
            {{ t }}
          </el-tag>
          <span v-if="!tagsOf(row).length" class="muted">—</span>
        </template>
      </el-table-column>
      <el-table-column label="状态" width="100">
        <template #default="{ row }">
          <el-tag :type="row.processed ? 'success' : 'warning'" size="small" effect="light">
            {{ row.processed ? '已分析' : '待分析' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="220" fixed="right">
        <template #default="{ row }">
          <el-button size="small" :icon="VideoPlay" @click="play(row)">回放</el-button>
          <el-button
            size="small"
            type="primary"
            :icon="MagicStick"
            :loading="analyzingIds.includes(row.id)"
            @click="processEvent(row)"
          >
            {{ row.processed ? '重新分析' : '分析' }}
          </el-button>
          <el-button size="small" type="danger" :icon="Delete" @click="removeEvent(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <div class="pagination-wrap">
      <el-pagination
        v-model:current-page="filter.page"
        v-model:page-size="filter.pageSize"
        :total="total"
        :page-sizes="[20, 50, 100]"
        layout="total, sizes, prev, pager, next, jumper"
        @size-change="loadList"
        @current-change="loadList"
      />
    </div>

    <el-dialog v-model="playVisible" title="事件回放" width="760px">
      <video v-if="playVisible" :src="playUrl" controls autoplay style="width: 100%; border-radius: 8px" />
    </el-dialog>

    <el-dialog v-model="uploadVisible" title="上传摄像头事件视频" width="520px" @closed="resetUpload">
      <el-upload
        ref="uploadRef"
        drag
        :auto-upload="false"
        :limit="1"
        accept="video/*"
        :on-change="handleFileChange"
        :on-remove="resetUpload"
        :on-exceed="handleExceed"
      >
        <el-icon class="el-icon--upload"><UploadFilled /></el-icon>
        <div class="el-upload__text">将视频拖到此处，或<em>点击选择</em></div>
        <template #tip>
          <div class="el-upload__tip">上传后自动进行 AI 视觉分析（大视频自动抽帧），单文件即可</div>
        </template>
      </el-upload>
      <el-form label-width="90px" style="margin-top: 16px">
        <el-form-item label="摄像头名称">
          <el-input v-model="uploadForm.cameraName" placeholder="如：大门 / 后院 / 门口" />
        </el-form-item>
        <el-form-item label="摄像头 ID">
          <el-input v-model="uploadForm.cameraId" placeholder="选填" />
        </el-form-item>
        <el-form-item label="事件时间">
          <el-date-picker v-model="uploadForm.eventTime" type="datetime" placeholder="选填，默认当前时间" style="width: 100%" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="uploadVisible = false">取消</el-button>
        <el-button type="primary" :loading="uploading" @click="submitUpload">上传</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Search, VideoPlay, MagicStick, UploadFilled, Delete } from '@element-plus/icons-vue'
import { getCameraEvents, processCameraEvent, uploadCameraEvent, getCameraStreamUrl, deleteCameraEvent } from '@/api/camera'

const props = defineProps({
  // 归属智能体：传入后数据按该智能体隔离（Agent 详情「摄像头」Tab）
  agentId: { type: Number, default: 0 },
  // 归属知识库：传入后事件按该知识库隔离（知识库详情页）
  knowledgeId: { type: Number, default: 0 }
})

const list = ref([])
const total = ref(0)
const loading = ref(false)
const analyzingIds = ref([])
const filter = reactive({ processed: '', cameraId: '', page: 1, pageSize: 20 })

const playVisible = ref(false)
const playUrl = ref('')

const uploadVisible = ref(false)
const uploading = ref(false)
const uploadFile = ref(null)
const uploadForm = reactive({ cameraName: '', cameraId: '', eventTime: '' })

const uploadRef = ref(null)

function openUpload() {
  uploadVisible.value = true
}

function resetUpload() {
  uploadFile.value = null
  uploadForm.cameraName = ''
  uploadForm.cameraId = ''
  uploadForm.eventTime = ''
  // 清空 el-upload 内部 fileList：不清的话 limit=1 会阻止第二次选择新文件
  uploadRef.value?.clearFiles()
}

function handleFileChange(file) {
  uploadFile.value = file.raw
}

function handleExceed() {
  // limit=1 且已有文件时：移除旧文件，让新选择生效
  uploadRef.value?.clearFiles()
}

async function submitUpload() {
  if (!uploadFile.value) {
    ElMessage.warning('请选择视频文件')
    return
  }
  if (!props.agentId && !props.knowledgeId) {
    ElMessage.warning('agentId 或 knowledgeId 不能为空，请从智能体或知识库进入再上传')
    return
  }
  const fd = new FormData()
  fd.append('video', uploadFile.value)
  if (props.agentId) fd.append('agentId', props.agentId)
  if (props.knowledgeId) fd.append('knowledgeId', props.knowledgeId)
  if (uploadForm.cameraId) fd.append('cameraId', Number(uploadForm.cameraId))
  if (uploadForm.cameraName) fd.append('cameraName', uploadForm.cameraName)
  if (uploadForm.eventTime) fd.append('eventTime', new Date(uploadForm.eventTime).toISOString())
  uploading.value = true
  try {
    const res = await uploadCameraEvent(fd)
    if (res.code === 0) {
      ElMessage.success('上传成功，已开始自动分析')
      uploadVisible.value = false
      resetUpload()
      loadList()
    } else {
      ElMessage.error(res.message || '上传失败')
    }
  } finally {
    uploading.value = false
  }
}

async function loadList() {
  loading.value = true
  try {
    const params = { page: filter.page, pageSize: filter.pageSize }
    if (filter.processed) params.processed = filter.processed
    if (filter.cameraId) params.cameraId = Number(filter.cameraId)
    if (props.agentId) params.agentId = props.agentId
    if (props.knowledgeId) params.knowledgeId = props.knowledgeId
    const res = await getCameraEvents(params)
    if (res.code === 0) {
      list.value = res.data.list || []
      total.value = res.data.total || 0
    }
  } finally {
    loading.value = false
  }
}

async function processEvent(row) {
  analyzingIds.value.push(row.id)
  try {
    const res = await processCameraEvent(row.id)
    if (res.code === 0) {
      ElMessage.success(row.processed ? '已触发重新分析，请稍后刷新' : '已触发分析，请稍后刷新')
      setTimeout(loadList, 3000)
    } else {
      ElMessage.error(res.message || '操作失败')
    }
  } finally {
    analyzingIds.value = analyzingIds.value.filter(id => id !== row.id)
  }
}

async function removeEvent(row) {
  try {
    await ElMessageBox.confirm(
      `确定删除该摄像头事件？视频文件将一并删除，删除后不可恢复。`,
      '删除确认',
      { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  const res = await deleteCameraEvent(row.id)
  if (res.code === 0) {
    ElMessage.success('已删除')
    loadList()
  } else {
    ElMessage.error(res.message || '删除失败')
  }
}

function play(row) {
  playUrl.value = getCameraStreamUrl(row.id)
  playVisible.value = true
}

function tagsOf(row) {
  const tags = []
  if (row.hasPerson) tags.push(`人物×${row.personCount || 1}`)
  if (row.hasVehicle) tags.push('车辆' + (row.vehicleType ? ':' + row.vehicleType : ''))
  if (row.hasPet) tags.push('宠物' + (row.petType ? ':' + row.petType : ''))
  if (row.hasPackage) tags.push('包裹')
  if (row.action) tags.push('动作:' + row.action)
  if (row.zone) tags.push('区域:' + row.zone)
  return tags
}

function formatTime(t) {
  if (!t) return '—'
  const d = new Date(t)
  if (Number.isNaN(d.getTime())) return '—'
  const p = n => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

onMounted(loadList)
</script>

<style scoped>
.page-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 16px; }
.header-left { display: flex; align-items: baseline; gap: 12px; }
.page-header h2 { margin: 0; font-size: 20px; }
.header-sub { font-size: 13px; color: var(--text-secondary); }
.filter-bar { padding: 16px 24px; margin-bottom: 16px; }
.pagination-wrap { display: flex; justify-content: center; margin-top: 20px; }
.muted { color: var(--text-muted); }

/* 事件描述完整换行显示（100%），不被截断缩放，与视频场景描述等其它内容一致 */
.event-summary {
  white-space: normal;
  line-height: 1.5;
  word-break: break-word;
}
</style>
