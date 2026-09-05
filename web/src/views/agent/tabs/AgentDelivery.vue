<template>
  <div v-loading="loading">
    <!-- 产品信息 -->
    <div class="app-card section">
      <div class="card-title">
        产品信息
        <span class="sub">客户购买与看到的内容</span>
      </div>
      <el-form :model="productForm" label-width="96px" class="product-form">
        <el-form-item label="产品名称">
          <el-input v-model="productForm.name" placeholder="对外展示的产品名，如：AI 视频分析助手" />
        </el-form-item>
        <el-form-item label="产品简介">
          <el-input v-model="productForm.summary" type="textarea" :rows="2" maxlength="200" show-word-limit placeholder="一句话说明这个产品能帮客户解决什么" />
        </el-form-item>
        <el-form-item label="交付方式">
          <el-checkbox-group v-model="productForm.deliveryModes">
            <el-checkbox v-for="m in DELIVERY_MODES" :key="m.value" :value="m.value" :disabled="!m.ready">
              {{ m.label }}
              <el-tag v-if="!m.ready" size="small" type="info" effect="plain">规划中</el-tag>
            </el-checkbox>
          </el-checkbox-group>
        </el-form-item>
        <el-form-item label="上架状态">
          <el-radio-group v-model="productForm.status">
            <el-radio value="draft">未上架</el-radio>
            <el-radio value="online">已上架</el-radio>
            <el-radio value="offline">已下架</el-radio>
          </el-radio-group>
          <span class="tip">未发布任何版本时无法上架</span>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :loading="saving" @click="saveProduct">保存产品信息</el-button>
        </el-form-item>
      </el-form>
    </div>

    <!-- 接入端点 -->
    <div class="app-card section">
      <div class="card-title">
        接入端点
        <span class="sub">客户凭 API Key 调用</span>
      </div>
      <div class="endpoint-list">
        <div v-for="ep in endpointList" :key="ep.key" class="endpoint-row">
          <div class="ep-info">
            <span class="ep-label">{{ ep.label }}</span>
            <el-tag v-if="!ep.ready" size="small" type="info" effect="plain">规划中</el-tag>
          </div>
          <code class="ep-url">{{ ep.url }}</code>
          <el-button size="small" :icon="DocumentCopy" @click="copy(ep.url)">复制</el-button>
        </div>
      </div>
    </div>

    <!-- 接入示例 -->
    <div class="app-card section">
      <div class="card-title">接入示例</div>
      <el-tabs v-model="snippetTab">
        <el-tab-pane label="Claude / Cursor" name="claude">
          <pre class="code">{{ snippets.claude }}</pre>
          <el-button size="small" :icon="DocumentCopy" @click="copy(snippets.claude)">复制配置</el-button>
        </el-tab-pane>
        <el-tab-pane label="cURL" name="curl">
          <pre class="code">{{ snippets.curl }}</pre>
          <el-button size="small" :icon="DocumentCopy" @click="copy(snippets.curl)">复制命令</el-button>
        </el-tab-pane>
        <el-tab-pane label="Python" name="python">
          <pre class="code">{{ snippets.python }}</pre>
          <el-button size="small" :icon="DocumentCopy" @click="copy(snippets.python)">复制代码</el-button>
        </el-tab-pane>
      </el-tabs>
      <div class="snippet-tip">把示例中的「你的API-Key」替换为下方凭据列表里的 Key。</div>
    </div>

    <!-- 暴露能力 -->
    <div class="app-card section">
      <div class="card-title">
        当前版本暴露的能力
        <span class="sub">{{ currentVersion }}</span>
      </div>
      <div class="capability-block">
        <div class="cap-label">工具</div>
        <div class="chips">
          <el-tag v-for="t in tools" :key="t" effect="plain">{{ t }}</el-tag>
          <span v-if="!tools.length" class="muted">尚未暴露任何工具</span>
        </div>
      </div>
      <div class="capability-block">
        <div class="cap-label">预设问题</div>
        <div class="chips">
          <el-tag v-for="(q, i) in presetQuestions" :key="i" type="info" effect="plain">{{ q }}</el-tag>
          <span v-if="!presetQuestions.length" class="muted">暂无</span>
        </div>
      </div>
    </div>

    <!-- 用量 -->
    <div class="app-card section">
      <div class="card-title">
        用量
        <span class="sub">近 7 日</span>
      </div>
      <div class="usage-grid">
        <div class="usage-item">
          <div class="usage-value">{{ usage.totalRequests ?? 0 }}</div>
          <div class="usage-label">累计调用</div>
        </div>
        <div class="usage-item">
          <div class="usage-value">{{ usage.todayRequests ?? 0 }}</div>
          <div class="usage-label">今日调用</div>
        </div>
        <div class="usage-item">
          <div class="usage-value">{{ usage.totalToolCalls ?? 0 }}</div>
          <div class="usage-label">工具调用</div>
        </div>
        <div class="usage-item">
          <div class="usage-value">{{ usage.totalErrors ?? 0 }}</div>
          <div class="usage-label">错误数</div>
        </div>
        <div class="usage-item">
          <div class="usage-value">{{ usage.avgLatencyMs ?? 0 }}<small>ms</small></div>
          <div class="usage-label">平均耗时</div>
        </div>
      </div>
    </div>

    <!-- 客户授权 -->
    <div class="app-card section">
      <div class="card-title">
        客户授权
        <el-button size="small" type="primary" :icon="Plus" @click="openSubDialog">授权客户</el-button>
      </div>
      <el-table :data="subscriptions" style="width: 100%">
        <el-table-column prop="tenantName" label="客户" min-width="140" />
        <el-table-column label="使用版本" width="160">
          <template #default="{ row }">
            <el-tag :type="row.pinnedReleaseId ? 'warning' : 'success'" size="small" effect="plain">
              {{ row.pinnedVersionText }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="planName" label="套餐" width="120">
          <template #default="{ row }">{{ row.planName || '按量' }}</template>
        </el-table-column>
        <el-table-column prop="status" label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="row.status === 'active' ? 'success' : 'info'" size="small" effect="plain">
              {{ row.status === 'active' ? '生效中' : row.status }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="endedAt" label="到期时间" width="180">
          <template #default="{ row }">{{ row.endedAt ? formatTime(row.endedAt) : '长期有效' }}</template>
        </el-table-column>
        <el-table-column label="操作" width="180" fixed="right">
          <template #default="{ row }">
            <el-button size="small" :icon="Edit" @click="editPinnedVersion(row)">指定版本</el-button>
            <el-button size="small" type="danger" :icon="Delete" @click="removeSubscription(row)">取消</el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!subscriptions.length" description="尚未授权任何客户" :image-size="72" />
    </div>

    <!-- 访问凭据 -->
    <div class="app-card section">
      <div class="card-title">
        访问凭据
        <el-button size="small" type="primary" :icon="Plus" @click="openClientDialog">新建 API Key</el-button>
      </div>
      <el-table :data="clients" style="width: 100%">
        <el-table-column prop="name" label="名称" min-width="140" />
        <el-table-column prop="keyPrefix" label="Key" width="140">
          <template #default="{ row }"><code>{{ row.keyPrefix }}…</code></template>
        </el-table-column>
        <el-table-column prop="tenantName" label="归属客户" width="120">
          <template #default="{ row }">{{ row.tenantName || '—' }}</template>
        </el-table-column>
        <el-table-column label="作用域" width="180">
          <template #default="{ row }">
            <el-tag v-for="s in scopeList(row.scopes)" :key="s" size="small" effect="plain" style="margin-right:4px">
              {{ s }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="配额" width="140">
          <template #default="{ row }">{{ row.quotaRpm }}/分 · {{ row.quotaTpd }}/日</template>
        </el-table-column>
        <el-table-column label="调用次数" width="100" prop="requests" />
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag :type="row.status === 'active' ? 'success' : 'danger'" size="small" effect="plain">
              {{ row.status === 'active' ? '启用' : '已吊销' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="lastUsedAt" label="最后使用" width="180">
          <template #default="{ row }">{{ row.lastUsedAt ? formatTime(row.lastUsedAt) : '从未' }}</template>
        </el-table-column>
        <el-table-column label="操作" width="150" fixed="right">
          <template #default="{ row }">
            <el-button v-if="row.status === 'active'" size="small" :icon="CircleClose" @click="revokeClient(row)">吊销</el-button>
            <el-button size="small" type="danger" :icon="Delete" @click="removeClient(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!clients.length" description="尚未创建 API Key，客户无法调用" :image-size="72" />
    </div>

    <!-- 新建凭据 -->
    <el-dialog v-model="clientDialog" title="新建 API Key" width="520px">
      <el-form :model="clientForm" label-width="110px">
        <el-form-item label="名称" required>
          <el-input v-model="clientForm.name" placeholder="如：客户A-生产" />
        </el-form-item>
        <el-form-item label="归属客户">
          <el-select v-model="clientForm.userId" filterable clearable placeholder="从系统用户中选择客户" style="width:100%">
            <el-option v-for="u in tenantUsers" :key="u.id" :label="userLabel(u)" :value="u.id">
              <span>{{ userLabel(u) }}</span>
              <el-tag v-if="u.tenantId" size="small" type="info" effect="plain" class="opt-tag">已是客户</el-tag>
            </el-option>
          </el-select>
          <div class="form-tip">客户即平台用户；该用户尚未建过客户时会自动为其创建</div>
        </el-form-item>
        <el-form-item label="作用域">
          <el-checkbox-group v-model="clientForm.scopes">
            <el-checkbox value="mcp">MCP</el-checkbox>
            <el-checkbox value="chat_api">API</el-checkbox>
            <el-checkbox value="portal">门户</el-checkbox>
          </el-checkbox-group>
        </el-form-item>
        <el-form-item label="钉住版本">
          <el-select v-model="clientForm.pinnedVersion" clearable placeholder="留空则跟随默认版本" style="width:100%">
            <el-option v-for="r in releaseOptions" :key="r.id" :label="r.version" :value="r.version" />
          </el-select>
        </el-form-item>
        <el-form-item label="IP 白名单">
          <el-input v-model="clientForm.ipAllowList" placeholder="逗号分隔，留空不限制" />
        </el-form-item>
        <el-form-item label="配额">
          <el-input-number v-model="clientForm.quotaRpm" :min="1" :max="6000" />
          <span class="unit">次/分</span>
          <el-input-number v-model="clientForm.quotaTpd" :min="1" :max="1000000" />
          <span class="unit">次/日</span>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="clientDialog = false">取消</el-button>
        <el-button type="primary" @click="submitClient">创建</el-button>
      </template>
    </el-dialog>

    <!-- 凭据明文展示（仅一次） -->
    <el-dialog v-model="keyDialog" title="请立即保存 API Key" width="520px">
      <el-alert type="warning" :closable="false" show-icon title="关闭后将无法再次查看完整 Key" />
      <div class="plain-key">
        <code>{{ plainKey }}</code>
        <el-button type="primary" size="small" :icon="DocumentCopy" @click="copy(plainKey)">复制</el-button>
      </div>
      <template #footer>
        <el-button type="primary" @click="keyDialog = false">我已保存</el-button>
      </template>
    </el-dialog>

    <!-- 授权客户 -->
    <el-dialog v-model="subDialog" title="授权客户" width="480px">
      <el-form :model="subForm" label-width="100px">
        <el-form-item label="客户" required>
          <el-select v-model="subForm.userId" filterable placeholder="从系统用户中选择客户" style="width:100%">
            <el-option v-for="u in tenantUsers" :key="u.id" :label="userLabel(u)" :value="u.id">
              <span>{{ userLabel(u) }}</span>
              <el-tag v-if="u.tenantId" size="small" type="info" effect="plain" class="opt-tag">已是客户</el-tag>
            </el-option>
          </el-select>
        </el-form-item>
        <el-form-item label="使用版本">
          <el-select v-model="subForm.pinnedReleaseId" clearable placeholder="留空则跟随最新版" style="width:100%">
            <el-option v-for="r in releaseOptions" :key="r.id" :label="r.version" :value="r.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="到期时间">
          <el-date-picker
            v-model="subForm.endedAt"
            type="date"
            value-format="YYYY-MM-DDTHH:mm:ssZ"
            placeholder="留空为长期有效"
            style="width:100%"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="subDialog = false">取消</el-button>
        <el-button type="primary" @click="submitSubscription">确认授权</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Edit, Delete, DocumentCopy, CircleClose } from '@element-plus/icons-vue'
import {
  getAgentDelivery, updateAgentDelivery,
  getAgentClients, createAgentClient, revokeAgentClient, deleteAgentClient,
  getAgentSubscriptions, createAgentSubscription, updateAgentSubscription, deleteAgentSubscription,
  getAgentUsage
} from '@/api/agent'
import { listTenantCandidates } from '@/api/tenant'

const route = useRoute()
const agentId = computed(() => Number(route.params.id))

// 交付方式：R1 仅 MCP 可用，其余随 R2 客户门户开放
const DELIVERY_MODES = [
  { value: 'mcp', label: 'MCP', ready: true },
  { value: 'api', label: 'API', ready: false },
  { value: 'web', label: 'Web 门户', ready: false },
  { value: 'sdk', label: 'SDK', ready: false },
  { value: 'embed', label: 'Embed 嵌入', ready: false }
]

const loading = ref(false)
const saving = ref(false)
const data = ref({})
const clients = ref([])
const subscriptions = ref([])
const usage = ref({})
const snippetTab = ref('claude')

const productForm = reactive({ name: '', summary: '', deliveryModes: [], status: 'draft' })

const agent = computed(() => data.value.agent || {})
const product = computed(() => data.value.product || {})
const endpoints = computed(() => data.value.endpoints || {})
const snippets = computed(() => data.value.snippets || {})
const tools = computed(() => data.value.tools || [])
const presetQuestions = computed(() => data.value.presetQuestions || [])
const releaseOptions = computed(() => data.value.releases || [])
const currentVersion = computed(() => data.value.currentVersion || '未发布')

const endpointList = computed(() => {
  const e = endpoints.value
  return [
    { key: 'mcpStream', label: 'MCP（HTTP）', url: e.mcpStream, ready: true },
    { key: 'mcpSse', label: 'MCP（SSE）', url: e.mcpSse, ready: true },
    { key: 'chatApi', label: 'OpenAI 兼容 API', url: e.chatApi, ready: false },
    { key: 'portal', label: '客户门户', url: e.portal, ready: false }
  ].filter(i => i.url)
})

// ---------- 凭据 ----------
// 可授权的客户 = 平台用户。选中的用户若还没建过客户，后端会自动为其创建。
const tenantUsers = ref([])

function userLabel(u) {
  const name = u.nickname || u.username
  return name === u.username ? u.username : `${name}（${u.username}）`
}

async function loadTenantUsers() {
  try {
    const res = await listTenantCandidates()
    tenantUsers.value = res.data || []
  } catch (e) {
    tenantUsers.value = []
  }
}

const clientDialog = ref(false)
const keyDialog = ref(false)
const plainKey = ref('')
const clientForm = reactive({
  name: '', userId: null, scopes: ['mcp'], pinnedVersion: '',
  ipAllowList: '', quotaRpm: 60, quotaTpd: 10000
})

function openClientDialog() {
  Object.assign(clientForm, {
    name: '', userId: null, scopes: ['mcp'], pinnedVersion: '',
    ipAllowList: '', quotaRpm: 60, quotaTpd: 10000
  })
  loadTenantUsers()
  clientDialog.value = true
}

async function submitClient() {
  if (!clientForm.name) return ElMessage.warning('请填写凭据名称')
  const res = await createAgentClient(agentId.value, { ...clientForm })
  if (res.code === 0) {
    plainKey.value = res.data.plainKey
    clientDialog.value = false
    keyDialog.value = true
    loadClients()
  } else {
    ElMessage.error(res.message || '创建失败')
  }
}

async function revokeClient(row) {
  await ElMessageBox.confirm(`确定吊销凭据「${row.name}」？使用该 Key 的调用将立即失败。`, '提示', { type: 'warning' })
  const res = await revokeAgentClient(agentId.value, row.id)
  if (res.code === 0) { ElMessage.success('已吊销'); loadClients() }
}

async function removeClient(row) {
  await ElMessageBox.confirm(`确定删除凭据「${row.name}」？`, '提示', { type: 'warning' })
  const res = await deleteAgentClient(agentId.value, row.id)
  if (res.code === 0) { ElMessage.success('已删除'); loadClients() }
}

// ---------- 客户授权 ----------
const subDialog = ref(false)
const subForm = reactive({ userId: null, pinnedReleaseId: null, endedAt: '' })

function openSubDialog() {
  Object.assign(subForm, { userId: null, pinnedReleaseId: null, endedAt: '' })
  loadTenantUsers()
  subDialog.value = true
}

async function submitSubscription() {
  if (!subForm.userId) return ElMessage.warning('请选择客户')
  const res = await createAgentSubscription(agentId.value, { ...subForm })
  if (res.code === 0) { ElMessage.success('已授权'); subDialog.value = false; loadSubscriptions() }
  else ElMessage.error(res.message || '授权失败')
}

async function editPinnedVersion(row) {
  const { value } = await ElMessageBox.prompt(
    '留空表示跟随产品默认版本（latest）；选择具体版本后该客户将固定在此版本，不受后续发布影响。',
    `指定 ${row.tenantName} 使用的版本`,
    { inputType: 'text', inputPlaceholder: '留空 = 跟随最新版' }
  ).catch(() => ({ value: undefined }))
  if (value === undefined) return

  let releaseId = 0
  if (value) {
    const hit = releaseOptions.value.find(r => r.version === value.trim())
    if (!hit) return ElMessage.error(`版本 ${value} 不存在`)
    releaseId = hit.id
  }
  const res = await updateAgentSubscription(row.id, { pinnedReleaseId: releaseId })
  if (res.code === 0) { ElMessage.success('已更新'); loadSubscriptions() }
}

async function removeSubscription(row) {
  await ElMessageBox.confirm(`确定取消「${row.tenantName}」的授权？`, '提示', { type: 'warning' })
  const res = await deleteAgentSubscription(row.id)
  if (res.code === 0) { ElMessage.success('已取消授权'); loadSubscriptions() }
}

// ---------- 产品信息 ----------
async function saveProduct() {
  saving.value = true
  try {
    const res = await updateAgentDelivery(agentId.value, { ...productForm })
    if (res.code === 0) {
      ElMessage.success('已保存')
      data.value.product = res.data
    } else {
      ElMessage.error(res.message || '保存失败')
    }
  } finally {
    saving.value = false
  }
}

// ---------- 加载 ----------
async function loadDelivery() {
  const res = await getAgentDelivery(agentId.value)
  if (res.code === 0) {
    data.value = res.data || {}
    const p = res.data.product || {}
    Object.assign(productForm, {
      name: p.name || '',
      summary: p.summary || '',
      deliveryModes: res.data.deliveryModes || [],
      status: p.status || 'draft'
    })
  }
}
async function loadClients() {
  const res = await getAgentClients(agentId.value)
  if (res.code === 0) clients.value = res.data || []
}
async function loadSubscriptions() {
  const res = await getAgentSubscriptions(agentId.value)
  if (res.code === 0) subscriptions.value = res.data || []
}
async function loadUsage() {
  const res = await getAgentUsage(agentId.value, 7)
  if (res.code === 0) usage.value = res.data || {}
}

async function loadAll() {
  loading.value = true
  try {
    await Promise.all([loadDelivery(), loadClients(), loadSubscriptions(), loadUsage()])
  } finally {
    loading.value = false
  }
}

function scopeList(scopes) {
  return (scopes || '').split(',').map(s => s.trim()).filter(Boolean)
}

async function copy(text) {
  if (!text) return
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success('已复制')
  } catch (e) {
    ElMessage.warning('复制失败，请手动选择')
  }
}

function formatTime(t) {
  if (!t) return '—'
  const d = new Date(t)
  if (Number.isNaN(d.getTime())) return '—'
  const p = n => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

onMounted(() => {
  loadAll()
  loadTenantUsers()
})
</script>

<style scoped>
.section { padding: 20px 24px; margin-bottom: 16px; }
.card-title {
  display: flex; align-items: center; gap: 12px;
  font-size: 15px; font-weight: 600; color: var(--text); margin-bottom: 14px;
}
.sub { font-size: 12px; font-weight: 400; color: var(--text-muted); }
.card-title .el-button { margin-left: auto; }

.product-form { max-width: 720px; }
.tip { font-size: 12px; color: var(--text-muted); margin-left: 12px; }
.unit { font-size: 12px; color: var(--text-muted); margin: 0 12px 0 6px; }
.form-tip { font-size: 12px; color: var(--text-muted); margin-top: 4px; line-height: 1.5; }
.opt-tag { margin-left: 8px; transform: scale(0.85); }

.endpoint-list { display: flex; flex-direction: column; gap: 10px; }
.endpoint-row { display: flex; align-items: center; gap: 12px; }
.ep-info { width: 160px; display: flex; align-items: center; gap: 6px; }
.ep-label { font-size: 13px; color: var(--text); }
.ep-url {
  flex: 1; font-family: monospace; font-size: 12px; color: var(--text-secondary);
  background: var(--bg-subtle, #f5f7fa); padding: 7px 10px; border-radius: 6px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}

.code {
  background: #1f2430; color: #d6deeb; padding: 14px 16px; border-radius: 8px;
  font-family: monospace; font-size: 12px; line-height: 1.6; overflow-x: auto; margin: 0 0 10px;
}
.snippet-tip { font-size: 12px; color: var(--text-muted); }

.capability-block { display: flex; gap: 12px; margin-bottom: 10px; }
.cap-label { width: 72px; font-size: 13px; color: var(--text-secondary); flex-shrink: 0; }
.chips { display: flex; flex-wrap: wrap; gap: 6px; }

.usage-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(120px, 1fr)); }
.usage-item { text-align: center; padding: 8px 0; }
.usage-value { font-size: 22px; font-weight: 700; color: var(--text); }
.usage-value small { font-size: 12px; font-weight: 400; color: var(--text-muted); margin-left: 2px; }
.usage-label { font-size: 12px; color: var(--text-muted); margin-top: 4px; }

.plain-key {
  display: flex; align-items: center; gap: 12px; margin-top: 14px;
  background: #f5f7fa; padding: 12px; border-radius: 8px;
}
.plain-key code { flex: 1; font-family: monospace; word-break: break-all; font-size: 13px; }

.muted { font-size: 12px; color: var(--text-muted); }
</style>
