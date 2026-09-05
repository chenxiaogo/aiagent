<template>
  <div class="model-binding">
    <div class="mb-head">
      <div class="mb-title">
        模型配置
        <span class="mb-sub">一个智能体可绑定多个模型，按用途路由、同用途内按优先级回退</span>
      </div>
      <el-button size="small" :icon="Plus" @click="addRow">添加模型</el-button>
    </div>

    <el-table :data="rows" size="small" style="width: 100%">
      <el-table-column label="用途" width="120">
        <template #default="{ row }">
          <el-select v-model="row.role" size="small" style="width: 100%">
            <el-option v-for="r in ROLES" :key="r.value" :label="r.label" :value="r.value" />
          </el-select>
        </template>
      </el-table-column>

      <el-table-column label="模型" min-width="180">
        <template #default="{ row }">
          <el-select v-model="row.modelId" size="small" filterable placeholder="选择模型" style="width: 100%">
            <el-option
              v-for="m in available"
              :key="m.id"
              :label="`${m.modelName}（${m.provider}）`"
              :value="m.id"
            >
              <span>{{ m.modelName }}</span>
              <span class="opt-meta">{{ m.provider }} · {{ typeText(m.modelType) }}</span>
            </el-option>
          </el-select>
        </template>
      </el-table-column>

      <el-table-column label="主模型" width="86" align="center">
        <template #default="{ row }">
          <el-switch v-model="row.isPrimary" size="small" @change="onPrimaryChange(row)" />
        </template>
      </el-table-column>

      <el-table-column label="优先级" width="100">
        <template #default="{ row }">
          <el-input-number v-model="row.priority" size="small" :min="1" :max="99" controls-position="right" style="width: 100%" />
        </template>
      </el-table-column>

      <el-table-column label="参数覆写" min-width="150">
        <template #default="{ row }">
          <el-input v-model="row.params" size="small" placeholder='{"temperature":0.7}' />
        </template>
      </el-table-column>

      <el-table-column label="启用" width="72" align="center">
        <template #default="{ row }">
          <el-switch v-model="row.enabled" size="small" />
        </template>
      </el-table-column>

      <el-table-column label="操作" width="64" align="center">
        <template #default="{ $index }">
          <el-button size="small" type="danger" :icon="Delete" circle @click="removeRow($index)" />
        </template>
      </el-table-column>
    </el-table>

    <el-empty v-if="!rows.length" description="尚未绑定模型" :image-size="60" />
    <div class="mb-tip">
      提示：数字越小优先级越高。同一用途下第一个（或标记为主模型的）会被优先调用，失败时自动回退到下一个。
    </div>
  </div>
</template>

<script setup>
import { ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Plus, Delete } from '@element-plus/icons-vue'
import { getAgentModels, saveAgentModels } from '@/api/agent'
import { getModelConfigList } from '@/api/modelConfig'

const props = defineProps({
  // 为 null 表示智能体尚未创建（新建流程：先建智能体，再保存模型）
  agentId: { type: [Number, null], default: null }
})

const ROLES = [
  { value: 'chat', label: '对话' },
  { value: 'embedding', label: '向量化' },
  { value: 'vision', label: '视觉理解' },
  { value: 'rerank', label: '结果重排' },
  { value: 'fallback', label: '兜底' }
]

const rows = ref([])
const available = ref([])

function addRow() {
  rows.value.push({
    modelId: null,
    role: rows.value.length ? guessNextRole() : 'chat',
    isPrimary: rows.value.length === 0,
    priority: (rows.value.length + 1) * 10,
    params: '',
    enabled: true
  })
}

function guessNextRole() {
  const used = new Set(rows.value.map(r => r.role))
  const order = ['embedding', 'vision', 'rerank', 'fallback', 'chat']
  return order.find(r => !used.has(r)) || 'chat'
}

function removeRow(index) {
  rows.value.splice(index, 1)
}

// 同一用途只允许一个主模型
function onPrimaryChange(row) {
  if (!row.isPrimary) return
  rows.value.forEach(r => {
    if (r !== row && r.role === row.role) r.isPrimary = false
  })
}

function typeText(t) {
  return { CHAT: '对话', EMBEDDING: '向量', VISION: '视觉' }[t] || t
}

// 可选模型清单（新建智能体、还没有 agentId 时也要能选模型）
async function loadAvailable() {
  const [chatRes, embedRes, visionRes] = await Promise.all([
    getModelConfigList('CHAT'),
    getModelConfigList('EMBEDDING'),
    getModelConfigList('VISION')
  ])
  const list = []
  if (chatRes.code === 0) list.push(...(chatRes.data || []))
  if (embedRes.code === 0) list.push(...(embedRes.data || []))
  if (visionRes.code === 0) list.push(...(visionRes.data || []))
  available.value = list
}

async function load() {
  if (!props.agentId) {
    rows.value = []
    await loadAvailable()
    return
  }
  const res = await getAgentModels(props.agentId)
  if (res.code !== 0) return
  available.value = res.data.available || []
  if (!available.value.length) await loadAvailable()
  rows.value = (res.data.models || []).map(m => ({
    modelId: m.modelId,
    role: m.role,
    isPrimary: !!m.isPrimary,
    priority: m.priority || 10,
    params: m.params || '',
    enabled: m.enabled !== false
  }))
}

// 新建场景：清空已填行
function reset() {
  rows.value = []
  loadAvailable()
}

/**
 * 保存模型绑定。
 * @param {number|null} agentId 允许外部传入（新建场景下先建智能体拿到 ID 再调用）
 */
async function save(agentId) {
  const id = agentId ?? props.agentId
  if (!id) return false

  for (const r of rows.value) {
    if (!r.modelId) {
      ElMessage.warning('存在未选择模型的行，请先补全')
      return false
    }
    if (r.params && !isValidJSON(r.params)) {
      ElMessage.warning('参数覆写必须是合法 JSON，例如 {"temperature":0.7}')
      return false
    }
  }
  const res = await saveAgentModels(id, rows.value)
  if (res.code === 0) {
    ElMessage.success('模型配置已保存')
    return true
  }
  ElMessage.error(res.message || '模型配置保存失败')
  return false
}

function isValidJSON(s) {
  try {
    JSON.parse(s)
    return true
  } catch (e) {
    return false
  }
}

watch(() => props.agentId, load, { immediate: true })

defineExpose({ load, save, reset, rows })
</script>

<style scoped>
.mb-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
}
.mb-title { font-size: 14px; font-weight: 600; color: var(--text); }
.mb-sub {
  display: block;
  font-size: 12px;
  font-weight: 400;
  color: var(--text-muted);
  margin-top: 2px;
}
.mb-tip { margin-top: 10px; font-size: 12px; color: var(--text-muted); line-height: 1.6; }
.opt-meta { float: right; color: var(--text-secondary); font-size: 12px; }

/* 表头浅底 + 行内控件垂直居中，避免多行时高低不齐 */
:deep(.el-table th.el-table__cell) { background: var(--bg-subtle, #fafbfc); }
:deep(.el-table td.el-table__cell) { vertical-align: middle; }
</style>
