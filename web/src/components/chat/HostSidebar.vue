<template>
  <div class="host-sidebar" v-loading="loading">
    <!-- 顶部 Tab -->
    <div class="hs-tabs">
      <div
        v-for="t in tabs"
        :key="t.key"
        class="hs-tab"
        :class="{ active: activeTab === t.key }"
        @click="activeTab = t.key"
      >
        <el-icon><component :is="t.icon" /></el-icon>
        <span>{{ t.label }}</span>
      </div>
    </div>

    <!-- 终端 Tab -->
    <div v-show="activeTab === 'terminal'" class="hs-panel">
      <div class="hs-panel-head">
        <div class="term-host-info" v-if="wsConnected || connecting">
          <span class="term-status-dot" :class="connecting ? 'dot-pending' : 'dot-online'"></span>
          <span class="term-host-name">{{ currentTerminalHostName }}</span>
        </div>
        <el-select v-else v-model="terminalHostId" size="small" placeholder="选择主机" style="flex:1">
          <el-option v-for="h in allHosts" :key="h.id" :label="h.name" :value="h.id">
            <span class="opt-dot" :class="dotClass(h.status)"></span>
            {{ h.name }}
            <span class="opt-meta">{{ h.hostname }}</span>
          </el-option>
        </el-select>
        <div class="term-actions">
          <el-button v-if="!wsConnected && !connecting" type="primary" size="small" :disabled="!terminalHostId" @click="connectTerminal">
            <el-icon><Connection /></el-icon>
            连接
          </el-button>
          <template v-else>
            <el-button link size="small" @click="reconnectTerminal" title="重连">
              <el-icon><Refresh /></el-icon>
            </el-button>
            <el-button link size="small" type="danger" @click="disconnectTerminal" title="断开">
              <el-icon><Close /></el-icon>
            </el-button>
          </template>
        </div>
      </div>

      <div v-if="wsConnected || connecting" class="terminal-container" ref="terminalRef"></div>
      <div v-else class="terminal-empty">
        <div class="term-empty-icon">🖥️</div>
        <div class="term-empty-title">终端未连接</div>
        <div class="term-empty-desc">选择主机后点击「连接」打开 SSH 终端</div>
        <el-button type="primary" size="small" :disabled="!terminalHostId" @click="connectTerminal" style="margin-top: 12px">
          连接终端
        </el-button>
      </div>
    </div>

    <!-- 主机 Tab -->
    <div v-show="activeTab === 'hosts'" class="hs-panel">
      <div class="hs-panel-head">
        <span class="hs-title">主机列表</span>
        <div class="hs-head-actions">
          <el-button link size="small" title="刷新" @click="loadAllHosts">
            <el-icon><Refresh /></el-icon>
          </el-button>
          <el-button link size="small" @click="openCreateGroup" title="新建主机组">
            <el-icon><Folder /></el-icon>
            建组
          </el-button>
          <el-button link size="small" type="primary" @click="openCreateHost" title="添加主机">
            <el-icon><Plus /></el-icon>
            添加
          </el-button>
        </div>
      </div>
      <el-select v-model="filterGroupId" size="small" placeholder="全部主机组" clearable style="width:100%; margin-bottom:8px" @change="loadAllHosts">
        <el-option label="全部主机" :value="0" />
        <el-option v-for="g in groups" :key="g.id" :label="g.name" :value="g.id" />
      </el-select>
      <div class="host-list">
        <div
          v-for="host in allHosts"
          :key="host.id"
          class="host-item"
        >
          <span class="hi-dot" :class="dotClass(host.status)"></span>
          <div class="hi-info">
            <div class="hi-name">{{ host.name }}</div>
            <div class="hi-meta">{{ host.hostname }} · {{ host.username }}</div>
          </div>
          <el-button link size="small" type="primary" @click.stop="openHostTerminal(host)">
            <el-icon><Connection /></el-icon>
            连接
          </el-button>
        </div>
        <el-empty v-if="!loading && allHosts.length === 0" description="暂无主机" :image-size="50" />
      </div>
    </div>

    <!-- 文件 Tab -->
    <div v-show="activeTab === 'files'" class="hs-panel">
      <div class="hs-panel-head">
        <span class="hs-title">文件浏览</span>
        <el-select v-model="fileHostId" size="small" style="max-width: 140px" @change="onFileHostChange">
          <el-option v-for="h in allHosts" :key="h.id" :label="h.name" :value="h.id" />
        </el-select>
      </div>
      <div class="file-path-bar">
        <el-button link size="small" :disabled="currentPath === '/'" @click="goUpDir">
          <el-icon><ArrowUp /></el-icon>
        </el-button>
        <span class="file-path-text">{{ currentPath }}</span>
        <el-button link size="small" @click="loadFileList">
          <el-icon><Refresh /></el-icon>
        </el-button>
      </div>
      <div class="file-list" v-loading="fileLoading">
        <div
          v-for="item in fileList"
          :key="item.name"
          class="file-item"
          @click="onFileItemClick(item)"
        >
          <el-icon class="file-icon" :class="{ 'is-dir': item.isDir }">
            <component :is="item.isDir ? 'Folder' : 'Document'" />
          </el-icon>
          <span class="file-name">{{ item.name }}</span>
          <span v-if="!item.isDir" class="file-size">{{ formatFileSize(item.size) }}</span>
        </div>
        <el-empty v-if="!fileLoading && fileList.length === 0" description="空目录" :image-size="40" />
      </div>
    </div>

    <!-- 新建主机组：和添加主机一样，不跳出聊天页 -->
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

    <!-- 添加主机：不跳出聊天页就能补充主机资产 -->
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
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onBeforeUnmount, watch, nextTick } from 'vue'
import { Terminal } from 'xterm'
import { FitAddon } from 'xterm-addon-fit'
import 'xterm/css/xterm.css'
import {
  Monitor, Folder, Document, Refresh, ArrowUp, Connection, Close, Plus
} from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { listHostGroups, createHostGroup, listHosts, createHost, HOST_ROLES } from '@/api/host'

const props = defineProps({
  agentId: { type: Number, default: 0 },
  agent: { type: Object, default: null }
})

const tabs = [
  { key: 'terminal', label: '终端', icon: Monitor },
  { key: 'hosts', label: '主机', icon: Folder },
  { key: 'files', label: '文件', icon: Document },
]

const activeTab = ref('hosts')
const loading = ref(false)
const groups = ref([])
const allHosts = ref([])
const filterGroupId = ref(0)

// 终端
const terminalRef = ref(null)
const terminalHostId = ref(null)
const wsConnected = ref(false)
const connecting = ref(false)
let term = null
let fitAddon = null
let ws = null
let termResizeObserver = null

// 文件
const fileHostId = ref(null)
const currentPath = ref('/')
const fileList = ref([])
const fileLoading = ref(false)

const currentTerminalHostName = computed(() =>
  allHosts.value.find(h => h.id === terminalHostId.value)?.name || ''
)

function dotClass(s) {
  return {
    online: 'dot-online', offline: 'dot-offline',
    pending: 'dot-pending', failed: 'dot-failed'
  }[s] || 'dot-offline'
}

async function loadGroups() {
  try {
    const res = await listHostGroups()
    if (res.code === 0) groups.value = res.data || []
  } catch (e) { /* ignore */ }
}

async function loadAllHosts() {
  loading.value = true
  try {
    const params = { pageSize: 100 }
    if (filterGroupId.value > 0) params.groupId = filterGroupId.value
    const res = await listHosts(params)
    if (res.code === 0) {
      allHosts.value = res.data.list || []
      if (allHosts.value.length > 0) {
        if (!terminalHostId.value) terminalHostId.value = allHosts.value[0].id
        if (!fileHostId.value) fileHostId.value = allHosts.value[0].id
      }
    }
  } finally { loading.value = false }
}

// ---------- 主机组 ----------

const groupDialogVisible = ref(false)
const groupSaving = ref(false)
const groupForm = reactive({ name: '', description: '' })

function openCreateGroup() {
  groupForm.name = ''
  groupForm.description = ''
  groupDialogVisible.value = true
}

async function submitGroup() {
  if (!groupForm.name.trim()) {
    return ElMessage.warning('请填写主机组名称')
  }
  groupSaving.value = true
  try {
    const res = await createHostGroup({
      name: groupForm.name.trim(),
      description: groupForm.description,
    })
    if (res.code !== 0) {
      ElMessage.error(res.message || '保存失败')
      return
    }
    ElMessage.success('主机组已创建')
    groupDialogVisible.value = false
    await loadGroups()
    // 建完组就把筛选切到新组，紧接着「添加」主机时默认就落在这个组里
    const newId = res.data?.id
    if (newId) {
      filterGroupId.value = newId
      await loadAllHosts()
    }
  } finally { groupSaving.value = false }
}

// ---------- 添加主机 ----------

const hostDialogVisible = ref(false)
const hostSaving = ref(false)
const hostForm = reactive({
  name: '', hostname: '', port: 22, username: 'root',
  authType: 'password', password: '', privateKey: '', passphrase: '',
  os: 'linux', role: 'other', groupId: 0,
})

function openCreateHost() {
  Object.assign(hostForm, {
    name: '', hostname: '', port: 22, username: 'root',
    authType: 'password', password: '', privateKey: '', passphrase: '',
    os: 'linux', role: 'other', groupId: filterGroupId.value || 0,
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
    if (res.code !== 0) {
      ElMessage.error(res.message || '保存失败')
      return
    }
    ElMessage.success('主机已添加')
    hostDialogVisible.value = false
    await loadAllHosts()
  } finally { hostSaving.value = false }
}

// ---------- 终端 ----------

function initTerminal() {
  if (term || !terminalRef.value) return
  term = new Terminal({
    fontSize: 13,
    fontFamily: "'JetBrains Mono', 'Fira Code', 'Consolas', monospace",
    theme: {
      background: '#1e1e2e',
      foreground: '#cdd6f4',
      cursor: '#f5e0dc',
      black: '#45475a',
      red: '#f38ba8',
      green: '#a6e3a1',
      yellow: '#f9e2af',
      blue: '#89b4fa',
      magenta: '#cba6f7',
      cyan: '#94e2d5',
      white: '#bac2de',
      brightBlack: '#585b70',
      brightRed: '#f38ba8',
      brightGreen: '#a6e3a1',
      brightYellow: '#f9e2af',
      brightBlue: '#89b4fa',
      brightMagenta: '#cba6f7',
      brightCyan: '#94e2d5',
      brightWhite: '#a6adc8',
    },
    cursorBlink: true,
    scrollback: 2000,
    convertEol: true,
  })
  fitAddon = new FitAddon()
  term.loadAddon(fitAddon)
  term.open(terminalRef.value)
  fitAddon.fit()

  // 监听尺寸变化
  termResizeObserver = new ResizeObserver(() => {
    if (fitAddon && wsConnected.value) {
      try { fitAddon.fit() } catch (e) {}
      sendResize()
    }
  })
  termResizeObserver.observe(terminalRef.value)

  // 用户输入 -> WebSocket
  term.onData(data => {
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ type: 'input', data }))
    }
  })
}

function connectTerminal() {
  if (!terminalHostId.value || connecting.value) return
  connecting.value = true
  wsConnected.value = false

  nextTick(() => {
    initTerminal()
    term.writeln('\x1b[90m正在连接主机...\x1b[0m')

    const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
    // WebSocket 握手无法带 Authorization 头，token 走 query（后端 Auth 中间件支持）
    const token = localStorage.getItem('aiagent_token') || ''
    const wsUrl = `${protocol}//${location.host}/api/hosts/${terminalHostId.value}/terminal?token=${encodeURIComponent(token)}`
    ws = new WebSocket(wsUrl)

    ws.onopen = () => {
      wsConnected.value = true
      connecting.value = false
      term.writeln('\x1b[32m✓ 已连接\x1b[0m')
      fitAddon.fit()
      sendResize()
      term.focus()
    }

    ws.onmessage = (event) => {
      let data
      try { data = JSON.parse(event.data) } catch (e) { return }
      if (data.type === 'output') {
        term.write(data.data)
      } else if (data.type === 'error') {
        term.writeln(`\r\n\x1b[31m错误：${data.data}\x1b[0m`)
      } else if (data.type === 'close') {
        wsConnected.value = false
        term.writeln('\r\n\x1b[33m连接已关闭\x1b[0m')
      }
    }

    ws.onerror = () => {
      connecting.value = false
      wsConnected.value = false
      term.writeln('\r\n\x1b[31m✗ 连接失败\x1b[0m')
    }

    ws.onclose = () => {
      wsConnected.value = false
      connecting.value = false
    }
  })
}

function sendResize() {
  if (!ws || ws.readyState !== WebSocket.OPEN || !term) return
  ws.send(JSON.stringify({
    type: 'resize',
    size: { rows: term.rows, cols: term.cols }
  }))
}

function disconnectTerminal() {
  if (ws) {
    ws.close()
    ws = null
  }
  wsConnected.value = false
  connecting.value = false
  if (term) term.clear()
}

function reconnectTerminal() {
  disconnectTerminal()
  connectTerminal()
}

function onTerminalHostChange() {
  if (ws) {
    ws.close()
    ws = null
  }
  wsConnected.value = false
  if (term) term.clear()
}

function openHostTerminal(host) {
  terminalHostId.value = host.id
  activeTab.value = 'terminal'
  nextTick(() => connectTerminal())
}

function fullscreenTerminal() {
  ElMessage.info('全屏功能开发中')
}

// ---------- 文件 ----------

function onFileHostChange() {
  currentPath.value = '/'
  loadFileList()
}

async function loadFileList() {
  if (!fileHostId.value) return
  fileLoading.value = true
  try {
    // TODO: 调用后端列目录接口
    await new Promise(r => setTimeout(r, 300))
    if (currentPath.value === '/') {
      fileList.value = [
        { name: 'root', isDir: true, size: 0 },
        { name: 'home', isDir: true, size: 0 },
        { name: 'etc', isDir: true, size: 0 },
        { name: 'var', isDir: true, size: 0 },
        { name: 'tmp', isDir: true, size: 0 },
        { name: 'opt', isDir: true, size: 0 },
      ]
    } else {
      fileList.value = [
        { name: '..', isDir: true, size: 0 },
        { name: 'README.md', isDir: false, size: 2048 },
        { name: 'config.yaml', isDir: false, size: 1024 },
        { name: 'logs', isDir: true, size: 0 },
      ]
    }
  } finally { fileLoading.value = false }
}

function onFileItemClick(item) {
  if (item.isDir) {
    if (item.name === '..') {
      goUpDir()
    } else {
      const path = currentPath.value.endsWith('/')
        ? currentPath.value + item.name
        : currentPath.value + '/' + item.name
      currentPath.value = path
      loadFileList()
    }
  } else {
    ElMessage.info('文件预览功能开发中')
  }
}

function goUpDir() {
  if (currentPath.value === '/') return
  const parts = currentPath.value.split('/').filter(Boolean)
  parts.pop()
  currentPath.value = '/' + parts.join('/')
  if (currentPath.value === '/') currentPath.value = '/'
  loadFileList()
}

function formatFileSize(bytes) {
  if (!bytes) return '-'
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / 1024 / 1024).toFixed(1) + ' MB'
}

onMounted(async () => {
  await loadGroups()
  await loadAllHosts()
})

onBeforeUnmount(() => {
  if (ws) { ws.close(); ws = null }
  if (termResizeObserver) { termResizeObserver.disconnect(); termResizeObserver = null }
  if (term) { term.dispose(); term = null }
})

watch(() => props.agentId, () => {
  allHosts.value = []
  terminalHostId.value = null
  fileHostId.value = null
  loadGroups()
  loadAllHosts()
})
</script>

<style scoped>
.host-sidebar {
  width: 340px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  border-left: 1px solid var(--border);
  background: #fafbfc;
  min-height: 0;
}

/* Tab 顶栏 */
.hs-tabs {
  display: flex;
  border-bottom: 1px solid var(--border);
  background: #fff;
}
.hs-tab {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 2px;
  padding: 10px 0;
  font-size: 11px;
  color: var(--text-muted);
  cursor: pointer;
  transition: all 0.15s;
  border-bottom: 2px solid transparent;
}
.hs-tab:hover { color: var(--primary); }
.hs-tab.active {
  color: var(--primary);
  border-bottom-color: var(--primary);
  background: rgba(79,110,247,0.04);
}
.hs-tab .el-icon { font-size: 16px; }

/* 面板容器 */
.hs-panel {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-height: 0;
}
.hs-panel-head {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 10px 12px;
  border-bottom: 1px solid var(--border-light);
  background: #fff;
}
.hs-title { font-size: 13px; font-weight: 600; color: var(--text); flex: 1; }

/* 终端 */
.term-host-info {
  flex: 1;
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.term-status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}
.term-host-name {
  font-size: 13px;
  font-weight: 600;
  color: var(--text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.term-actions { display: flex; gap: 4px; flex-shrink: 0; }

.terminal-container {
  flex: 1;
  background: #1e1e2e;
  padding: 8px;
  min-height: 0;
  overflow: hidden;
}
.terminal-container :deep(.xterm) {
  height: 100%;
}

.terminal-empty {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 24px;
  text-align: center;
  background: var(--bg-soft);
}
.term-empty-icon { font-size: 40px; margin-bottom: 12px; }
.term-empty-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--text);
  margin-bottom: 6px;
}
.term-empty-desc {
  font-size: 12px;
  color: var(--text-muted);
}

/* 主机列表 */
.host-list {
  flex: 1;
  overflow-y: auto;
  padding: 8px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.host-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border-radius: 8px;
  transition: background 0.15s;
  background: #fff;
  border: 1px solid var(--border-light);
}
.host-item:hover {
  border-color: var(--primary-light);
  background: rgba(79,110,247,0.04);
}
.host-item:hover {
  border-color: var(--primary-light);
  background: rgba(79,110,247,0.04);
}
.hi-dot { width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0; }
.dot-online { background: #67c23a; box-shadow: 0 0 4px rgba(103,194,58,0.5); }
.dot-offline { background: #909399; }
.dot-pending { background: #e6a23c; }
.dot-failed { background: #f56c6c; }
.hi-info { flex: 1; min-width: 0; }
.hi-name { font-size: 13px; font-weight: 500; color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.hi-meta { font-size: 11px; color: var(--text-muted); margin-top: 2px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.opt-dot {
  display: inline-block;
  width: 6px; height: 6px;
  border-radius: 50%;
  margin-right: 6px;
  vertical-align: middle;
}
.opt-meta {
  float: right;
  font-size: 11px;
  color: var(--text-muted);
}

/* 文件 */
.file-path-bar {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 8px 12px;
  background: #fff;
  border-bottom: 1px solid var(--border-light);
}
.file-path-text {
  flex: 1;
  font-size: 12px;
  font-family: 'JetBrains Mono', 'Fira Code', monospace;
  color: var(--text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  background: var(--bg-soft);
  padding: 4px 8px;
  border-radius: 4px;
}
.file-list {
  flex: 1;
  overflow-y: auto;
  padding: 6px;
}
.file-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 8px;
  border-radius: 4px;
  cursor: pointer;
  font-size: 12px;
  transition: background 0.15s;
}
.file-item:hover { background: rgba(79,110,247,0.06); }
.file-icon { color: var(--text-muted); font-size: 14px; flex-shrink: 0; }
.file-icon.is-dir { color: #e6a23c; }
.file-name { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text); }
.file-size { font-size: 11px; color: var(--text-muted); flex-shrink: 0; }
</style>
