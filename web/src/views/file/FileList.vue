<template>
  <div class="file-page">
    <div class="page-header">
      <h2>文件管理</h2>
      <el-button type="primary" @click="showUpload = true">
        <el-icon><Upload /></el-icon> 上传文件
      </el-button>
    </div>

    <!-- 知识库 / 标签筛选 -->
    <div class="filter-bar">
      <el-select v-if="!knowledgeId" v-model="filterKB" placeholder="按知识库筛选" clearable @change="loadFiles">
        <el-option v-for="kb in fileKBs" :key="kb.id" :label="kb.name" :value="kb.id" />
      </el-select>
      <el-select v-model="filterTag" placeholder="按标签筛选" clearable style="width: 200px">
        <el-option v-for="t in allTags" :key="t" :label="t" :value="t" />
      </el-select>
    </div>

    <!-- 文件列表 -->
    <el-table :data="displayedFiles" stripe v-loading="loading" class="app-card">
      <el-table-column prop="fileName" label="文件名" min-width="200">
        <template #default="{ row }">
          <span class="file-name">
            <el-icon><Document /></el-icon>
            {{ row.fileName }}
          </span>
        </template>
      </el-table-column>
      <el-table-column prop="fileType" label="类型" width="80">
        <template #default="{ row }">
          <el-tag size="small">{{ row.fileType }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="fileSize" label="大小" width="100">
        <template #default="{ row }">
          {{ formatSize(row.fileSize) }}
        </template>
      </el-table-column>
      <el-table-column prop="chunkCount" label="分块数" width="80" />
      <el-table-column label="标签" min-width="160">
        <template #default="{ row }">
          <el-tag
            v-for="t in splitTags(row.tags)"
            :key="t"
            size="small"
            type="info"
            class="tag-item"
          >{{ t }}</el-tag>
          <span v-if="!splitTags(row.tags).length" class="muted">-</span>
        </template>
      </el-table-column>
      <el-table-column prop="status" label="状态" width="100">
        <template #default="{ row }">
          <el-tag :type="statusType(row.status)" size="small">{{ statusText(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="uploaderName" label="上传者" width="100" />
      <el-table-column prop="createdAt" label="上传时间" width="160">
        <template #default="{ row }">
          {{ formatDate(row.createdAt) }}
        </template>
      </el-table-column>
      <el-table-column label="操作" width="280" fixed="right">
        <template #default="{ row }">
          <el-button text size="small" type="primary" @click="viewChunks(row)">
            <el-icon><View /></el-icon> 切片
          </el-button>
          <el-button text size="small" type="primary" @click="openTags(row)">
            <el-icon><CollectionTag /></el-icon> 标签
          </el-button>
          <el-button text size="small" type="primary" @click="reindex(row)">
            <el-icon><Refresh /></el-icon> 重建索引
          </el-button>
          <el-button text size="small" type="danger" @click="handleDelete(row)">
            <el-icon><Delete /></el-icon>
          </el-button>
        </template>
      </el-table-column>
    </el-table>

    <!-- 上传对话框 -->
    <el-dialog v-model="showUpload" title="上传文件" width="640px">
      <el-form label-width="80px">
        <el-form-item label="知识库" v-if="!knowledgeId">
          <el-select v-model="uploadKB" placeholder="选择文件知识库" style="width: 100%">
            <el-option v-for="kb in fileKBs" :key="kb.id" :label="kb.name" :value="kb.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="文件">
          <el-upload
            ref="uploadRef"
            class="upload-block"
            :auto-upload="false"
            :on-change="handleFileChange"
            :accept="acceptTypes"
            multiple
            drag
          >
            <el-icon class="el-icon--upload"><UploadFilled /></el-icon>
            <div class="el-upload__text">将文件拖到此处，或<em>点击上传</em></div>
            <template #tip>
              <div class="el-upload__tip">
                支持 txt、md、pdf、docx、xlsx、html、csv、json 等文档；也支持 mp4、mkv、mov 等视频（将抽取关键帧由视觉模型理解后建索引）
              </div>
            </template>
          </el-upload>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showUpload = false">取消</el-button>
        <el-button type="primary" :loading="uploading" @click="doUpload">开始上传</el-button>
      </template>
    </el-dialog>

    <!-- 切片查看：看这份文件被拆成了哪些块、每块的出处（页码 / 行号） -->
    <el-drawer v-model="showChunks" :title="chunkFile ? `切片 · ${chunkFile.fileName}` : '切片'" size="60%">
      <el-descriptions v-if="chunkFile" :column="3" size="small" border class="chunk-meta">
        <el-descriptions-item label="类型">{{ chunkFile.fileType }}</el-descriptions-item>
        <el-descriptions-item label="大小">{{ formatSize(chunkFile.fileSize) }}</el-descriptions-item>
        <el-descriptions-item label="切片数">{{ chunkTotal }}</el-descriptions-item>
        <el-descriptions-item label="解析元信息" :span="3">{{ fileMetaText(chunkFile.meta) }}</el-descriptions-item>
      </el-descriptions>

      <el-empty v-if="!chunks.length" description="暂无切片，文件可能尚未完成索引" />
      <div v-else class="chunk-list">
        <div v-for="c in chunks" :key="c.id" class="chunk-item">
          <div class="chunk-head">
            <el-tag size="small" type="primary">#{{ c.chunkIndex }}</el-tag>
            <span class="muted">{{ c.contentLen }} 字</span>
            <span v-if="chunkSource(c.metadata)" class="muted">{{ chunkSource(c.metadata) }}</span>
          </div>
          <pre class="chunk-content">{{ c.content }}</pre>
        </div>
      </div>

      <div class="chunk-pager">
        <el-pagination
          layout="prev, pager, next"
          :page-size="chunkPageSize"
          :current-page="chunkPage"
          :total="chunkTotal"
          @current-change="onChunkPage"
        />
      </div>
    </el-drawer>

    <!-- 标签编辑 -->
    <el-dialog v-model="showTags" title="编辑标签" width="480px">
      <el-select
        v-model="tagInput"
        multiple
        filterable
        allow-create
        default-first-option
        placeholder="输入后回车新建标签"
        style="width: 100%"
      >
        <el-option v-for="t in allTags" :key="t" :label="t" :value="t" />
      </el-select>
      <template #footer>
        <el-button @click="showTags = false">取消</el-button>
        <el-button type="primary" :loading="tagging" @click="saveTags">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted, computed } from 'vue'
import { View, CollectionTag } from '@element-plus/icons-vue'
import { listFiles, uploadFiles, deleteFile, reindexFile, listKnowledgeBases, listFileChunks, updateFileTags } from '@/api/file'
import { ElMessage, ElMessageBox } from 'element-plus'

const props = defineProps({
  // 归属智能体：传入后数据按该智能体隔离（Agent 详情「文件」Tab）
  agentId: { type: Number, default: 0 },
  // 归属知识库：传入后内容按该知识库隔离（知识库详情页），并锁定上传目标
  knowledgeId: { type: Number, default: 0 }
})

const files = ref([])
const knowledgeBases = ref([])
const fileKBs = computed(() => knowledgeBases.value.filter(k => k.type === 'file'))
const loading = ref(false)
const showUpload = ref(false)
const uploadKB = ref(null)
const uploading = ref(false)
const uploadRef = ref(null)
// 文档 + 视频：视频上传后由后端抽帧 + 视觉模型理解建索引
const acceptTypes = '.txt,.md,.html,.json,.csv,.pdf,.docx,.doc,.xlsx,.xls,.mp4,.avi,.mov,.mkv,.flv,.wmv,.webm,.m4v,.mpg,.mpeg,.ts'
const filterKB = ref(null)
const filterTag = ref('')
const uploadFilesList = ref([])

// 标签相关
const showTags = ref(false)
const tagInput = ref([])
const tagFile = ref(null)
const tagging = ref(false)

// 切片查看
const showChunks = ref(false)
const chunks = ref([])
const chunkTotal = ref(0)
const chunkPage = ref(1)
const chunkPageSize = 20
const chunkFile = ref(null)

// 标签是逗号分隔的字符串，展示与筛选都要先拆成数组
function splitTags(s) {
  return s ? String(s).split(',').map(t => t.trim()).filter(Boolean) : []
}

const allTags = computed(() => {
  const set = new Set()
  files.value.forEach(f => splitTags(f.tags).forEach(t => set.add(t)))
  return [...set]
})

const displayedFiles = computed(() => {
  if (!filterTag.value) return files.value
  return files.value.filter(f => splitTags(f.tags).includes(filterTag.value))
})

onMounted(() => {
  loadFiles()
  loadKBs()
})

async function loadFiles() {
  loading.value = true
  try {
    const params = { knowledgeId: props.knowledgeId || filterKB.value || '' }
    if (props.agentId) params.agentId = props.agentId
    const res = await listFiles(params)
    files.value = res.data || []
  } finally {
    loading.value = false
  }
}

async function loadKBs() {
  try {
    const params = props.agentId ? { agentId: props.agentId } : {}
    const res = await listKnowledgeBases(params)
    knowledgeBases.value = res.data || []
  } catch (e) { /* */ }
}

function handleFileChange(file) {
  uploadFilesList.value.push(file.raw)
}

async function doUpload() {
  const targetKB = props.knowledgeId || uploadKB.value
  if (!targetKB) {
    ElMessage.warning('请选择知识库')
    return
  }
  if (uploadFilesList.value.length === 0) {
    ElMessage.warning('请选择文件')
    return
  }

  uploading.value = true
  try {
    const formData = new FormData()
    formData.append('knowledgeId', targetKB)
    if (props.agentId) formData.append('agentId', props.agentId)
    uploadFilesList.value.forEach(f => formData.append('files', f))

    const res = await uploadFiles(formData)
    ElMessage.success(`成功上传 ${res.data?.length || 0} 个文件，正在后台处理...`)
    showUpload.value = false
    uploadFilesList.value = []
    loadFiles()
  } catch (e) { /* */ } finally {
    uploading.value = false
  }
}

async function reindex(row) {
  try {
    await reindexFile(row.id)
    ElMessage.success('已触发重新索引')
    loadFiles()
  } catch (e) { /* */ }
}

async function handleDelete(row) {
  try {
    await ElMessageBox.confirm('确定删除该文件？删除后将无法恢复。', '确认删除', { type: 'warning' })
    await deleteFile(row.id)
    ElMessage.success('已删除')
    loadFiles()
  } catch (e) { /* */ }
}

function formatSize(bytes) {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  let i = 0
  let size = bytes
  while (size >= 1024 && i < units.length - 1) {
    size /= 1024
    i++
  }
  return size.toFixed(1) + ' ' + units[i]
}

function formatDate(date) {
  if (!date) return '-'
  return new Date(date).toLocaleString('zh-CN')
}

function statusType(status) {
  const map = { pending: 'info', processing: 'warning', ready: 'success', failed: 'danger' }
  return map[status] || 'info'
}

function statusText(status) {
  const map = { pending: '待处理', processing: '处理中', ready: '已就绪', failed: '失败' }
  return map[status] || status
}

// ---------- 切片查看 ----------

async function viewChunks(row) {
  chunkFile.value = row
  chunkPage.value = 1
  showChunks.value = true
  await loadChunks()
}

async function loadChunks() {
  if (!chunkFile.value) return
  try {
    const res = await listFileChunks(chunkFile.value.id, {
      limit: chunkPageSize,
      offset: (chunkPage.value - 1) * chunkPageSize
    })
    chunks.value = res.data || []
    chunkTotal.value = res.total || 0
  } catch (e) { /* */ }
}

function onChunkPage(page) {
  chunkPage.value = page
  loadChunks()
}

// 文件级解析元信息（后端 files.meta，JSON）：把机器字段翻成人话
function fileMetaText(meta) {
  if (!meta) return '-'
  try {
    const obj = typeof meta === 'string' ? JSON.parse(meta) : meta
    const parts = []
    if (obj.segments != null) parts.push(`${obj.segments} 个片段`)
    if (obj.pages != null) parts.push(`${obj.pages} 页`)
    if (obj.rows != null) parts.push(`${obj.rows} 行`)
    if (Array.isArray(obj.sheets) && obj.sheets.length) parts.push(`工作表：${obj.sheets.join('、')}`)
    return parts.length ? parts.join(' · ') : '-'
  } catch (e) {
    return '-'
  }
}

// 切片级出处（后端 document_chunks.metadata，JSON）：页码 / 工作表 / 行号
function chunkSource(metadata) {
  if (!metadata) return ''
  try {
    const obj = typeof metadata === 'string' ? JSON.parse(metadata) : metadata
    const parts = []
    if (obj.page != null) parts.push(`第 ${obj.page} 页`)
    if (obj.sheet) parts.push(obj.sheet)
    if (obj.row != null) parts.push(`第 ${obj.row} 行`)
    return parts.join(' / ')
  } catch (e) {
    return ''
  }
}

// ---------- 标签 ----------

function openTags(row) {
  tagFile.value = row
  tagInput.value = splitTags(row.tags)
  showTags.value = true
}

async function saveTags() {
  if (!tagFile.value) return
  tagging.value = true
  try {
    await updateFileTags(tagFile.value.id, tagInput.value)
    ElMessage.success('标签已更新')
    showTags.value = false
    loadFiles()
  } catch (e) { /* */ } finally {
    tagging.value = false
  }
}
</script>

<style scoped>
.file-page {
  width: 100%;
}

.filter-bar {
  display: flex;
  gap: 12px;
  margin-bottom: 16px;
}

.file-name {
  display: flex;
  align-items: center;
  gap: 6px;
}

.tag-item { margin-right: 4px; }
.muted { color: var(--text-secondary); font-size: 12px; }

.chunk-meta { margin-bottom: 16px; }

.chunk-list { display: flex; flex-direction: column; gap: 12px; }

.chunk-item {
  border: 1px solid var(--border-color, #ebeef5);
  border-radius: 6px;
  padding: 10px 12px;
}

.chunk-head {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 8px;
}

.chunk-content {
  margin: 0;
  white-space: pre-wrap;
  word-break: break-word;
  font-size: 13px;
  line-height: 1.6;
  max-height: 220px;
  overflow: auto;
  color: var(--text);
}

.chunk-pager {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}
</style>