<template>
  <div class="kb-manager">
    <!-- 列表视图 -->
    <div v-if="!selectedKB">
      <div class="page-header">
        <h2>知识库</h2>
        <el-button type="primary" :icon="Plus" @click="openCreate">新建知识库</el-button>
      </div>

      <div class="filter-bar" v-if="allTags.length">
        <el-select v-model="filterTag" placeholder="按标签筛选" clearable style="width: 220px">
          <el-option v-for="t in allTags" :key="t" :label="t" :value="t" />
        </el-select>
      </div>

      <div class="kb-grid">
        <div
          v-for="kb in displayedKBs"
          :key="kb.id"
          class="kb-card app-card"
          @click="openKB(kb)"
        >
          <div class="kb-icon">{{ kbTypeIcon(kb.type) }}</div>
          <div class="kb-info">
            <h3>
              {{ kb.name }}
              <el-tag size="small" type="info">{{ kbTypeIcon(kb.type) }} {{ kbTypeLabel(kb.type) }}</el-tag>
            </h3>
            <p>{{ kb.description || '暂无描述' }}</p>
            <div class="kb-stats">
              <span v-if="kb.type === 'video'"><el-icon><Film /></el-icon> {{ kb.videoCount || 0 }} 个视频</span>
              <span v-else><el-icon><Document /></el-icon> {{ kb.fileCount }} 个文件</span>
              <span><el-icon><Grid /></el-icon> {{ kb.chunkCount }} 个分块</span>
              <span><el-icon><Clock /></el-icon> {{ formatDate(kb.updatedAt || kb.createdAt) }}</span>
            </div>
            <div class="kb-tags" v-if="splitTags(kb.tags).length">
              <el-tag v-for="t in splitTags(kb.tags)" :key="t" size="small" class="tag-item">{{ t }}</el-tag>
            </div>
            <div class="kb-meta" v-if="metaText(kb.meta)">
              <span class="muted">元信息：{{ metaText(kb.meta) }}</span>
            </div>
          </div>
          <div class="kb-actions" @click.stop>
            <el-button text size="small" :icon="Edit" @click="editKB(kb)" />
            <el-button text size="small" type="danger" :icon="Delete" @click="deleteKB(kb.id)" />
          </div>
        </div>
      </div>

      <el-empty v-if="knowledgeBases.length === 0" :description="emptyText" />
    </div>

    <!-- 知识库详情（drill-down）：查询 / 上传入库 -->
    <div v-else class="kb-detail">
      <div class="detail-head">
        <el-button :icon="ArrowLeft" @click="selectedKB = null">返回</el-button>
        <div class="detail-title">
          <span class="detail-icon">{{ kbTypeIcon(selectedKB.type) }}</span>
          <span class="detail-name">{{ selectedKB.name }}</span>
          <el-tag size="small" type="info">{{ kbTypeLabel(selectedKB.type) }}</el-tag>
          <el-tag
            v-for="t in splitTags(selectedKB.tags)"
            :key="t"
            size="small"
            class="tag-item"
          >{{ t }}</el-tag>
        </div>
        <span class="detail-desc">{{ selectedKB.description || '暂无描述' }}</span>
      </div>

      <el-alert
        v-if="selectedKB.type === 'video'"
        type="success"
        :closable="false"
        show-icon
        title="视频知识库"
        description="下方可查询该知识库下的视频，并通过「上传视频」将新视频入库（自动归入本知识库）。"
        style="margin-bottom: 16px"
      />
      <el-alert
        v-else-if="selectedKB.type === 'camera'"
        type="success"
        :closable="false"
        show-icon
        title="摄像头知识库"
        description="下方为该知识库下的摄像头事件；接入摄像头片段经 AI 视觉分析后，会自动归入本知识库。"
        style="margin-bottom: 16px"
      />
      <el-alert
        v-else
        type="success"
        :closable="false"
        show-icon
        title="文件知识库"
        description="下方可查询该知识库下的文档，并通过「上传文件」将新文件入库（自动归入本知识库）。"
        style="margin-bottom: 16px"
      />

      <VideoList
        v-if="selectedKB.type === 'video'"
        :knowledge-id="selectedKB.id"
      />
      <CameraEvents
        v-else-if="selectedKB.type === 'camera'"
        :knowledge-id="selectedKB.id"
      />
      <FileList
        v-else
        :knowledge-id="selectedKB.id"
      />
    </div>

    <!-- 新建 / 编辑对话框 -->
    <el-dialog v-model="showCreate" :title="editing.id ? '编辑知识库' : '新建知识库'" width="480px">
      <el-form ref="formRef" :model="form" :rules="rules" label-width="80px">
        <el-form-item label="类型" prop="type">
          <el-select v-model="form.type" placeholder="选择内容类型" style="width: 100%">
            <el-option v-for="t in kbTypes" :key="t.value" :label="`${t.icon} ${t.label}`" :value="t.value" />
          </el-select>
          <span class="tip">一个知识库只代表一种内容类型（如视频 / 摄像头 / 文件）。</span>
        </el-form-item>
        <el-form-item label="名称" prop="name">
          <el-input v-model="form.name" placeholder="知识库名称" />
        </el-form-item>
        <el-form-item label="描述" prop="description">
          <el-input v-model="form.description" type="textarea" :rows="3" placeholder="知识库描述（可选）" />
        </el-form-item>
        <el-form-item label="图标" prop="icon">
          <el-input v-model="form.icon" placeholder="emoji 图标（可选）" maxlength="2" />
        </el-form-item>
        <el-form-item label="标签" prop="tags">
          <el-select
            v-model="form.tags"
            multiple
            filterable
            allow-create
            default-first-option
            placeholder="输入后回车新建标签"
            style="width: 100%"
          >
            <el-option v-for="t in allTags" :key="t" :label="t" :value="t" />
          </el-select>
          <span class="tip">用于归类和筛选，如「产品文档」「运维手册」。</span>
        </el-form-item>
        <el-form-item label="元信息" prop="meta">
          <el-input
            v-model="form.meta"
            type="textarea"
            :rows="2"
            placeholder='JSON，如 {"owner":"运维组","lang":"zh"}'
          />
          <span class="tip">自定义键值（归属 / 语言 / 负责人等）；留空表示不改动。</span>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCreate = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="doSave">
          {{ editing.id ? '保存' : '创建' }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, computed } from 'vue'
import { Plus, Edit, Delete, ArrowLeft } from '@element-plus/icons-vue'
import { Document, Grid, Film, Clock } from '@element-plus/icons-vue'
import { listKnowledgeBases, createKnowledgeBase, updateKnowledgeBase, deleteKnowledgeBase } from '@/api/file'
import { KB_TYPES, kbTypeLabel, kbTypeIcon } from '@/constants/knowledgeTypes'
import { ElMessage, ElMessageBox } from 'element-plus'
import VideoList from '@/views/video/VideoList.vue'
import FileList from '@/views/file/FileList.vue'
import CameraEvents from '@/views/camera/CameraEvents.vue'

// 平台级知识库管理：不再按智能体隔离。
// 原先的 agentId prop 用于 Agent 详情的「知识库」Tab，该 Tab 已下线，相关逻辑一并移除，
// 避免留下没有入口的死分支。智能体要用哪些知识库，在智能体「资源」Tab 里绑定即可。
const knowledgeBases = ref([])
const selectedKB = ref(null)
const kbTypes = KB_TYPES
const filterTag = ref('')
const showCreate = ref(false)
const saving = ref(false)
const editing = ref({})
const formRef = ref(null)

const form = reactive({
  type: 'video',
  name: '',
  description: '',
  icon: '',
  tags: [],
  meta: ''
})

// 标签：后端用逗号分隔的字符串存，前端按数组编辑与筛选
function splitTags(s) {
  return s ? String(s).split(',').map(t => t.trim()).filter(Boolean) : []
}

const allTags = computed(() => {
  const set = new Set()
  knowledgeBases.value.forEach(kb => splitTags(kb.tags).forEach(t => set.add(t)))
  return [...set]
})

const displayedKBs = computed(() => {
  if (!filterTag.value) return knowledgeBases.value
  return knowledgeBases.value.filter(kb => splitTags(kb.tags).includes(filterTag.value))
})

// 元信息是人类可读的一句话，卡片上不直接甩 JSON
function metaText(meta) {
  if (!meta) return ''
  try {
    const obj = typeof meta === 'string' ? JSON.parse(meta) : meta
    return Object.entries(obj).map(([k, v]) => `${k}: ${v}`).join(' · ')
  } catch (e) {
    return ''
  }
}

function formatDate(date) {
  if (!date) return '-'
  return new Date(date).toLocaleString('zh-CN')
}

const rules = {
  name: [{ required: true, message: '请输入知识库名称', trigger: 'blur' }],
  type: [{ required: true, message: '请选择内容类型', trigger: 'change' }]
}

const emptyText = '暂无知识库，点击右上角「新建知识库」创建'

onMounted(() => {
  loadKBs()
})

async function loadKBs() {
  try {
    const res = await listKnowledgeBases()
    knowledgeBases.value = res.data || []
  } catch (e) { /* */ }
}

function openKB(kb) {
  selectedKB.value = kb
}

function openCreate() {
  editing.value = {}
  form.type = 'video'
  form.name = ''
  form.description = ''
  form.icon = ''
  form.tags = []
  form.meta = ''
  showCreate.value = true
}

function editKB(kb) {
  editing.value = kb
  form.type = kb.type || 'video'
  form.name = kb.name
  form.description = kb.description || ''
  form.icon = kb.icon || ''
  form.tags = splitTags(kb.tags)
  form.meta = kb.meta || ''
  showCreate.value = true
}

async function doSave() {
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return

  saving.value = true
  try {
    const payload = { ...form }
    // 元信息留空表示不改动（后端按「字段是否出现」判断），有值则先校验 JSON
    if (payload.meta && payload.meta.trim()) {
      try {
        JSON.parse(payload.meta)
      } catch (e) {
        ElMessage.warning('元信息不是合法 JSON')
        return
      }
    } else {
      delete payload.meta
    }
    if (editing.value.id) {
      await updateKnowledgeBase(editing.value.id, payload)
      ElMessage.success('更新成功')
    } else {
      await createKnowledgeBase(payload)
      ElMessage.success('创建成功')
    }
    showCreate.value = false
    loadKBs()
  } catch (e) { /* */ } finally {
    saving.value = false
  }
}

async function deleteKB(id) {
  try {
    await ElMessageBox.confirm('确定删除该知识库？所有关联的文件和索引将被删除。', '确认删除', { type: 'warning' })
    await deleteKnowledgeBase(id)
    ElMessage.success('已删除')
    loadKBs()
  } catch (e) { /* */ }
}
</script>

<style scoped>
.kb-manager { width: 100%; }

.page-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}
.page-header h2 { margin: 0; font-size: 20px; }

.kb-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 16px;
}

.kb-card {
  display: flex;
  align-items: flex-start;
  gap: 16px;
  cursor: pointer;
  transition: transform 0.2s, box-shadow 0.2s;
}
.kb-card:hover {
  transform: translateY(-2px);
  box-shadow: 0 4px 20px rgba(0, 0, 0, 0.1);
}

.kb-icon { font-size: 32px; flex-shrink: 0; }

.kb-info { flex: 1; min-width: 0; }
.kb-info h3 { font-size: 16px; margin-bottom: 4px; }
.kb-info p {
  color: var(--text-secondary);
  font-size: 13px;
  margin-bottom: 12px;
  height: 20px;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}

.kb-stats {
  display: flex;
  gap: 16px;
  font-size: 12px;
  color: var(--text-secondary);
  flex-wrap: wrap;
}
.kb-stats span { display: flex; align-items: center; gap: 4px; }

.filter-bar { margin-bottom: 16px; }

.kb-tags { margin-top: 10px; }
.kb-meta { margin-top: 6px; }
.tag-item { margin-right: 4px; }
.muted { color: var(--text-secondary); font-size: 12px; }

.kb-actions {
  display: flex;
  flex-direction: column;
  gap: 4px;
  opacity: 0;
  transition: opacity 0.2s;
}
.kb-card:hover .kb-actions { opacity: 1; }

.detail-head {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;
  flex-wrap: wrap;
}
.detail-title { display: flex; align-items: center; gap: 8px; }
.detail-icon { font-size: 22px; }
.detail-name { font-size: 18px; font-weight: 600; color: var(--text); }
.detail-desc {
  font-size: 13px;
  color: var(--text-secondary);
  margin-left: auto;
  max-width: 50%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
