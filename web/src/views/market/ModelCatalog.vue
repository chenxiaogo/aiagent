<template>
  <div class="page-container">
    <el-tabs v-model="tab">
      <!-- 模型目录 -->
      <el-tab-pane label="模型目录" name="models">
        <div class="page-header">
          <div>
            <h2 class="page-title">模型目录</h2>
            <p class="page-sub">已接入的模型与单价（成本 = token × 单价），单价可在「系统设置 → 大模型配置」维护。</p>
          </div>
        </div>
        <el-card shadow="never">
          <el-table :data="models" v-loading="loadingModels" stripe>
            <el-table-column prop="provider" label="厂商" width="120" />
            <el-table-column prop="modelName" label="模型" min-width="180" />
            <el-table-column prop="modelType" label="类型" width="120" />
            <el-table-column label="输入单价(元/1K)" width="140">
              <template #default="{ row }">{{ (row.priceInPer1k / 100).toFixed(4) }}</template>
            </el-table-column>
            <el-table-column label="输出单价(元/1K)" width="140">
              <template #default="{ row }">{{ (row.priceOutPer1k / 100).toFixed(4) }}</template>
            </el-table-column>
            <el-table-column label="计费" width="90">
              <template #default="{ row }">{{ row.billingType === 'time' ? '时长' : 'token' }}</template>
            </el-table-column>
            <el-table-column label="操作" width="100" fixed="right">
              <template #default="{ row }">
                <el-button link type="primary" @click="openPrice(row)">改价</el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-tab-pane>

      <!-- 模型路由 -->
      <el-tab-pane label="模型路由" name="routing">
        <div class="page-header">
          <div>
            <h2 class="page-title">模型路由规则</h2>
            <p class="page-sub">平台级策略：按智能体分类 / 关键词 / 成本选择模型，优先级数字小者先匹配。</p>
          </div>
          <el-button type="primary" :icon="Plus" @click="openRule">新建规则</el-button>
        </div>
        <el-card shadow="never">
          <el-table :data="rules" v-loading="loadingRules" stripe>
            <el-table-column prop="priority" label="优先级" width="90" />
            <el-table-column prop="name" label="名称" min-width="140" />
            <el-table-column label="匹配分类" width="120">
              <template #default="{ row }">{{ row.matchCategory || '任意' }}</template>
            </el-table-column>
            <el-table-column label="匹配关键词" min-width="140">
              <template #default="{ row }">{{ row.matchKeyword || '任意' }}</template>
            </el-table-column>
            <el-table-column label="策略" width="110">
              <template #default="{ row }">
                <el-tag size="small">{{ strategyText(row.strategy) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="目标模型" min-width="160">
              <template #default="{ row }">{{ row.strategy === 'manual' ? modelName(row.targetModelId) : '—' }}</template>
            </el-table-column>
            <el-table-column label="操作" width="160" fixed="right">
              <template #default="{ row }">
                <el-button link type="primary" @click="openRule(row)">编辑</el-button>
                <el-button link type="danger" @click="removeRule(row)">删除</el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-tab-pane>
    </el-tabs>

    <!-- 改价 -->
    <el-dialog v-model="priceDialog" title="修改模型单价" width="420px">
      <el-form label-width="110px">
        <el-form-item label="输入单价(分/1K)">
          <el-input-number v-model="priceForm.priceInPer1k" :min="0" :step="0.1" />
        </el-form-item>
        <el-form-item label="输出单价(分/1K)">
          <el-input-number v-model="priceForm.priceOutPer1k" :min="0" :step="0.1" />
        </el-form-item>
        <el-form-item label="计费方式">
          <el-select v-model="priceForm.billingType" style="width:100%">
            <el-option label="token" value="token" />
            <el-option label="时长" value="time" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="priceDialog = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="savePrice">保存</el-button>
      </template>
    </el-dialog>

    <!-- 路由规则 -->
    <el-dialog v-model="ruleDialog" :title="ruleForm.id ? '编辑规则' : '新建规则'" width="520px">
      <el-form :model="ruleForm" label-width="100px">
        <el-form-item label="名称">
          <el-input v-model="ruleForm.name" />
        </el-form-item>
        <el-form-item label="匹配分类">
          <el-select v-model="ruleForm.matchCategory" clearable placeholder="任意" style="width:100%">
            <el-option v-for="c in categories" :key="c.value" :label="c.label" :value="c.value" />
          </el-select>
        </el-form-item>
        <el-form-item label="匹配关键词">
          <el-input v-model="ruleForm.matchKeyword" placeholder="逗号分隔，如 报告,总结" />
        </el-form-item>
        <el-form-item label="策略">
          <el-select v-model="ruleForm.strategy" style="width:100%">
            <el-option label="成本优先" value="cost" />
            <el-option label="智能路由" value="smart" />
            <el-option label="指定模型" value="manual" />
          </el-select>
        </el-form-item>
        <el-form-item label="目标模型" v-if="ruleForm.strategy === 'manual'">
          <el-select v-model="ruleForm.targetModelId" placeholder="选择模型" style="width:100%">
            <el-option v-for="m in chatModels" :key="m.id" :label="m.modelName" :value="m.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="优先级">
          <el-input-number v-model="ruleForm.priority" :min="1" :max="999" />
        </el-form-item>
        <el-form-item label="启用">
          <el-switch v-model="ruleForm.enabled" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="ruleDialog = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="saveRule">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted, computed } from 'vue'
import { Plus } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  listMarketModels, updateModelPrice,
  listRoutingRules, saveRoutingRule, deleteRoutingRule
} from '@/api/market'

const categories = [
  { value: 'video', label: '视频检索' },
  { value: 'camera', label: '摄像头检索' },
  { value: 'doc', label: '文档检索' },
  { value: 'report', label: '报告生成' },
  { value: 'general', label: '通用对话' }
]
const tab = ref('models')
const models = ref([])
const loadingModels = ref(false)
const rules = ref([])
const loadingRules = ref(false)
const chatModels = computed(() => models.value.filter(m => m.modelType === 'CHAT'))

const priceDialog = ref(false)
const saving = ref(false)
const priceForm = ref({ id: null, priceInPer1k: 0, priceOutPer1k: 0, billingType: 'token' })

const ruleDialog = ref(false)
const ruleForm = ref(emptyRule())
function emptyRule() {
  return { id: null, name: '', matchCategory: '', matchKeyword: '', strategy: 'cost', targetModelId: null, priority: 10, enabled: true }
}

function strategyText(s) {
  return { cost: '成本优先', smart: '智能路由', manual: '指定模型' }[s] || s
}
function modelName(id) {
  return models.value.find(m => m.id === id)?.modelName || `#${id}`
}

async function loadModels() {
  loadingModels.value = true
  try {
    const res = await listMarketModels()
    if (res.code === 0) models.value = res.data
  } finally { loadingModels.value = false }
}
async function loadRules() {
  loadingRules.value = true
  try {
    const res = await listRoutingRules()
    if (res.code === 0) rules.value = res.data
  } finally { loadingRules.value = false }
}

function openPrice(row) {
  priceForm.value = {
    id: row.id, priceInPer1k: row.priceInPer1k, priceOutPer1k: row.priceOutPer1k, billingType: row.billingType || 'token'
  }
  priceDialog.value = true
}
async function savePrice() {
  saving.value = true
  try {
    const res = await updateModelPrice(priceForm.value.id, {
      priceInPer1k: priceForm.value.priceInPer1k,
      priceOutPer1k: priceForm.value.priceOutPer1k,
      billingType: priceForm.value.billingType
    })
    if (res.code === 0) { ElMessage.success('已更新'); priceDialog.value = false; loadModels() }
  } finally { saving.value = false }
}

function openRule(row) {
  ruleForm.value = row ? { ...row } : emptyRule()
  ruleDialog.value = true
}
async function saveRule() {
  if (!['cost', 'smart', 'manual'].includes(ruleForm.value.strategy)) return ElMessage.warning('策略非法')
  if (ruleForm.value.strategy === 'manual' && !ruleForm.value.targetModelId) return ElMessage.warning('请选择目标模型')
  saving.value = true
  try {
    const res = await saveRoutingRule(ruleForm.value)
    if (res.code === 0) { ElMessage.success('已保存'); ruleDialog.value = false; loadRules() }
  } finally { saving.value = false }
}
async function removeRule(row) {
  await ElMessageBox.confirm(`确认删除规则「${row.name}」？`, '提示', { type: 'warning' })
  const res = await deleteRoutingRule(row.id)
  if (res.code === 0) { ElMessage.success('已删除'); loadRules() }
}

onMounted(() => { loadModels(); loadRules() })
</script>

<style scoped>
.page-container { padding: 20px; }
.page-header { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 16px; }
.page-title { margin: 0 0 4px; font-size: 20px; }
.page-sub { margin: 0; color: #8a8f99; font-size: 13px; }
</style>
