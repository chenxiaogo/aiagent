<template>
  <div class="knowledge-page">
    <div class="page-header">
      <h2>知识库</h2>
      <el-button type="primary" @click="showCreate = true">
        <el-icon><Plus /></el-icon> 新建知识库
      </el-button>
    </div>

    <div class="kb-grid">
      <div v-for="kb in knowledgeBases" :key="kb.id" class="kb-card app-card">
        <div class="kb-icon">{{ kb.icon || '📁' }}</div>
        <div class="kb-info">
          <h3>{{ kb.name }} <el-tag size="small" type="info">{{ kbTypeIcon(kb.type) }} {{ kbTypeLabel(kb.type) }}</el-tag></h3>
          <p>{{ kb.description || '暂无描述' }}</p>
          <div class="kb-stats">
            <span><el-icon><Document /></el-icon> {{ kb.fileCount }} 个文件</span>
            <span><el-icon><Grid /></el-icon> {{ kb.chunkCount }} 个分块</span>
          </div>
        </div>
        <div class="kb-actions">
          <el-button text size="small" @click="editKB(kb)">
            <el-icon><Edit /></el-icon>
          </el-button>
          <el-button text size="small" type="danger" @click="deleteKB(kb.id)">
            <el-icon><Delete /></el-icon>
          </el-button>
        </div>
      </div>
    </div>

    <el-empty v-if="knowledgeBases.length === 0" description="暂无知识库，点击上方按钮创建">
      <el-button type="primary" @click="showCreate = true">新建知识库</el-button>
    </el-empty>

    <!-- 创建/编辑对话框 -->
    <el-dialog v-model="showCreate" :title="editing.id ? '编辑知识库' : '新建知识库'" width="480px">
      <el-form ref="formRef" :model="form" :rules="rules" label-width="80px">
        <el-form-item label="类型" prop="type">
          <el-select v-model="form.type" placeholder="选择内容类型" style="width: 100%">
            <el-option v-for="t in kbTypes" :key="t.value" :label="`${t.icon} ${t.label}`" :value="t.value" />
          </el-select>
          <span class="tip">一个知识库只代表一种内容类型（如视频 / 文件 / 音频 / 文本）。</span>
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
import { ref, reactive, onMounted } from 'vue'
import { listKnowledgeBases, createKnowledgeBase, updateKnowledgeBase, deleteKnowledgeBase } from '@/api/file'
import { KB_TYPES, kbTypeLabel, kbTypeIcon } from '@/constants/knowledgeTypes'
import { ElMessage, ElMessageBox } from 'element-plus'

const props = defineProps({
  // 归属智能体：传入后数据按该智能体隔离（Agent 详情「资源」Tab）
  agentId: { type: Number, default: 0 }
})

const knowledgeBases = ref([])
const kbTypes = KB_TYPES
const showCreate = ref(false)
const saving = ref(false)
const editing = ref({})
const formRef = ref(null)

const form = reactive({
  type: 'general',
  name: '',
  description: '',
  icon: ''
})

const rules = {
  name: [{ required: true, message: '请输入知识库名称', trigger: 'blur' }],
  type: [{ required: true, message: '请选择内容类型', trigger: 'change' }]
}

onMounted(() => {
  loadKBs()
})

async function loadKBs() {
  try {
    const params = props.agentId ? { agentId: props.agentId } : {}
    const res = await listKnowledgeBases(params)
    knowledgeBases.value = res.data || []
  } catch (e) { /* */ }
}

function editKB(kb) {
  editing.value = kb
  form.type = kb.type || 'general'
  form.name = kb.name
  form.description = kb.description || ''
  form.icon = kb.icon || ''
  showCreate.value = true
}

function resetForm() {
  editing.value = {}
  form.type = 'general'
  form.name = ''
  form.description = ''
  form.icon = ''
}

async function doSave() {
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return

  saving.value = true
  try {
    if (editing.value.id) {
      await updateKnowledgeBase(editing.value.id, { ...form })
      ElMessage.success('更新成功')
    } else {
      await createKnowledgeBase({ ...form, agentId: props.agentId })
      ElMessage.success('创建成功')
    }
    showCreate.value = false
    resetForm()
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
.knowledge-page {
  width: 100%;
}

.kb-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 16px;
}

.kb-card {
  display: flex;
  align-items: flex-start;
  gap: 16px;
  cursor: default;
  transition: transform 0.2s, box-shadow 0.2s;
}

.kb-card:hover {
  transform: translateY(-2px);
  box-shadow: 0 4px 20px rgba(0, 0, 0, 0.1);
}

.kb-icon {
  font-size: 32px;
  flex-shrink: 0;
}

.kb-info {
  flex: 1;
}

.kb-info h3 {
  font-size: 16px;
  margin-bottom: 4px;
}

.kb-info p {
  color: var(--text-secondary);
  font-size: 13px;
  margin-bottom: 12px;
  height: 20px;
  overflow: hidden;
}

.kb-stats {
  display: flex;
  gap: 16px;
  font-size: 12px;
  color: var(--text-secondary);
}

.kb-stats span {
  display: flex;
  align-items: center;
  gap: 4px;
}

.kb-actions {
  display: flex;
  flex-direction: column;
  gap: 4px;
  opacity: 0;
  transition: opacity 0.2s;
}

.kb-card:hover .kb-actions {
  opacity: 1;
}
</style>