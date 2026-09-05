<template>
  <div>
    <div class="page-header">
      <h2>大模型配置</h2>
      <span class="header-sub">统一管理对话 / 向量 / 视觉模型，智能体按用途（对话 / 向量化 / 视觉 / 兜底）绑定并自动回退。视觉模型用于视频帧、摄像头截图与知识库视频分析</span>
    </div>

    <div class="app-card toolbar-card">
      <div class="tab-filter">
        <el-radio-group v-model="modelFilter" size="default" @change="loadModels">
          <el-radio-button value="">全部</el-radio-button>
          <el-radio-button value="CHAT">对话模型</el-radio-button>
          <el-radio-button value="EMBEDDING">向量模型</el-radio-button>
          <el-radio-button value="VISION">视觉模型</el-radio-button>
        </el-radio-group>
      </div>
      <el-button type="primary" :icon="Plus" @click="openModelDialog">新增模型</el-button>
    </div>

    <el-table :data="modelList" v-loading="modelLoading" style="width: 100%">
      <el-table-column prop="id" label="ID" width="70" />
      <el-table-column prop="provider" label="厂商" width="120" />
      <el-table-column prop="modelName" label="模型名称" min-width="160" />
      <el-table-column prop="modelType" label="类型" width="110">
        <template #default="{ row }">
          <el-tag :type="modelTypeTag(row.modelType)" size="small">{{ modelTypeText(row.modelType) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="baseUrl" label="Base URL" min-width="200" show-overflow-tooltip />
      <el-table-column prop="temperature" label="温度" width="90" />
      <el-table-column prop="maxTokens" label="MaxTokens" width="110" />
      <el-table-column label="状态" width="90">
        <template #default="{ row }">
          <el-tag v-if="row.isActive" type="success" size="small">已激活</el-tag>
          <el-tag v-else type="info" size="small">未激活</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="200" fixed="right">
        <template #default="{ row }">
          <el-button size="small" :icon="Edit" @click="editModel(row)">编辑</el-button>
          <el-button v-if="!row.isActive" size="small" type="success" :icon="Check" @click="activateModel(row)">激活</el-button>
          <el-button size="small" type="danger" :icon="Delete" @click="deleteModel(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="modelDialogVisible" :title="editingModelId ? '编辑模型' : '新增模型'" width="600px">
      <el-form :model="modelForm" :rules="modelRules" ref="modelFormRef" label-width="130px">
        <el-form-item label="厂商" prop="provider">
          <el-input v-model="modelForm.provider" placeholder="如：openai / qwen / deepseek" />
        </el-form-item>
        <el-form-item label="Base URL" prop="baseUrl">
          <el-input v-model="modelForm.baseUrl" placeholder="https://api.example.com/v1" />
        </el-form-item>
        <el-form-item label="API Key" prop="apiKey">
          <el-input v-model="modelForm.apiKey" type="password" show-password placeholder="sk-xxx" />
        </el-form-item>
        <el-form-item label="模型名称" prop="modelName">
          <el-input v-model="modelForm.modelName" placeholder="如：gpt-4 / qwen-plus" />
        </el-form-item>
        <el-form-item label="模型类型" prop="modelType">
          <el-radio-group v-model="modelForm.modelType">
            <el-radio value="CHAT">对话模型</el-radio>
            <el-radio value="EMBEDDING">向量模型</el-radio>
            <el-radio value="VISION">视觉模型</el-radio>
          </el-radio-group>
          <div class="form-tip">视觉模型用于视频帧 / 摄像头截图 / 知识库视频分析；Gemini 系列填 provider=google、Base URL 填 <code>https://generativelanguage.googleapis.com</code> 即可自动走 Google 原生 API</div>
        </el-form-item>
        <el-form-item label="温度">
          <el-slider v-model="modelForm.temperature" :min="0" :max="2" :step="0.1" show-tooltip />
        </el-form-item>
        <el-form-item label="Max Tokens">
          <el-input-number v-model="modelForm.maxTokens" :min="128" :max="32768" :step="256" />
        </el-form-item>
        <el-form-item label="Completions 路径">
          <el-input v-model="modelForm.completionsPath" placeholder="/v1/chat/completions" />
        </el-form-item>
        <el-form-item label="Embeddings 路径">
          <el-input v-model="modelForm.embeddingsPath" placeholder="/v1/embeddings" />
        </el-form-item>
        <el-form-item label="代理设置">
          <el-switch v-model="modelForm.proxyEnabled" />
        </el-form-item>
        <template v-if="modelForm.proxyEnabled">
          <el-form-item label="代理主机">
            <el-input v-model="modelForm.proxyHost" placeholder="127.0.0.1" />
          </el-form-item>
          <el-form-item label="代理端口">
            <el-input-number v-model="modelForm.proxyPort" :min="1" :max="65535" />
          </el-form-item>
          <el-form-item label="代理用户名">
            <el-input v-model="modelForm.proxyUsername" />
          </el-form-item>
          <el-form-item label="代理密码">
            <el-input v-model="modelForm.proxyPassword" type="password" show-password />
          </el-form-item>
        </template>
      </el-form>
      <template #footer>
        <el-button @click="modelDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="submitModel">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Edit, Delete, Check } from '@element-plus/icons-vue'
import {
  getModelConfigList, createModelConfig, updateModelConfig,
  deleteModelConfig, activateModelConfig
} from '@/api/modelConfig'

const modelList = ref([])
const modelLoading = ref(false)
const modelFilter = ref('')
const modelDialogVisible = ref(false)
const editingModelId = ref(null)
const modelFormRef = ref(null)
const modelForm = reactive({
  provider: '', baseUrl: '', apiKey: '', modelName: '',
  modelType: 'CHAT', temperature: 0.7, maxTokens: 2048,
  completionsPath: '/v1/chat/completions', embeddingsPath: '/v1/embeddings',
  proxyEnabled: false, proxyHost: '', proxyPort: 7890,
  proxyUsername: '', proxyPassword: ''
})
const modelRules = {
  provider: [{ required: true, message: '请输入厂商', trigger: 'blur' }],
  baseUrl: [{ required: true, message: '请输入 Base URL', trigger: 'blur' }],
  modelName: [{ required: true, message: '请输入模型名称', trigger: 'blur' }],
  modelType: [{ required: true, message: '请选择模型类型', trigger: 'change' }]
}

async function loadModels() {
  modelLoading.value = true
  try {
    const res = await getModelConfigList(modelFilter.value)
    if (res.code === 0) modelList.value = res.data
  } catch (e) {
    ElMessage.error('加载模型配置失败')
  } finally {
    modelLoading.value = false
  }
}

function modelTypeText(t) {
  return { CHAT: '对话', EMBEDDING: '向量', VISION: '视觉' }[t] || t
}
function modelTypeTag(t) {
  if (t === 'CHAT') return 'primary'
  if (t === 'VISION') return 'warning'
  return 'success'
}

function openModelDialog() {
  editingModelId.value = null
  Object.assign(modelForm, {
    provider: '', baseUrl: '', apiKey: '', modelName: '',
    modelType: 'CHAT', temperature: 0.7, maxTokens: 2048,
    completionsPath: '/v1/chat/completions', embeddingsPath: '/v1/embeddings',
    proxyEnabled: false, proxyHost: '', proxyPort: 7890,
    proxyUsername: '', proxyPassword: ''
  })
  modelDialogVisible.value = true
}

function editModel(row) {
  editingModelId.value = row.id
  Object.assign(modelForm, {
    provider: row.provider,
    baseUrl: row.baseUrl,
    apiKey: row.apiKey,
    modelName: row.modelName,
    modelType: row.modelType,
    temperature: row.temperature,
    maxTokens: row.maxTokens,
    completionsPath: row.completionsPath,
    embeddingsPath: row.embeddingsPath,
    proxyEnabled: row.proxyEnabled,
    proxyHost: row.proxyHost,
    proxyPort: row.proxyPort,
    proxyUsername: row.proxyUsername,
    proxyPassword: row.proxyPassword
  })
  modelDialogVisible.value = true
}

async function submitModel() {
  await modelFormRef.value.validate()
  try {
    if (editingModelId.value) {
      await updateModelConfig(editingModelId.value, modelForm)
      ElMessage.success('更新成功')
    } else {
      await createModelConfig(modelForm)
      ElMessage.success('创建成功')
    }
    modelDialogVisible.value = false
    loadModels()
  } catch (e) {
    ElMessage.error('操作失败')
  }
}

async function activateModel(row) {
  try {
    await activateModelConfig(row.id)
    ElMessage.success('已激活')
    loadModels()
  } catch (e) {
    ElMessage.error('操作失败')
  }
}

async function deleteModel(row) {
  await ElMessageBox.confirm(`确定删除模型「${row.modelName}」？`, '提示', { type: 'warning' })
  try {
    await deleteModelConfig(row.id)
    ElMessage.success('删除成功')
    loadModels()
  } catch (e) {
    ElMessage.error('删除失败')
  }
}

onMounted(loadModels)
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

.form-tip {
  font-size: 12px;
  line-height: 1.7;
  color: var(--text-secondary, #8c8c8c);
  margin-top: 6px;
}
.form-tip code {
  background: rgba(0, 0, 0, 0.05);
  border-radius: 4px;
  padding: 1px 5px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}
</style>
