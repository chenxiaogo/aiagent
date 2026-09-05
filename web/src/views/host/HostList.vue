<template>
  <div class="page-container">
    <div class="page-header">
      <div>
        <div class="page-title">主机管理</div>
        <div class="page-subtitle">运维主机资产，按组管理，Agent 通过绑定主机组获取操作权限</div>
      </div>
      <el-button type="primary" :icon="Plus" @click="openCreateHost">新增主机</el-button>
    </div>

    <div class="main-layout">
      <!-- 左侧：主机组列表 -->
      <div class="group-panel app-card">
        <div class="panel-head">
          <span class="panel-title">主机组</span>
          <el-button link type="primary" @click="openCreateGroup">新建</el-button>
        </div>
        <div class="group-list">
          <div
            class="group-item"
            :class="{ active: currentGroupId === 0 }"
            @click="selectGroup(0)"
          >
            <span class="group-name">全部主机</span>
            <span class="group-count">{{ totalCount }}</span>
          </div>
          <div
            v-for="g in groups"
            :key="g.id"
            class="group-item"
            :class="{ active: currentGroupId === g.id }"
            @click="selectGroup(g.id)"
          >
            <span class="group-name" :title="g.name">{{ g.name }}</span>
            <span class="group-count">{{ g.hostCount }}</span>
            <div class="group-actions">
              <el-dropdown trigger="click" @command="cmd => handleGroupCmd(cmd, g)">
                <el-icon class="more-btn"><MoreFilled /></el-icon>
                <template #dropdown>
                  <el-dropdown-menu>
                    <el-dropdown-item command="edit">编辑</el-dropdown-item>
                    <el-dropdown-item command="delete" divided>删除</el-dropdown-item>
                  </el-dropdown-menu>
                </template>
              </el-dropdown>
            </div>
          </div>
        </div>
      </div>

      <!-- 右侧：主机列表 -->
      <div class="host-panel app-card">
        <div class="filter-bar">
          <el-input v-model="keyword" placeholder="搜索主机名/IP" clearable style="width:240px" @change="loadHosts" />
          <el-select v-model="statusFilter" placeholder="全部状态" clearable style="width:120px" @change="loadHosts">
            <el-option label="在线" value="online" />
            <el-option label="离线" value="offline" />
            <el-option label="待检测" value="pending" />
            <el-option label="失败" value="failed" />
          </el-select>
          <el-select v-model="roleFilter" placeholder="全部角色" clearable style="width:120px" @change="loadHosts">
            <el-option v-for="r in HOST_ROLES" :key="r.value" :label="r.label" :value="r.value" />
          </el-select>
        </div>

        <el-table :data="hosts" v-loading="hostLoading" stripe>
          <el-table-column prop="name" label="主机名" min-width="140">
            <template #default="{ row }">
              <div class="host-name-cell">
                <el-icon :class="statusIcon(row.status)"><Monitor /></el-icon>
                <span>{{ row.name }}</span>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="地址" min-width="160">
            <template #default="{ row }">{{ row.hostname }}:{{ row.port }}</template>
          </el-table-column>
          <el-table-column prop="username" label="用户" width="100" />
          <el-table-column prop="os" label="系统" width="80" />
          <el-table-column label="角色" width="90">
            <template #default="{ row }">
              <el-tag size="small" :type="roleTagType(row.role)" effect="plain">{{ hostRoleText(row.role) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="状态" width="100">
            <template #default="{ row }">
              <el-tag size="small" :type="statusType(row.status)">{{ statusText(row.status) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="tags" label="标签" min-width="120" show-overflow-tooltip />
          <el-table-column label="操作" width="180" fixed="right">
            <template #default="{ row }">
              <el-button link type="primary" @click="openViewHost(row)">详情</el-button>
              <el-button link type="primary" @click="openEditHost(row)">编辑</el-button>
              <el-popconfirm title="确认删除？" @confirm="handleDeleteHost(row)">
                <template #reference>
                  <el-button link type="danger">删除</el-button>
                </template>
              </el-popconfirm>
            </template>
          </el-table-column>
        </el-table>

        <div class="pagination-bar">
          <el-pagination
            v-model:current-page="page"
            v-model:page-size="pageSize"
            :total="total"
            :page-sizes="[10, 20, 50]"
            layout="total, prev, pager, next, sizes"
            @current-change="loadHosts"
            @size-change="loadHosts"
          />
        </div>
      </div>
    </div>

    <!-- 新建/编辑主机弹窗 -->
    <el-dialog v-model="hostDialogVisible" :title="editingHostId ? '编辑主机' : '新增主机'" width="560px">
      <el-form :model="hostForm" label-width="90px">
        <el-form-item label="主机名" required>
          <el-input v-model="hostForm.name" placeholder="显示名称，如 web-server-01" />
        </el-form-item>
        <el-form-item label="所属组">
          <el-select v-model="hostForm.groupId" placeholder="选择主机组" clearable style="width:100%">
            <el-option v-for="g in groups" :key="g.id" :label="g.name" :value="g.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="IP/域名" required>
          <el-input v-model="hostForm.hostname" placeholder="192.168.1.100 或 example.com" />
        </el-form-item>
        <el-form-item label="端口">
          <el-input-number v-model="hostForm.port" :min="1" :max="65535" style="width:100%" />
        </el-form-item>
        <el-form-item label="用户名" required>
          <el-input v-model="hostForm.username" placeholder="root / ubuntu / ..." />
        </el-form-item>
        <el-form-item label="认证方式">
          <el-radio-group v-model="hostForm.authType">
            <el-radio value="password">密码</el-radio>
            <el-radio value="key">私钥</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item v-if="hostForm.authType === 'password'" label="密码">
          <el-input v-model="hostForm.password" type="password" show-password placeholder="SSH 登录密码" />
        </el-form-item>
        <el-form-item v-if="hostForm.authType === 'key'" label="私钥">
          <el-input v-model="hostForm.privateKey" type="textarea" :rows="4" placeholder="-----BEGIN RSA PRIVATE KEY----- ..." />
        </el-form-item>
        <el-form-item v-if="hostForm.authType === 'key'" label="私钥口令">
          <el-input v-model="hostForm.passphrase" type="password" show-password placeholder="可选，私钥加密口令" />
        </el-form-item>
        <el-form-item label="操作系统">
          <el-select v-model="hostForm.os" style="width:100%">
            <el-option label="Linux" value="linux" />
            <el-option label="Windows" value="windows" />
          </el-select>
        </el-form-item>
        <el-form-item label="角色">
          <el-select v-model="hostForm.role" style="width:100%">
            <el-option v-for="r in HOST_ROLES" :key="r.value" :label="r.label" :value="r.value" />
          </el-select>
        </el-form-item>
        <el-form-item label="标签">
          <el-input v-model="hostForm.tags" placeholder="逗号分隔，如 prod,web,nginx" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="hostForm.description" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="hostDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="hostSaving" @click="handleSaveHost">保存</el-button>
      </template>
    </el-dialog>

    <!-- 主机组弹窗 -->
    <el-dialog v-model="groupDialogVisible" :title="editingGroupId ? '编辑主机组' : '新建主机组'" width="420px">
      <el-form :model="groupForm" label-width="80px">
        <el-form-item label="名称" required>
          <el-input v-model="groupForm.name" placeholder="如：生产环境 / 测试环境" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="groupForm.description" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="groupDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="groupSaving" @click="handleSaveGroup">保存</el-button>
      </template>
    </el-dialog>

    <!-- 主机详情抽屉 -->
    <el-drawer v-model="detailVisible" :title="currentHost?.name || '主机详情'" size="560px">
      <div v-if="currentHost" class="host-detail">
        <div class="detail-section">
          <div class="section-head-inline">
            <div class="section-title">基本信息</div>
            <el-button type="primary" size="small" :icon="Monitor" @click="openTerminal">打开终端</el-button>
            <el-button type="success" size="small" :icon="Platform" @click="openExec">执行命令</el-button>
          </div>
          <el-descriptions :column="1" border size="small">
            <el-descriptions-item label="主机名">{{ currentHost.name }}</el-descriptions-item>
            <el-descriptions-item label="地址">{{ currentHost.hostname }}:{{ currentHost.port }}</el-descriptions-item>
            <el-descriptions-item label="用户名">{{ currentHost.username }}</el-descriptions-item>
            <el-descriptions-item label="操作系统">{{ currentHost.os }}</el-descriptions-item>
            <el-descriptions-item label="角色">
              <el-tag size="small" :type="roleTagType(currentHost.role)" effect="plain">{{ hostRoleText(currentHost.role) }}</el-tag>
            </el-descriptions-item>
            <el-descriptions-item label="状态">
              <el-tag size="small" :type="statusType(currentHost.status)">{{ statusText(currentHost.status) }}</el-tag>
            </el-descriptions-item>
            <el-descriptions-item label="标签">{{ currentHost.tags || '-' }}</el-descriptions-item>
            <el-descriptions-item label="描述">{{ currentHost.description || '-' }}</el-descriptions-item>
          </el-descriptions>
        </div>
        <div class="detail-section">
          <div class="section-head-inline">
            <div class="section-title">最近执行记录</div>
            <el-button link type="primary" @click="loadCommandRecords">刷新</el-button>
          </div>
          <el-table :data="commandRecords" v-loading="cmdLoading" size="small" stripe>
            <el-table-column prop="command" label="命令" min-width="200" show-overflow-tooltip />
            <el-table-column label="结果" width="80" align="center">
              <template #default="{ row }">
                <el-icon :class="row.status === 'success' ? 'text-success' : 'text-danger'">
                  <component :is="row.status === 'success' ? 'CircleCheck' : 'CircleClose'" />
                </el-icon>
              </template>
            </el-table-column>
            <el-table-column prop="exitCode" label="退出码" width="70" align="center" />
            <el-table-column prop="durationMs" label="耗时(ms)" width="90" align="center" />
            <el-table-column prop="createdAt" label="时间" width="150">
              <template #default="{ row }">{{ formatTime(row.createdAt) }}</template>
            </el-table-column>
          </el-table>
        </div>
        <div class="detail-section">
          <div class="section-head-inline">
            <div class="section-title">操作审计</div>
            <el-button link type="primary" @click="loadAuditRecords">刷新</el-button>
          </div>
          <el-timeline v-loading="auditLoading">
            <el-timeline-item
              v-for="a in auditRecords"
              :key="a.id"
              :timestamp="formatTime(a.createdAt)"
              placement="top"
            >
              <div class="audit-item">
                <div class="audit-head">
                  <el-tag size="small" effect="plain">{{ auditActionText(a.action) }}</el-tag>
                  <span class="audit-op">{{ a.operatorName || '未知' }}</span>
                  <span class="audit-ip" v-if="a.clientIp">{{ a.clientIp }}</span>
                </div>
                <div class="audit-detail" v-if="a.detail">{{ a.detail }}</div>
              </div>
            </el-timeline-item>
            <el-empty v-if="!auditLoading && auditRecords.length === 0" description="暂无审计记录" :image-size="50" />
          </el-timeline>
        </div>
      </div>
    </el-drawer>

    <!-- SSH 终端 -->
    <el-dialog v-model="terminalVisible" title="SSH 终端" width="860px" top="5vh"
      :close-on-click-modal="false" @closed="terminalHostId = null">
      <div class="terminal-wrap">
        <HostTerminal v-if="terminalVisible && terminalHostId" :host-id="terminalHostId" />
      </div>
    </el-dialog>

    <!-- 流式命令执行（WebSocket，参考 1Shell host_exec） -->
    <el-dialog v-model="execVisible" title="命令执行（实时流式输出）" width="900px" top="5vh"
      :close-on-click-modal="false" @closed="execHostId = null">
      <div class="exec-wrap">
        <HostExecConsole v-if="execVisible && execHostId" :host-id="execHostId" />
      </div>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { Plus, MoreFilled, Monitor, Platform, CircleCheck, CircleClose } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import {
  listHostGroups, createHostGroup, updateHostGroup, deleteHostGroup,
  listHosts, createHost, updateHost, deleteHost, listHostCommands,
  listHostAudits, HOST_ROLES, hostRoleText
} from '@/api/host'
import HostTerminal from '@/components/host/HostTerminal.vue'
import HostExecConsole from '@/components/host/HostExecConsole.vue'

const groups = ref([])
const currentGroupId = ref(0)
const hosts = ref([])
const total = ref(0)
const totalCount = ref(0)
const page = ref(1)
const pageSize = ref(20)
const keyword = ref('')
const statusFilter = ref('')
const roleFilter = ref('')
const hostLoading = ref(false)

// 主机弹窗
const hostDialogVisible = ref(false)
const editingHostId = ref(null)
const hostSaving = ref(false)
const hostForm = reactive({
  name: '', hostname: '', port: 22, username: 'root',
  authType: 'password', password: '', privateKey: '', passphrase: '',
  os: 'linux', role: 'other', groupId: 0, tags: '', description: '',
})

// 终端弹窗
const terminalVisible = ref(false)
const terminalHostId = ref(null)

// 命令执行弹窗
const execVisible = ref(false)
const execHostId = ref(null)

// 主机组弹窗
const groupDialogVisible = ref(false)
const editingGroupId = ref(null)
const groupSaving = ref(false)
const groupForm = reactive({ name: '', description: '' })

// 详情抽屉
const detailVisible = ref(false)
const currentHost = ref(null)
const commandRecords = ref([])
const cmdLoading = ref(false)
const auditRecords = ref([])
const auditLoading = ref(false)

function roleTagType(role) {
  return { prod: 'danger', test: 'warning', dev: 'success', bastion: 'info', other: '' }[role] || ''
}

function openTerminal() {
  if (!currentHost.value) return
  terminalHostId.value = currentHost.value.id
  terminalVisible.value = true
}

function openExec() {
  if (!currentHost.value) return
  execHostId.value = currentHost.value.id
  execVisible.value = true
}

async function loadAuditRecords() {
  if (!currentHost.value) return
  auditLoading.value = true
  try {
    const res = await listHostAudits({ targetType: 'host', targetId: currentHost.value.id, limit: 50 })
    if (res.code === 0) auditRecords.value = res.data || []
  } finally { auditLoading.value = false }
}

function auditActionText(a) {
  return {
    host_group_create: '创建主机组',
    host_group_update: '更新主机组',
    host_group_delete: '删除主机组',
    host_create: '创建主机',
    host_update: '更新主机',
    host_delete: '删除主机',
    host_terminal_open: '打开终端',
    host_exec: '命令执行',
  }[a] || a
}

async function loadGroups() {
  // all=1：管理员可查看全部主机组（服务端仅对管理员生效，普通用户仍为本人隔离）
  const res = await listHostGroups({ all: 1 })
  if (res.code === 0) groups.value = res.data || []
}

function selectGroup(id) {
  currentGroupId.value = id
  page.value = 1
  loadHosts()
}

async function loadHosts() {
  hostLoading.value = true
  try {
    const params = {
      page: page.value, pageSize: pageSize.value,
      keyword: keyword.value, status: statusFilter.value,
    }
    if (currentGroupId.value > 0) params.groupId = currentGroupId.value
    if (roleFilter.value) params.role = roleFilter.value
    const res = await listHosts(params)
    if (res.code === 0) {
      hosts.value = res.data.list || []
      total.value = res.data.total || 0
      if (currentGroupId.value === 0) totalCount.value = res.data.total || 0
    }
  } finally { hostLoading.value = false }
}

function openCreateHost() {
  editingHostId.value = null
  Object.assign(hostForm, {
    name: '', hostname: '', port: 22, username: 'root',
    authType: 'password', password: '', privateKey: '', passphrase: '',
    os: 'linux', role: 'other', groupId: currentGroupId.value || 0, tags: '', description: '',
  })
  hostDialogVisible.value = true
}

function openEditHost(row) {
  editingHostId.value = row.id
  Object.assign(hostForm, {
    name: row.name, hostname: row.hostname, port: row.port, username: row.username,
    authType: row.authType || 'password', password: '', privateKey: '', passphrase: '',
    os: row.os || 'linux', role: row.role || 'other', groupId: row.groupId || 0, tags: row.tags || '', description: row.description || '',
  })
  hostDialogVisible.value = true
}

async function handleSaveHost() {
  if (!hostForm.name.trim() || !hostForm.hostname.trim() || !hostForm.username.trim()) {
    return ElMessage.warning('请填写必填项')
  }
  hostSaving.value = true
  try {
    const res = editingHostId.value
      ? await updateHost(editingHostId.value, hostForm)
      : await createHost(hostForm)
    if (res.code !== 0) {
      ElMessage.error(res.message || '保存失败')
      return
    }
    ElMessage.success('已保存')
    hostDialogVisible.value = false
    loadGroups()
    loadHosts()
  } finally { hostSaving.value = false }
}

async function handleDeleteHost(row) {
  const res = await deleteHost(row.id)
  if (res.code === 0) {
    ElMessage.success('已删除')
    loadGroups()
    loadHosts()
  } else {
    ElMessage.error(res.message || '删除失败')
  }
}

function openCreateGroup() {
  editingGroupId.value = null
  groupForm.name = ''
  groupForm.description = ''
  groupDialogVisible.value = true
}

function handleGroupCmd(cmd, g) {
  if (cmd === 'edit') {
    editingGroupId.value = g.id
    groupForm.name = g.name
    groupForm.description = g.description || ''
    groupDialogVisible.value = true
  } else if (cmd === 'delete') {
    ElMessage.warning('请先将主机移到其他组后再删除')
  }
}

async function handleSaveGroup() {
  if (!groupForm.name.trim()) return ElMessage.warning('请填写名称')
  groupSaving.value = true
  try {
    const res = editingGroupId.value
      ? await updateHostGroup(editingGroupId.value, groupForm)
      : await createHostGroup(groupForm)
    if (res.code !== 0) {
      ElMessage.error(res.message || '保存失败')
      return
    }
    ElMessage.success('已保存')
    groupDialogVisible.value = false
    loadGroups()
  } finally { groupSaving.value = false }
}

// 详情
async function openViewHost(row) {
  currentHost.value = row
  detailVisible.value = true
  loadCommandRecords()
  loadAuditRecords()
}

async function loadCommandRecords() {
  if (!currentHost.value) return
  cmdLoading.value = true
  try {
    const res = await listHostCommands(currentHost.value.id, 20)
    if (res.code === 0) commandRecords.value = res.data || []
  } finally { cmdLoading.value = false }
}

function statusText(s) {
  return { online: '在线', offline: '离线', pending: '待检测', failed: '失败' }[s] || s
}
function statusType(s) {
  return { online: 'success', offline: 'info', pending: 'warning', failed: 'danger' }[s] || 'info'
}
function statusIcon(s) {
  return { online: 'text-success', offline: 'text-muted', pending: 'text-warning', failed: 'text-danger' }[s] || ''
}
function formatTime(t) {
  if (!t) return '-'
  return new Date(t).toLocaleString('zh-CN', { hour12: false })
}

onMounted(async () => {
  await loadGroups()
  loadHosts()
})
</script>

<style scoped>
.page-container { padding: 20px; }
.page-header { display:flex; justify-content:space-between; align-items:flex-start; margin-bottom:16px; }
.page-title { font-size:20px; font-weight:600; color:var(--text); }
.page-subtitle { margin-top:4px; font-size:13px; color:var(--text-muted); }

.main-layout { display: flex; gap: 16px; align-items: flex-start; }

.group-panel {
  width: 240px; flex-shrink: 0;
  padding: 12px;
}
.panel-head {
  display: flex; justify-content: space-between; align-items: center;
  padding: 0 4px 10px; border-bottom: 1px solid var(--border);
  margin-bottom: 8px;
}
.panel-title { font-size: 14px; font-weight: 600; color: var(--text); }
.group-list { max-height: calc(100vh - 200px); overflow-y: auto; }
.group-item {
  display: flex; align-items: center; gap: 8px;
  padding: 8px 10px; border-radius: 6px; cursor: pointer;
  font-size: 13px; color: var(--text);
  position: relative;
}
.group-item:hover { background: var(--bg-soft); }
.group-item.active { background: var(--primary-light); color: var(--primary); font-weight: 500; }
.group-name { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.group-count { font-size: 12px; color: var(--text-muted); background: var(--bg-soft); padding: 1px 6px; border-radius: 10px; }
.group-item.active .group-count { background: rgba(79,110,247,0.15); color: var(--primary); }
.group-actions { opacity: 0; transition: opacity 0.2s; }
.group-item:hover .group-actions { opacity: 1; }
.more-btn { color: var(--text-muted); font-size: 14px; }

.host-panel { flex: 1; padding: 16px; min-width: 0; }
.filter-bar { display: flex; gap: 12px; margin-bottom: 16px; }
.pagination-bar { display: flex; justify-content: flex-end; margin-top: 16px; }

.host-name-cell { display: flex; align-items: center; gap: 8px; }
.host-name-cell .el-icon { font-size: 16px; }
.text-success { color: #67c23a; }
.text-danger { color: #f56c6c; }
.text-warning { color: #e6a23c; }
.text-muted { color: #909399; }

.detail-section { margin-bottom: 24px; }
.section-title { font-size: 14px; font-weight: 600; color: var(--text); margin-bottom: 12px; }
.section-head-inline { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; }

/* SSH 终端弹窗 */
.terminal-wrap { height: 70vh; }
.terminal-wrap :deep(.host-terminal) { height: 100%; }

/* 操作审计 */
.audit-item { font-size: 13px; color: var(--text); }
.audit-head { display: flex; align-items: center; gap: 8px; }
.audit-op { color: var(--text); font-weight: 500; }
.audit-ip { color: var(--text-muted); font-size: 12px; }
.audit-detail { margin-top: 4px; color: var(--text-secondary); }
</style>
