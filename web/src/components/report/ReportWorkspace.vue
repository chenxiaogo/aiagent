<template>
  <div class="report-workspace">
    <div class="rw-toolbar">
      <el-select v-model="filter.reportType" placeholder="全部类型" clearable style="width: 140px" @change="loadList">
        <el-option label="分析报告" value="analysis" />
        <el-option label="摘要报告" value="summary" />
        <el-option label="自定义" value="custom" />
      </el-select>
      <div class="rw-toolbar-right">
        <el-button :icon="Refresh" circle title="刷新" @click="loadList" />
        <el-button type="primary" :icon="Plus" @click="handleCreate">生成报告</el-button>
      </div>
    </div>

    <div v-loading="loading" class="rw-body">
      <div class="report-grid">
        <div v-for="report in list" :key="report.id" class="report-card">
          <div class="report-card-header">
            <div class="report-icon">📊</div>
            <div class="report-info">
              <div class="report-title">{{ report.title }}</div>
              <div class="report-meta">
                <el-tag size="small" :type="reportTypeType(report.reportType)">{{ reportTypeText(report.reportType) }}</el-tag>
                <el-tag size="small" :type="statusType(report.status)">{{ statusText(report.status) }}</el-tag>
              </div>
            </div>
          </div>
          <div class="report-preview">
            {{ report.content ? stripHtml(report.content).slice(0, 120) + '...' : '暂无内容' }}
          </div>
          <div class="report-footer">
            <span>{{ report.creatorName }}</span>
            <span>{{ formatTime(report.createdAt) }}</span>
          </div>
          <div class="report-actions">
            <el-button size="small" :icon="View" @click="handleView(report)">查看</el-button>
            <el-button size="small" :icon="Download" :disabled="report.status !== 'ready'" @click="handleDownload(report)">下载 HTML</el-button>
            <el-button size="small" type="danger" :icon="Delete" @click="handleDelete(report)">删除</el-button>
          </div>
        </div>
      </div>

      <el-empty v-if="!loading && !list.length" description="该智能体还没有报告，点击右上角生成" />

      <div v-if="total" class="pagination-wrap">
        <el-pagination
          v-model:current-page="filter.page"
          v-model:page-size="filter.pageSize"
          :total="total"
          :page-sizes="[9, 18, 36]"
          layout="total, sizes, prev, pager, next, jumper"
          @size-change="loadList"
          @current-change="loadList"
        />
      </div>
    </div>

    <!-- 查看报告 -->
    <el-dialog v-model="viewVisible" :title="currentReport?.title || '报告详情'" width="900px" top="5vh">
      <div v-if="currentReport" class="report-view">
        <div class="report-view-meta">
          <el-tag :type="statusType(currentReport.status)">{{ statusText(currentReport.status) }}</el-tag>
          <span>{{ formatTime(currentReport.createdAt) }}</span>
        </div>
        <div v-if="currentReport.status === 'generating'" class="report-generating">
          <el-icon class="is-loading"><Loading /></el-icon>
          <span>报告生成中...</span>
        </div>
        <div v-else-if="currentReport.htmlContent" class="report-html" v-html="currentReport.htmlContent"></div>
        <div v-else-if="currentReport.content" class="report-markdown">{{ currentReport.content }}</div>
        <div v-else class="report-empty">暂无内容</div>
      </div>
      <template #footer>
        <el-button @click="viewVisible = false">关闭</el-button>
        <el-button type="primary" :icon="Download" :disabled="currentReport?.status !== 'ready'" @click="handleDownload(currentReport)">
          下载 HTML
        </el-button>
      </template>
    </el-dialog>

    <!-- 生成报告 -->
    <el-dialog v-model="createVisible" title="生成报告" width="500px">
      <el-form :model="createForm" :rules="createRules" ref="createFormRef" label-width="100px">
        <el-form-item label="报告标题" prop="title">
          <el-input v-model="createForm.title" placeholder="请输入报告标题" />
        </el-form-item>
        <el-form-item label="报告类型" prop="reportType">
          <el-select v-model="createForm.reportType" style="width: 100%">
            <el-option label="分析报告" value="analysis" />
            <el-option label="摘要报告" value="summary" />
            <el-option label="自定义" value="custom" />
          </el-select>
        </el-form-item>
        <el-form-item label="关联视频">
          <el-input v-model="createForm.videoIds" placeholder="视频ID，逗号分隔" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSubmitCreate">生成</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, watch, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, View, Download, Delete, Loading, Refresh } from '@element-plus/icons-vue'
import { getReportList, createReport, deleteReport, downloadReportHTML, getReport } from '@/api/report'

// 作为智能体工作台形态使用：报告归属于传入的智能体
const props = defineProps({
  agentId: { type: Number, required: true },
  category: { type: String, default: 'report' }
})

const list = ref([])
const total = ref(0)
const loading = ref(false)
const filter = reactive({ reportType: '', page: 1, pageSize: 9 })

const viewVisible = ref(false)
const currentReport = ref(null)
const createVisible = ref(false)
const createFormRef = ref(null)
const createForm = reactive({ title: '', reportType: 'analysis', videoIds: '' })
const createRules = {
  title: [{ required: true, message: '请输入标题', trigger: 'blur' }]
}

async function loadList() {
  if (!props.agentId) return
  loading.value = true
  try {
    const res = await getReportList({ ...filter, agentId: props.agentId })
    if (res.code === 0) {
      list.value = res.data.list || []
      total.value = res.data.total || 0
    }
  } catch (e) {
    ElMessage.error('加载失败')
  } finally {
    loading.value = false
  }
}

function handleCreate() {
  createForm.title = ''
  createForm.reportType = 'analysis'
  createForm.videoIds = ''
  createVisible.value = true
}

async function handleSubmitCreate() {
  await createFormRef.value.validate()
  try {
    const res = await createReport({ ...createForm, agentId: props.agentId })
    if (res.code === 0) {
      ElMessage.success('报告生成任务已提交')
      createVisible.value = false
      loadList()
    }
  } catch (e) {
    ElMessage.error('创建失败')
  }
}

async function handleView(report) {
  currentReport.value = report
  viewVisible.value = true
  try {
    const res = await getReport(report.id)
    if (res.code === 0) currentReport.value = res.data
  } catch (e) { /* 保留列表数据即可 */ }
}

async function handleDownload(report) {
  try {
    const blob = await downloadReportHTML(report.id)
    const url = window.URL.createObjectURL(new Blob([blob]))
    const a = document.createElement('a')
    a.href = url
    a.download = `${report.title}.html`
    a.click()
    window.URL.revokeObjectURL(url)
  } catch (e) {
    ElMessage.error('下载失败')
  }
}

async function handleDelete(report) {
  await ElMessageBox.confirm(`确定删除报告「${report.title}」？`, '提示', { type: 'warning' })
  try {
    await deleteReport(report.id)
    ElMessage.success('删除成功')
    loadList()
  } catch (e) {
    ElMessage.error('删除失败')
  }
}

function statusText(s) {
  return { generating: '生成中', ready: '已完成', failed: '失败' }[s] || s
}
function statusType(s) {
  return { generating: 'warning', ready: 'success', failed: 'danger' }[s] || 'info'
}
function reportTypeText(t) {
  return { analysis: '分析报告', summary: '摘要报告', custom: '自定义' }[t] || t
}
function reportTypeType(t) {
  return { analysis: 'primary', summary: 'success', custom: 'info' }[t] || 'info'
}
function stripHtml(html) {
  const tmp = document.createElement('div')
  tmp.innerHTML = html
  return tmp.textContent || tmp.innerText || ''
}
function formatTime(t) {
  if (!t) return '—'
  const d = new Date(t)
  if (Number.isNaN(d.getTime())) return '—'
  const p = n => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

onMounted(loadList)
watch(() => props.agentId, loadList)
</script>

<style scoped>
.report-workspace {
  display: flex;
  flex-direction: column;
  gap: 16px;
  height: 100%;
  min-height: 0;
}
.rw-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.rw-toolbar-right { display: flex; gap: 8px; }

.rw-body { flex: 1; min-height: 0; overflow-y: auto; }

.report-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: 14px;
}
.report-card {
  display: flex;
  flex-direction: column;
  padding: 16px;
  background: var(--card-bg);
  border: 1px solid var(--border-light);
  border-radius: var(--radius-md, 10px);
  transition: transform .2s, box-shadow .2s;
}
.report-card:hover { transform: translateY(-2px); box-shadow: var(--shadow-md); }
.report-card-header { display: flex; gap: 12px; margin-bottom: 10px; }
.report-icon { font-size: 24px; }
.report-info { min-width: 0; }
.report-title { font-size: 14px; font-weight: 600; color: var(--text); margin-bottom: 6px; }
.report-meta { display: flex; gap: 6px; }
.report-preview {
  font-size: 12px; color: var(--text-secondary); line-height: 1.6;
  display: -webkit-box; -webkit-line-clamp: 3; -webkit-box-orient: vertical; overflow: hidden;
  min-height: 54px;
}
.report-footer {
  display: flex; justify-content: space-between;
  font-size: 12px; color: var(--text-muted);
  padding: 8px 0; border-top: 1px solid var(--border-light); margin-top: 8px;
}
.report-actions { display: flex; gap: 6px; }
.pagination-wrap { display: flex; justify-content: center; margin-top: 18px; }

.report-generating { display: flex; align-items: center; gap: 8px; color: var(--text-secondary); }
.report-html, .report-markdown { max-height: 60vh; overflow-y: auto; }
.report-markdown { white-space: pre-wrap; line-height: 1.7; }
.report-empty { color: var(--text-muted); }
.report-view-meta { display: flex; align-items: center; gap: 10px; margin-bottom: 12px; color: var(--text-muted); font-size: 12px; }
</style>
