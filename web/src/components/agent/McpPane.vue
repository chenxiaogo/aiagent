<template>
  <div class="pane">
    <div class="pane-head">
      <div>
        <h3>🔌 MCP 工具</h3>
        <p class="pane-desc">
          接入外部 MCP 服务器后，该智能体即可调用其提供的工具（地图、数据库、第三方 API 等）。
        </p>
      </div>
      <div class="head-actions">
        <el-button size="small" :icon="Download" @click="openImport">从注册表导入</el-button>
        <el-button type="primary" size="small" :icon="Plus" @click="openCreate">添加 MCP</el-button>
      </div>
    </div>

    <el-empty v-if="!loading && list.length === 0" description="尚未接入 MCP 服务器" :image-size="70" />

    <div v-else class="card-list">
      <div v-for="item in list" :key="item.id" class="item-card">
        <div class="item-main">
          <div class="item-title">
            {{ item.name }}
            <el-tag size="small" :type="item.enabled ? 'success' : 'info'">
              {{ item.enabled ? '已启用' : '已停用' }}
            </el-tag>
            <el-tag size="small" effect="plain">{{ item.transport }}</el-tag>
            <el-tag v-if="item.approvalRequired === false" size="small" type="warning">免审批</el-tag>
            <el-tag v-if="item.registryId" size="small" type="info" effect="plain">注册表</el-tag>
          </div>
          <div class="item-url">{{ item.url }}</div>
        </div>
        <div class="item-actions">
          <el-button size="small" :icon="Connection" :loading="testingId === item.id" @click="handleTest(item)">
            测试
          </el-button>
          <el-button size="small" :icon="Edit" @click="openEdit(item)">编辑</el-button>
          <el-button size="small" type="danger" :icon="Delete" @click="handleDelete(item)">删除</el-button>
        </div>
      </div>
    </div>

    <!-- 测试结果显示 -->
    <el-dialog v-model="testVisible" title="MCP 连通性测试" width="560px">
      <div v-if="testResult">
        <el-alert
          v-if="testResult.ok"
          type="success"
          :closable="false"
          show-icon
          :title="`连接成功，共 ${testResult.tools?.length || 0} 个工具`"
        />
        <el-alert v-else type="error" :closable="false" show-icon :title="testResult.error || '连接失败'" />
        <div v-if="testResult.tools?.length" class="tool-list">
          <div v-for="t in testResult.tools" :key="t.name" class="tool-item">
            <span class="tool-name">{{ t.name }}</span>
            <span class="tool-desc">{{ t.description || '无描述' }}</span>
          </div>
        </div>
      </div>
    </el-dialog>

    <!-- 新增/编辑 -->
    <el-dialog v-model="dialogVisible" :title="editingId ? '编辑 MCP' : '添加 MCP'" width="560px">
      <el-form :model="form" label-width="96px">
        <el-form-item label="名称" required>
          <el-input v-model="form.name" placeholder="如：高德地图" />
        </el-form-item>
        <el-form-item label="传输方式">
          <el-radio-group v-model="form.transport">
            <el-radio value="sse">SSE</el-radio>
            <el-radio value="streamable_http">Streamable HTTP</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="端点地址" required>
          <el-input v-model="form.url" placeholder="https://mcp.example.com/sse" />
        </el-form-item>
        <el-form-item label="请求头">
          <el-input
            v-model="form.headers"
            type="textarea"
            :rows="3"
            placeholder='JSON 对象，如 {"Authorization":"Bearer xxx"}'
          />
        </el-form-item>
        <el-form-item label="启用">
          <el-switch v-model="form.enabled" />
        </el-form-item>
        <el-form-item label="免审批">
          <el-switch v-model="form.noApproval" />
          <div class="form-tip">
            开启后，该 MCP 的工具不再弹人工确认、直接执行。仅建议对只读可信的服务（如地图查询）开启。
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="handleSubmit">保存</el-button>
      </template>
    </el-dialog>

    <!-- 从 MCP 注册表导入 -->
    <el-dialog v-model="importVisible" title="从 MCP 注册表导入" width="700px">
      <el-table
        :data="registryList"
        v-loading="registryLoading"
        max-height="360"
        @selection-change="onSelectionChange"
      >
        <el-table-column type="selection" width="46" />
        <el-table-column prop="name" label="名称" min-width="130" />
        <el-table-column prop="category" label="分类" width="100" />
        <el-table-column prop="transport" label="传输" width="130" />
        <el-table-column label="免审批" width="90">
          <template #default="{ row }">
            <el-tag v-if="row.approvalRequired === false" type="warning" size="small">免审批</el-tag>
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column prop="url" label="地址" min-width="220" show-overflow-tooltip />
      </el-table>
      <div class="import-tip">已导入过的项会同步更新连接信息，不会产生重复配置。</div>
      <template #footer>
        <el-button @click="importVisible = false">取消</el-button>
        <el-button type="primary" :loading="importing" :disabled="!selected.length" @click="handleImport">
          导入{{ selected.length ? `（${selected.length}）` : '' }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Edit, Delete, Connection, Download } from '@element-plus/icons-vue'
import {
  listMcpServers, createMcpServer, updateMcpServer, deleteMcpServer, testMcpServer, importMcpFromRegistry
} from '@/api/agentTool'
import { listMCPRegistry } from '@/api/market'

const props = defineProps({ agentId: { type: Number, required: true } })

const list = ref([])
const loading = ref(false)
const dialogVisible = ref(false)
const editingId = ref(null)
const saving = ref(false)
const testingId = ref(null)
const testVisible = ref(false)
const testResult = ref(null)

// 从 MCP 注册表导入
const importVisible = ref(false)
const registryList = ref([])
const registryLoading = ref(false)
const selected = ref([])
const importing = ref(false)

const form = reactive({
  name: '',
  transport: 'sse',
  url: '',
  headers: '',
  enabled: true,
  noApproval: false // 免审批开关，提交时转换成后端的 approvalRequired
})

async function loadList() {
  loading.value = true
  try {
    const res = await listMcpServers(props.agentId)
    list.value = res.data || []
  } catch (e) {
    ElMessage.error('加载失败')
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editingId.value = null
  Object.assign(form, { name: '', transport: 'sse', url: '', headers: '', enabled: true, noApproval: false })
  dialogVisible.value = true
}

function openEdit(item) {
  editingId.value = item.id
  Object.assign(form, {
    name: item.name,
    transport: item.transport,
    url: item.url,
    headers: item.headers || '',
    enabled: item.enabled,
    // 后端 approvalRequired 为 false 才表示免审批；null / true 均视为需审批
    noApproval: item.approvalRequired === false
  })
  dialogVisible.value = true
}

async function handleSubmit() {
  if (!form.name || !form.url) {
    ElMessage.warning('名称和地址必填')
    return
  }
  saving.value = true
  try {
    // noApproval → approvalRequired（false 免审批，true 需审批）
    const payload = { ...form, approvalRequired: !form.noApproval }
    delete payload.noApproval
    if (editingId.value) {
      await updateMcpServer(editingId.value, { ...payload, agentId: props.agentId })
    } else {
      await createMcpServer(props.agentId, payload)
    }
    ElMessage.success('已保存')
    dialogVisible.value = false
    loadList()
  } catch (e) {
    ElMessage.error('保存失败')
  } finally {
    saving.value = false
  }
}

async function handleTest(item) {
  testingId.value = item.id
  try {
    const res = await testMcpServer(item.id)
    testResult.value = res.data || { ok: false, error: '无返回' }
    testVisible.value = true
  } catch (e) {
    testResult.value = { ok: false, error: e.message || '测试失败' }
    testVisible.value = true
  } finally {
    testingId.value = null
  }
}

async function handleDelete(item) {
  await ElMessageBox.confirm(`确定删除「${item.name}」？`, '提示', { type: 'warning' })
  try {
    await deleteMcpServer(item.id)
    ElMessage.success('已删除')
    loadList()
  } catch (e) {
    ElMessage.error('删除失败')
  }
}

async function openImport() {
  importVisible.value = true
  selected.value = []
  registryLoading.value = true
  try {
    const res = await listMCPRegistry()
    // 只展示启用中的注册项
    registryList.value = (res.data || []).filter((r) => r.status === 1)
  } catch (e) {
    ElMessage.error('加载注册表失败')
  } finally {
    registryLoading.value = false
  }
}

function onSelectionChange(rows) {
  selected.value = rows
}

async function handleImport() {
  if (!selected.value.length) return
  importing.value = true
  try {
    const res = await importMcpFromRegistry(props.agentId, {
      registryIds: selected.value.map((r) => r.id)
    })
    const d = res.data || {}
    ElMessage.success(`导入完成：新增 ${d.created || 0}，更新 ${d.updated || 0}，关联 ${d.linked || 0}`)
    importVisible.value = false
    loadList()
  } catch (e) {
    ElMessage.error('导入失败')
  } finally {
    importing.value = false
  }
}

onMounted(loadList)
watch(() => props.agentId, loadList)
</script>

<style scoped>
.pane {
  flex: 1;
  overflow-y: auto;
  padding: 24px;
}

.pane-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 18px;
}

.pane-head h3 { font-size: 15px; margin-bottom: 4px; }

.pane-desc {
  font-size: 12px;
  color: var(--text-secondary);
  line-height: 1.6;
  max-width: 560px;
}

.card-list { display: flex; flex-direction: column; gap: 10px; }

.item-card {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 12px 14px;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: #fff;
}

.item-main { min-width: 0; flex: 1; }

.item-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  font-weight: 600;
  margin-bottom: 4px;
}

.item-url {
  font-size: 12px;
  color: var(--text-secondary);
  font-family: monospace;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.item-actions { display: flex; gap: 6px; flex-shrink: 0; }

.tool-list {
  margin-top: 14px;
  max-height: 260px;
  overflow-y: auto;
}

.tool-item {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 8px 10px;
  border: 1px solid var(--border);
  border-radius: 8px;
  margin-bottom: 6px;
}

.tool-name { font-size: 12px; font-weight: 600; font-family: monospace; }
.tool-desc { font-size: 12px; color: var(--text-secondary); }

.head-actions { display: flex; gap: 8px; flex-shrink: 0; }

.form-tip {
  font-size: 12px;
  color: var(--text-secondary);
  line-height: 1.5;
  margin-top: 4px;
}

.import-tip {
  margin-top: 10px;
  font-size: 12px;
  color: var(--text-secondary);
}
</style>
