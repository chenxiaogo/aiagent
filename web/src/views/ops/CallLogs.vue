<template>
  <div class="page-container">
    <div class="page-header">
      <div>
        <h2 class="page-title">调用观测</h2>
        <p class="page-sub">LLM 与 MCP 工具调用明细、错误和成本下钻；向量化观测正在逐步接入统一日志链路。</p>
      </div>
    </div>

    <el-card shadow="never" class="summary-card">
      <el-row :gutter="16">
        <el-col :span="4"><div class="stat"><div class="num">{{ summary.totalCalls }}</div><div class="lbl">总调用</div></div></el-col>
        <el-col :span="4"><div class="stat"><div class="num danger">{{ summary.errorCalls }}</div><div class="lbl">失败</div></div></el-col>
        <el-col :span="4"><div class="stat"><div class="num">{{ summary.totalTokens }}</div><div class="lbl">Token</div></div></el-col>
        <el-col :span="4"><div class="stat"><div class="num warn">¥{{ (summary.totalCostCents / 100).toFixed(2) }}</div><div class="lbl">估算成本</div></div></el-col>
        <el-col :span="4"><div class="stat"><div class="num">{{ summary.avgLatencyMs }}ms</div><div class="lbl">平均延迟</div></div></el-col>
      </el-row>
    </el-card>

    <el-card shadow="never" style="margin-top:16px">
      <el-form :inline="true" class="filter">
        <el-form-item label="类型">
          <el-select v-model="filters.callType" clearable placeholder="全部" style="width:140px" @change="search">
            <el-option label="LLM" value="llm" />
            <el-option label="LLM 辅助" value="llm_aux" />
            <el-option label="MCP 工具" value="mcp_tool" />
            <el-option label="向量化" value="embedding" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-checkbox v-model="showAux" @change="search">
            包含辅助调用
          </el-checkbox>
          <el-tooltip content="标题生成、记忆摘要等后台调用，默认隐藏" placement="top">
            <el-icon class="aux-tip"><QuestionFilled /></el-icon>
          </el-tooltip>
        </el-form-item>
        <el-form-item label="状态">
          <el-select v-model="filters.status" clearable placeholder="全部" style="width:120px" @change="search">
            <el-option label="成功" :value="1" />
            <el-option label="失败" :value="0" />
          </el-select>
        </el-form-item>
        <el-form-item label="日期">
          <el-date-picker v-model="filters.day" type="daterange" value-format="YYYY-MM-DD"
            range-separator="至" start-placeholder="开始" end-placeholder="结束" @change="search" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="search">查询</el-button>
          <el-button :icon="Refresh" @click="load" style="margin-left:8px">刷新</el-button>
          <el-switch v-model="autoRefresh" active-text="自动刷新" style="margin-left:14px" @change="toggleAuto" />
        </el-form-item>
      </el-form>

      <el-table :data="logs" v-loading="loading" stripe>
        <el-table-column prop="createdAt" label="时间" width="170" />
        <el-table-column label="类型" width="100">
          <template #default="{ row }">
            <el-tag size="small" :type="tagType(row.callType)">
              {{ typeText(row.callType) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="模型/工具" min-width="160">
          <template #default="{ row }">{{ row.callType === 'mcp_tool' ? (row.toolName || '-') : (row.modelName || '-') }}</template>
        </el-table-column>
        <el-table-column prop="agentId" label="AgentID" width="90" />
        <el-table-column label="Token" width="120">
          <template #default="{ row }">{{ row.totalTokens }} ({{ row.promptTokens }}/{{ row.outputTokens }})</template>
        </el-table-column>
        <el-table-column label="成本" width="110">
          <template #default="{ row }">¥{{ (row.costCents / 100).toFixed(4) }}</template>
        </el-table-column>
        <el-table-column prop="latencyMs" label="延迟" width="90" />
        <el-table-column label="状态" width="80">
          <template #default="{ row }">
            <el-tag :type="row.status === 1 ? 'success' : 'danger'" size="small">{{ row.status === 1 ? '成功' : '失败' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="errorMsg" label="错误" min-width="160" show-overflow-tooltip />
      </el-table>

      <el-pagination v-model:current-page="page" :page-size="pageSize" :total="total"
        layout="total, prev, pager, next" style="margin-top:16px" @current-change="load" />
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, onBeforeUnmount } from 'vue'
import { Refresh, QuestionFilled } from '@element-plus/icons-vue'
import { listCallLogs, summaryCallLogs } from '@/api/market'

const logs = ref([])
const loading = ref(false)
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const autoRefresh = ref(true)
const refreshMs = 5000
let timer = null
const summary = reactive({ totalCalls: 0, errorCalls: 0, totalTokens: 0, totalCostCents: 0, avgLatencyMs: 0 })
const filters = reactive({ callType: '', status: '', day: null })
// 辅助调用（标题生成 / 记忆摘要）默认隐藏：它们数量多、与排查主链路无关
const showAux = ref(false)

function typeText(t) {
  return { llm: 'LLM', llm_aux: '辅助', mcp_tool: 'MCP 工具', embedding: '向量化' }[t] || t
}
function tagType(t) {
  if (t === 'llm') return 'primary'
  if (t === 'mcp_tool') return 'warning'
  if (t === 'llm_aux') return 'info'
  return 'info'
}
function buildParams() {
  const p = { page: page.value, pageSize: pageSize.value }
  if (filters.callType) p.callType = filters.callType
  // 没勾选「包含辅助调用」时让后端直接排除，保证分页数量正确
  if (!showAux.value && filters.callType !== 'llm_aux') p.excludeCallType = 'llm_aux'
  if (filters.status !== '') p.status = filters.status
  if (filters.day && filters.day.length === 2) {
    p.dayFrom = filters.day[0]; p.dayTo = filters.day[1]
  }
  return p
}
async function load() {
  if (loading.value) return
  loading.value = true
  try {
    const [listRes, sumRes] = await Promise.all([listCallLogs(buildParams()), summaryCallLogs(buildParams())])
    if (listRes.code === 0) { logs.value = listRes.data.list; total.value = listRes.data.total }
    if (sumRes.code === 0) Object.assign(summary, sumRes.data)
  } finally { loading.value = false }
}
function search() { page.value = 1; load() }

function startAuto() {
  stopAuto()
  if (!autoRefresh.value) return
  timer = setInterval(() => { if (!loading.value) load() }, refreshMs)
}
function stopAuto() {
  if (timer) { clearInterval(timer); timer = null }
}
function toggleAuto() {
  if (autoRefresh.value) startAuto()
  else stopAuto()
}

onMounted(() => { load(); startAuto() })
onBeforeUnmount(stopAuto)
</script>

<style scoped>
.page-container { padding: 20px; }
.page-header { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 16px; }
.page-title { margin: 0 0 4px; font-size: 20px; }
.page-sub { margin: 0; color: #8a8f99; font-size: 13px; }
.summary-card .stat { text-align: center; }
.summary-card .num { font-size: 22px; font-weight: 700; }
.summary-card .num.danger { color: #f56c6c; }
.summary-card .num.warn { color: #e6a23c; }
.summary-card .lbl { color: #8a8f99; font-size: 13px; margin-top: 4px; }
.filter { margin-bottom: 8px; }
.aux-tip { margin-left: 4px; color: var(--text-muted); cursor: help; }
</style>
