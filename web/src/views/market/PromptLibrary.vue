<template>
  <div>
    <div class="page-header">
      <h2>提示词库</h2>
      <span class="header-sub">平台级提示词资产：意图识别、查询增强、视频分析、报告生成等各环节的 Prompt 模板，供各智能体复用</span>
    </div>

    <div class="app-card toolbar-card">
      <div class="tab-filter">
        <el-select v-model="promptFilter" placeholder="全部类型" clearable style="width: 180px" @change="loadPrompts">
          <el-option v-for="t in PROMPT_TYPES" :key="t.value" :label="t.label" :value="t.value" />
        </el-select>
      </div>
      <el-button type="primary" :icon="Plus" @click="openPromptDialog">新增 Prompt</el-button>
    </div>

    <el-table :data="promptList" v-loading="promptLoading" style="width: 100%">
      <el-table-column prop="id" label="ID" width="70" />
      <el-table-column prop="name" label="名称" min-width="160" />
      <el-table-column prop="promptType" label="类型" width="130">
        <template #default="{ row }">
          <el-tag size="small">{{ promptTypeText(row.promptType) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="description" label="描述" min-width="200" show-overflow-tooltip />
      <el-table-column prop="priority" label="优先级" width="90" />
      <el-table-column label="状态" width="90">
        <template #default="{ row }">
          <el-tag v-if="row.enabled" type="success" size="small">启用</el-tag>
          <el-tag v-else type="info" size="small">禁用</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="creatorName" label="创建者" width="100" />
      <el-table-column prop="createdAt" label="创建时间" width="170">
        <template #default="{ row }">{{ formatTime(row.createdAt) }}</template>
      </el-table-column>
      <el-table-column label="操作" width="160" fixed="right">
        <template #default="{ row }">
          <el-button size="small" :icon="Edit" @click="editPrompt(row)">编辑</el-button>
          <el-button size="small" type="danger" :icon="Delete" @click="deletePrompt(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="promptDialogVisible" :title="editingPromptId ? '编辑 Prompt' : '新增 Prompt'" width="640px">
      <el-form :model="promptForm" :rules="promptRules" ref="promptFormRef" label-width="110px">
        <el-form-item label="名称" prop="name">
          <el-input v-model="promptForm.name" placeholder="配置名称" />
        </el-form-item>
        <el-form-item label="类型" prop="promptType">
          <el-select v-model="promptForm.promptType" style="width: 100%">
            <el-option v-for="t in PROMPT_TYPES" :key="t.value" :label="t.label" :value="t.value" />
          </el-select>
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="promptForm.description" placeholder="简要描述此 Prompt 的用途" />
        </el-form-item>
        <el-form-item label="系统 Prompt" prop="systemPrompt">
          <el-input v-model="promptForm.systemPrompt" type="textarea" :rows="10" placeholder="输入系统提示词..." />
        </el-form-item>
        <el-form-item label="优先级">
          <el-input-number v-model="promptForm.priority" :min="0" :max="100" />
        </el-form-item>
        <el-form-item label="显示顺序">
          <el-input-number v-model="promptForm.displayOrder" :min="0" :max="999" />
        </el-form-item>
        <el-form-item label="启用">
          <el-switch v-model="promptForm.enabled" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="promptDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="submitPrompt">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Edit, Delete } from '@element-plus/icons-vue'
import { getPromptConfigList, createPromptConfig, updatePromptConfig, deletePromptConfig } from '@/api/promptConfig'
import { PROMPT_TYPES, promptTypeText } from '@/constants/promptTypes'

const promptList = ref([])
const promptLoading = ref(false)
const promptFilter = ref('')
const promptDialogVisible = ref(false)
const editingPromptId = ref(null)
const promptFormRef = ref(null)
const promptForm = reactive({
  name: '', promptType: 'video-analyze', systemPrompt: '',
  description: '', priority: 0, displayOrder: 0, enabled: true
})
const promptRules = {
  name: [{ required: true, message: '请输入名称', trigger: 'blur' }],
  promptType: [{ required: true, message: '请选择类型', trigger: 'change' }],
  systemPrompt: [{ required: true, message: '请输入系统 Prompt', trigger: 'blur' }]
}

async function loadPrompts() {
  promptLoading.value = true
  try {
    const res = await getPromptConfigList({ promptType: promptFilter.value })
    if (res.code === 0) promptList.value = res.data
  } catch (e) {
    ElMessage.error('加载 Prompt 配置失败')
  } finally {
    promptLoading.value = false
  }
}

function openPromptDialog() {
  editingPromptId.value = null
  Object.assign(promptForm, {
    name: '', promptType: 'video-analyze', systemPrompt: '',
    description: '', priority: 0, displayOrder: 0, enabled: true
  })
  promptDialogVisible.value = true
}

function editPrompt(row) {
  editingPromptId.value = row.id
  Object.assign(promptForm, {
    name: row.name,
    promptType: row.promptType,
    systemPrompt: row.systemPrompt,
    description: row.description,
    priority: row.priority,
    displayOrder: row.displayOrder,
    enabled: row.enabled
  })
  promptDialogVisible.value = true
}

async function submitPrompt() {
  await promptFormRef.value.validate()
  try {
    if (editingPromptId.value) {
      await updatePromptConfig(editingPromptId.value, promptForm)
      ElMessage.success('更新成功')
    } else {
      await createPromptConfig(promptForm)
      ElMessage.success('创建成功')
    }
    promptDialogVisible.value = false
    loadPrompts()
  } catch (e) {
    ElMessage.error('操作失败')
  }
}

async function deletePrompt(row) {
  await ElMessageBox.confirm(`确定删除 Prompt「${row.name}」？`, '提示', { type: 'warning' })
  try {
    await deletePromptConfig(row.id)
    ElMessage.success('删除成功')
    loadPrompts()
  } catch (e) {
    ElMessage.error('删除失败')
  }
}

function formatTime(t) {
  if (!t) return '—'
  const d = new Date(t)
  if (Number.isNaN(d.getTime())) return '—'
  const p = n => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

onMounted(loadPrompts)
</script>

<style scoped>
.page-header { display: flex; align-items: baseline; gap: 12px; margin-bottom: 16px; }
.page-header h2 { margin: 0; font-size: 20px; }
.header-sub { font-size: 13px; color: var(--text-secondary); }
.toolbar-card {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 14px 20px;
  margin-bottom: 16px;
}
</style>
