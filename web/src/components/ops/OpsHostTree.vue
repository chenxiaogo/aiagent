<template>
  <aside class="ops-tree">
    <div class="tree-head">
      <span class="tree-title">主机</span>
      <div class="tree-actions">
        <el-button link size="small" title="刷新" @click="reload">
          <el-icon><Refresh /></el-icon>
        </el-button>
        <el-button link size="small" title="新建主机组" @click="openCreateGroup">
          <el-icon><FolderAdd /></el-icon>
        </el-button>
        <el-button link size="small" type="primary" title="添加主机" @click="openCreateHost">
          <el-icon><Plus /></el-icon>
        </el-button>
      </div>
    </div>

    <div class="tree-search">
      <el-input v-model="keyword" size="small" placeholder="搜索主机 / 主机组" clearable />
    </div>

    <div class="tree-body" v-loading="loading">
      <!-- 不限定机器：整台 agent 视角的全局会话 -->
      <div
        class="tree-node tree-node--global"
        :class="{ active: selected.type === 'global' }"
        @click="selectGlobal"
      >
        <span class="node-icon">🌐</span>
        <span class="node-name">全部主机</span>
        <el-tag size="small" effect="plain" type="info">不限定</el-tag>
      </div>

      <div v-for="g in visibleGroups" :key="`g-${g.id}`" class="tree-group">
        <div
          class="tree-node tree-node--group"
          :class="{ active: selected.type === 'host_group' && selected.id === g.id }"
          @click="selectGroup(g)"
        >
          <span class="node-caret" @click.stop="toggle(g.id)">
            <el-icon>
              <CaretRight v-if="expanded[g.id] === false" />
              <CaretBottom v-else />
            </el-icon>
          </span>
          <span class="node-icon">📁</span>
          <span class="node-name">{{ g.name }}</span>
          <span class="node-count">{{ hostsOf(g.id).length }}</span>
        </div>

        <div v-show="expanded[g.id] !== false" class="tree-children">
          <div
            v-for="h in hostsOf(g.id)"
            :key="`h-${h.id}`"
            class="tree-node tree-node--host"
            :class="{ active: selected.type === 'host' && selected.id === h.id }"
            @click="selectHost(h)"
          >
            <span class="host-dot" :class="dotClass(h.status)" />
            <span class="node-name">{{ h.name }}</span>
            <span class="node-meta">{{ h.hostname }}</span>
          </div>
          <div v-if="hostsOf(g.id).length === 0" class="tree-empty">该组暂无主机</div>
        </div>
      </div>

      <!-- 未分组主机 -->
      <div v-if="ungroupedHosts.length" class="tree-group">
        <div class="tree-node tree-node--group is-static">
          <span class="node-icon">📦</span>
          <span class="node-name">未分组</span>
          <span class="node-count">{{ ungroupedHosts.length }}</span>
        </div>
        <div class="tree-children">
          <div
            v-for="h in ungroupedHosts"
            :key="`u-${h.id}`"
            class="tree-node tree-node--host"
            :class="{ active: selected.type === 'host' && selected.id === h.id }"
            @click="selectHost(h)"
          >
            <span class="host-dot" :class="dotClass(h.status)" />
            <span class="node-name">{{ h.name }}</span>
            <span class="node-meta">{{ h.hostname }}</span>
          </div>
        </div>
      </div>

      <el-empty v-if="!loading && groups.length === 0 && hosts.length === 0" description="暂无主机，点右上角 + 添加" :image-size="60" />
    </div>

    <!-- 新建主机组 -->
    <el-dialog v-model="groupDialogVisible" title="新建主机组" width="420px" append-to-body>
      <el-form :model="groupForm" label-width="72px">
        <el-form-item label="组名" required>
          <el-input v-model="groupForm.name" placeholder="如 生产环境 / 广州机房" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="groupForm.description" type="textarea" :rows="2" placeholder="可选" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="groupDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="groupSaving" @click="submitGroup">保存</el-button>
      </template>
    </el-dialog>

    <!-- 添加主机 -->
    <el-dialog v-model="hostDialogVisible" title="添加主机" width="520px" append-to-body>
      <el-form :model="hostForm" label-width="86px">
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
          <el-input v-model="hostForm.passphrase" type="password" show-password placeholder="可选" />
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
      </el-form>
      <template #footer>
        <el-button @click="hostDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="hostSaving" @click="submitHost">保存</el-button>
      </template>
    </el-dialog>
  </aside>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { Refresh, Plus, FolderAdd, CaretRight, CaretBottom } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { listHostGroups, createHostGroup, listHosts, createHost, HOST_ROLES } from '@/api/host'

const props = defineProps({
  selected: { type: Object, required: true }, // { type: 'global'|'host'|'host_group', id, name }
})
const emit = defineEmits(['select'])

const loading = ref(false)
const groups = ref([])
const hosts = ref([])
const keyword = ref('')
const expanded = ref({}) // groupId -> false 表示折叠

const groupDialogVisible = ref(false)
const groupSaving = ref(false)
const groupForm = reactive({ name: '', description: '' })

const hostDialogVisible = ref(false)
const hostSaving = ref(false)
const hostForm = reactive({
  name: '', hostname: '', port: 22, username: 'root',
  authType: 'password', password: '', privateKey: '', passphrase: '',
  os: 'linux', role: 'other', groupId: 0,
})

const kw = computed(() => keyword.value.trim().toLowerCase())

const visibleGroups = computed(() => {
  if (!kw.value) return groups.value
  return groups.value.filter(g =>
    g.name.toLowerCase().includes(kw.value) ||
    hostsOf(g.id).some(h => h.name.toLowerCase().includes(kw.value) || (h.hostname || '').toLowerCase().includes(kw.value))
  )
})

const ungroupedHosts = computed(() => filterHosts(hosts.value.filter(h => !h.groupId)))

function filterHosts(list) {
  if (!kw.value) return list
  return list.filter(h =>
    h.name.toLowerCase().includes(kw.value) || (h.hostname || '').toLowerCase().includes(kw.value)
  )
}

function hostsOf(groupId) {
  return filterHosts(hosts.value.filter(h => h.groupId === groupId))
}

function dotClass(s) {
  return {
    online: 'dot-online', offline: 'dot-offline',
    pending: 'dot-pending', failed: 'dot-failed'
  }[s] || 'dot-offline'
}

async function reload() {
  loading.value = true
  try {
    const [gRes, hRes] = await Promise.all([
      listHostGroups(),
      listHosts({ pageSize: 200 }),
    ])
    if (gRes.code === 0) groups.value = gRes.data || []
    if (hRes.code === 0) hosts.value = hRes.data.list || []
  } finally { loading.value = false }
}

function toggle(id) {
  expanded.value = { ...expanded.value, [id]: expanded.value[id] === false }
}

function selectGlobal() {
  emit('select', { type: 'global', id: 0, name: '全部主机' })
}
function selectGroup(g) {
  emit('select', { type: 'host_group', id: g.id, name: g.name })
}
function selectHost(h) {
  emit('select', { type: 'host', id: h.id, name: h.name, hostname: h.hostname })
}

function openCreateGroup() {
  groupForm.name = ''
  groupForm.description = ''
  groupDialogVisible.value = true
}

async function submitGroup() {
  if (!groupForm.name.trim()) return ElMessage.warning('请填写主机组名称')
  groupSaving.value = true
  try {
    const res = await createHostGroup({ name: groupForm.name.trim(), description: groupForm.description })
    if (res.code !== 0) return ElMessage.error(res.message || '保存失败')
    ElMessage.success('主机组已创建')
    groupDialogVisible.value = false
    await reload()
    if (res.data?.id) emit('select', { type: 'host_group', id: res.data.id, name: res.data.name })
  } finally { groupSaving.value = false }
}

function openCreateHost() {
  Object.assign(hostForm, {
    name: '', hostname: '', port: 22, username: 'root',
    authType: 'password', password: '', privateKey: '', passphrase: '',
    os: 'linux', role: 'other',
    groupId: props.selected.type === 'host_group' ? props.selected.id : 0,
  })
  hostDialogVisible.value = true
}

async function submitHost() {
  if (!hostForm.name.trim() || !hostForm.hostname.trim() || !hostForm.username.trim()) {
    return ElMessage.warning('主机名、IP/域名、用户名为必填项')
  }
  hostSaving.value = true
  try {
    const res = await createHost({ ...hostForm })
    if (res.code !== 0) return ElMessage.error(res.message || '保存失败')
    ElMessage.success('主机已添加')
    hostDialogVisible.value = false
    await reload()
    if (res.data?.id) emit('select', { type: 'host', id: res.data.id, name: res.data.name, hostname: res.data.hostname })
  } finally { hostSaving.value = false }
}

onMounted(reload)
defineExpose({ reload })
</script>

<style scoped>
.ops-tree {
  width: 248px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  border-right: 1px solid var(--border);
  background: var(--card-bg);
}

.tree-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 14px;
  border-bottom: 1px solid var(--border-light);
}
.tree-title { font-size: 14px; font-weight: 600; color: var(--text); }
.tree-actions { display: flex; gap: 2px; }

.tree-search { padding: 10px 12px; }

.tree-body { flex: 1; overflow: auto; padding: 0 8px 12px; }

.tree-node {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 7px 8px;
  border-radius: 6px;
  cursor: pointer;
  font-size: 13px;
  color: var(--text);
  transition: background var(--duration) var(--ease);
}
.tree-node:hover { background: var(--bg-subtle); }
.tree-node.active {
  background: var(--primary-soft);
  color: var(--primary);
  font-weight: 600;
}
.tree-node--global { margin-bottom: 6px; }
.tree-node--host { padding-left: 26px; font-size: 12.5px; }
.tree-node--group.is-static { cursor: default; }

.node-caret { display: flex; color: var(--text-muted); }
.node-icon { flex-shrink: 0; }
.node-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.node-meta {
  font-size: 11px;
  color: var(--text-muted);
  max-width: 84px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.node-count {
  font-size: 11px;
  color: var(--text-muted);
  background: var(--bg-subtle);
  border-radius: 8px;
  padding: 0 6px;
}

.tree-children { margin-bottom: 4px; }
.tree-empty { padding: 4px 8px 4px 26px; font-size: 12px; color: var(--text-muted); }

.host-dot { width: 7px; height: 7px; border-radius: 50%; flex-shrink: 0; }
.dot-online { background: #34c38f; }
.dot-offline { background: #c0c4cc; }
.dot-pending { background: #f1b44c; }
.dot-failed { background: #f46a6a; }
</style>
