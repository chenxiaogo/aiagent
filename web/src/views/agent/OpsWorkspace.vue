<template>
  <div class="ops-workspace">
    <!-- 左：主机 / 主机组树 -->
    <OpsHostTree :selected="selected" @select="onSelect" />

    <!-- 右：会话 + 下方面板 -->
    <div class="ops-main">
      <header class="ops-header">
        <div class="header-left">
          <span class="agent-name">{{ agent?.avatar || '🖥️' }} {{ agent?.name || '运维助手' }}</span>
          <span class="header-divider">/</span>
          <span class="scope-name">{{ scopeTitle }}</span>
          <el-tag v-if="selected.type === 'host'" size="small" effect="plain" type="success">单主机 · 可开终端</el-tag>
          <el-tag v-else-if="selected.type === 'host_group'" size="small" effect="plain" type="warning">主机组 · 无终端</el-tag>
        </div>

        <div class="header-right">
          <el-button
            size="small"
            type="danger"
            plain
            :icon="Delete"
            :disabled="isNewSession"
            title="删除当前会话及其消息"
            @click="removeSession"
          >
            删除
          </el-button>
          <el-button size="small" :icon="Plus" :disabled="isNewSession" @click="newSession">
            {{ isNewSession ? '新会话待开始' : '新建会话' }}
          </el-button>
        </div>
      </header>

      <!-- 会话标签栏：已打开的会话横排，带 running 与关闭；关闭只是收起标签，不删会话 -->
      <nav class="session-tabs">
        <div
          v-for="s in openedSessions"
          :key="s.id"
          class="session-tab"
          :class="{ active: s.id === currentSessionId }"
          :title="s.id === 0 ? '新会话' : (s.title || '未命名会话')"
          @click="openTab(s.id)"
        >
          <el-icon v-if="isRunning(s.id)" class="is-loading tab-spin"><Loading /></el-icon>
          <span class="tab-title">{{ s.id === 0 ? '新会话' : (s.title || '未命名会话') }}</span>
          <el-icon class="tab-close" title="关闭标签（不会删除会话）" @click.stop="closeTab(s.id)">
            <Close />
          </el-icon>
        </div>

        <!-- 未打开的历史会话：点开成为新标签 -->
        <el-dropdown v-if="historySessions.length" @command="openTab">
          <button class="tab-history" title="历史会话">
            历史 {{ historySessions.length }}
            <el-icon><ArrowDown /></el-icon>
          </button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item v-for="s in historySessions" :key="s.id" :command="s.id">
                <span class="hist-title">{{ s.title || '未命名会话' }}</span>
                <span v-if="isRunning(s.id)" class="hist-running">生成中</span>
                <span v-else class="hist-time">{{ formatTime(s.updatedAt) }}</span>
              </el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </nav>

      <!-- 内容区：三个标签共用一块区域，全部 v-show 常驻，切回来时状态不丢 -->
      <div class="ops-content">
        <OpsChatPane
          v-show="mainTab === 'chat'"
          :agent-id="agentId"
          :agent="agent"
          :scope="selected"
          :session-id="currentSessionId"
          @session="onSessionCreated"
          @trace="trace = $event"
          @running-change="runningSessionIds = $event"
        />

        <div v-show="mainTab === 'terminal'" class="term-wrap">
          <!-- key 绑定主机：换主机时销毁旧终端，避免连着上一台 -->
          <HostTerminal v-if="canOpenTerminal" :key="selected.id" :host-id="selected.id" />
          <div v-else class="pane-empty">
            <div class="term-empty-icon">🖥️</div>
            <div class="term-empty-title">没有可连接的终端</div>
            <div class="term-empty-desc">
              当前选中的是主机组「{{ selected.name }}」，一个终端没法对多台机器。<br>
              在左侧选中单台主机即可打开 SSH 终端。
            </div>
          </div>
        </div>

        <div v-show="mainTab === 'files'" class="files-wrap">
          <OpsFileManager v-if="canOpenFiles" :key="selected.id" :host-id="selected.id" />
          <div v-else class="pane-empty">
            <div class="term-empty-icon">📁</div>
            <div class="term-empty-title">没有可浏览的文件</div>
            <div class="term-empty-desc">
              文件管理面向单台主机。在左侧选中一台主机，即可上传 / 下载 / 管理它的文件。
            </div>
          </div>
        </div>
      </div>

      <!-- 底部标签栏：会话 / 主机终端 / 文件 切换同一块内容区 -->
      <nav class="ops-tabs">
        <button class="ops-tab" :class="{ active: mainTab === 'chat' }" @click="mainTab = 'chat'">
          <el-icon><ChatDotRound /></el-icon>
          会话
          <span v-if="trace.length" class="tab-badge">{{ trace.length }}</span>
        </button>
        <button
          class="ops-tab"
          :class="{ active: mainTab === 'terminal', disabled: !canOpenTerminal }"
          :disabled="!canOpenTerminal"
          :title="terminalTitle"
          @click="canOpenTerminal && (mainTab = 'terminal')"
        >
          <el-icon><Monitor /></el-icon>
          主机
          <span v-if="canOpenTerminal" class="tab-host">{{ selected.name }}</span>
        </button>
        <button
          class="ops-tab"
          :class="{ active: mainTab === 'files', disabled: !canOpenFiles }"
          :disabled="!canOpenFiles"
          :title="filesTitle"
          @click="canOpenFiles && (mainTab = 'files')"
        >
          <el-icon><FolderOpened /></el-icon>
          文件
          <span v-if="canOpenFiles" class="tab-host">{{ selected.name }}</span>
        </button>
      </nav>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { Plus, Delete, Close, ArrowDown, ChatDotRound, Monitor, FolderOpened, Loading } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { listSessions, deleteSession } from '@/api/chat'
import OpsHostTree from '@/components/ops/OpsHostTree.vue'
import OpsChatPane from '@/components/ops/OpsChatPane.vue'
import OpsFileManager from '@/components/ops/OpsFileManager.vue'
import HostTerminal from '@/components/host/HostTerminal.vue'

const props = defineProps({
  agentId: { type: Number, required: true },
  agent: { type: Object, default: null },
})

// 当前操作对象：全部主机 / 某个主机组 / 某台主机
const selected = ref({ type: 'global', id: 0, name: '全部主机' })
const sessions = ref([])
const currentSessionId = ref(0)
const trace = ref([])
// 后台仍在生成的会话 ID（由聊天区上报）
const runningSessionIds = ref([])

function isRunning(id) {
  return runningSessionIds.value.includes(id)
}

// ---------- 会话标签栏 ----------
// 已打开的会话 ID（0 表示「新会话」标签）。关闭标签只是把它收起来，会话本身不删，
// 可以从右侧「历史」重新打开。
const openedIds = ref([])

const openedSessions = computed(() => {
  const list = []
  for (const id of openedIds.value) {
    if (id === 0) {
      list.push({ id: 0, title: '新会话' })
      continue
    }
    const hit = sessions.value.find(s => s.id === id)
    if (hit) list.push(hit)
  }
  return list
})

// 当前作用域下、尚未打开的会话
const historySessions = computed(() =>
  sessions.value.filter(s => !openedIds.value.includes(s.id))
)

function openTab(id) {
  if (!openedIds.value.includes(id)) openedIds.value = [...openedIds.value, id]
  onSwitchSession(id)
}

function closeTab(id) {
  let next = openedIds.value.filter(x => x !== id)
  // 全关完就留一个「新会话」标签，避免界面上没有可输入的地方
  if (!next.length) next = [0]
  openedIds.value = next
  if (currentSessionId.value === id) {
    onSwitchSession(next[next.length - 1])
  }
}

// 主区标签：会话 / 终端共用一块区域
const mainTab = ref('chat')
// 只有单台主机才有终端：主机组是多台机器，没有「一个终端」的概念
const canOpenTerminal = computed(() => selected.value.type === 'host' && selected.value.id > 0)
// 文件管理同样只面向单台主机
const canOpenFiles = computed(() => selected.value.type === 'host' && selected.value.id > 0)

// 当前是否处于「新建会话」（还没有会话 ID）
const isNewSession = computed(() => !currentSessionId.value)

const terminalTitle = computed(() =>
  canOpenTerminal.value ? `SSH 终端：${selected.value.name}` : '主机组没有 SSH 终端，请选中单台主机'
)
const filesTitle = computed(() =>
  canOpenFiles.value ? `文件管理：${selected.value.name}` : '文件管理面向单台主机，请先选中一台主机'
)

// 切到主机组 / 全局时自动退回会话标签，避免停在不可用的终端或文件页
watch([canOpenTerminal, canOpenFiles], ([termOK, fileOK]) => {
  if (!termOK && mainTab.value === 'terminal') mainTab.value = 'chat'
  if (!fileOK && mainTab.value === 'files') mainTab.value = 'chat'
})

const scopeTitle = computed(() => {
  if (selected.value.type === 'host') return selected.value.name || '主机'
  if (selected.value.type === 'host_group') return `主机组：${selected.value.name || ''}`
  return '全部主机'
})

function formatTime(t) {
  if (!t) return ''
  const d = new Date(t)
  const pad = n => String(n).padStart(2, '0')
  return `${d.getMonth() + 1}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

// 用户是否主动处于「新建会话」状态（会话 ID 为 0）。
// 必须显式记录：agentId 就绪、切主机等场景都会重拉会话列表，
// 若不加区分地把 currentSessionId 落到「最新一条」，用户刚点的「新建会话」会被吞掉。
const pendingNewSession = ref(false)

// 切换操作对象：拉它名下的会话，默认落在最新一条（没有则是新会话）
async function loadSessions() {
  if (!props.agentId) return
  try {
    const res = await listSessions(props.agentId, {
      scopeType: selected.value.type,
      scopeId: selected.value.id,
    })
    sessions.value = res.code === 0 ? (res.data || []) : []
    if (pendingNewSession.value) {
      // 用户正等着开新会话，保持 0，不要把他塞回旧会话
      return
    }
    // 当前会话已不在该对象名下（切了对象）时，落到该对象最新的一条
    if (sessions.value.length && !sessions.value.some(s => s.id === currentSessionId.value)) {
      currentSessionId.value = sessions.value[0].id
    } else if (!sessions.value.length) {
      currentSessionId.value = 0
    }
    // 让当前会话出现在标签栏上
    if (!openedIds.value.includes(currentSessionId.value)) {
      openedIds.value = [...openedIds.value, currentSessionId.value]
    }
  } catch {
    sessions.value = []
  }
}

function onSelect(node) {
  if (node.type === selected.value.type && node.id === selected.value.id) return
  selected.value = { ...node }
  trace.value = []
  // 换了操作对象就不再是「新建」状态，回到该对象自己的会话
  pendingNewSession.value = false
  // 标签栏随操作对象切换：旧作用域的会话不再属于这里
  openedIds.value = []
}

function onSwitchSession(id) {
  currentSessionId.value = id || 0
  // 显式选了某条会话（或选了「新建会话」选项）
  pendingNewSession.value = !id
  trace.value = []
}

function newSession() {
  if (isNewSession.value) return
  currentSessionId.value = 0
  pendingNewSession.value = true
  trace.value = []
  // 「新会话」也是一个标签，方便在标签之间来回切
  if (!openedIds.value.includes(0)) openedIds.value = [...openedIds.value, 0]
}

// 删除当前会话：删完落到「新建会话」状态，而不是自动跳到另一条历史会话
async function removeSession() {
  const id = currentSessionId.value
  if (!id) return
  const target = sessions.value.find(s => s.id === id)
  try {
    await ElMessageBox.confirm(
      `确定删除会话「${target?.title || '未命名会话'}」？该会话的消息会一并删除，且不可恢复。`,
      '删除会话',
      { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' }
    )
  } catch { return }

  const res = await deleteSession(id)
  if (res.code !== 0) {
    ElMessage.error(res.message || '删除失败')
    return
  }
  ElMessage.success('会话已删除')
  // 标签栏去掉被删的会话，并留一个「新会话」标签
  openedIds.value = openedIds.value.filter(x => x !== id)
  if (!openedIds.value.includes(0)) openedIds.value = [...openedIds.value, 0]
  currentSessionId.value = 0
  pendingNewSession.value = true
  trace.value = []
  await loadSessions()
}

// 首轮消息由后端建好会话后回填，这里把新会话并入列表并选中
async function onSessionCreated(id) {
  if (!id) return
  // 首次发消息后后端才给出会话 ID：把「新会话」标签换成真实会话
  openedIds.value = openedIds.value.map(x => (x === 0 ? id : x))
  if (!openedIds.value.includes(id)) openedIds.value = [...openedIds.value, id]
  pendingNewSession.value = false
  currentSessionId.value = id
  await loadSessions()
}

watch(() => [selected.value.type, selected.value.id], loadSessions, { immediate: true })
watch(() => props.agentId, loadSessions)
</script>

<style scoped>
.ops-workspace {
  display: flex;
  height: 100%;
  min-height: 0;
  background: var(--bg);
}

.ops-main {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
}

.ops-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 10px 20px;
  border-bottom: 1px solid var(--border);
  background: var(--card-bg);
  flex-shrink: 0;
}

.header-left { display: flex; align-items: center; gap: 8px; min-width: 0; }
.agent-name {
  font-size: 14px; font-weight: 600; color: var(--text);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.header-divider { color: var(--text-muted); }
.scope-name {
  font-size: 13px; color: var(--text-secondary);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}

.header-right { display: flex; align-items: center; gap: 8px; flex-shrink: 0; }
/* ---------- 会话标签栏 ---------- */
.session-tabs {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 12px;
  border-bottom: 1px solid var(--border);
  background: var(--card-bg);
  flex-shrink: 0;
  overflow-x: auto;
  scrollbar-width: thin;
}
.session-tabs::-webkit-scrollbar { height: 4px; }
.session-tabs::-webkit-scrollbar-thumb { background: var(--border); border-radius: 2px; }

.session-tab {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  max-width: 200px;
  height: 28px;
  padding: 0 8px 0 10px;
  border: 1px solid var(--border-light);
  border-radius: 6px;
  background: var(--bg-subtle);
  font-size: 12.5px;
  color: var(--text-secondary);
  cursor: pointer;
  flex-shrink: 0;
  transition: color var(--duration) var(--ease), border-color var(--duration) var(--ease), background var(--duration) var(--ease);
}
.session-tab:hover { border-color: var(--primary); color: var(--text); }
.session-tab.active {
  background: var(--primary-soft);
  border-color: var(--primary);
  color: var(--primary);
  font-weight: 600;
}

.tab-title {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.tab-spin { font-size: 13px; }
.tab-close {
  border-radius: 4px;
  font-size: 12px;
  padding: 2px;
  opacity: 0.6;
  flex-shrink: 0;
}
.tab-close:hover { opacity: 1; background: rgba(0, 0, 0, 0.08); color: var(--danger, #f56c6c); }

.tab-history {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 28px;
  padding: 0 10px;
  border: 1px dashed var(--border);
  border-radius: 6px;
  background: transparent;
  color: var(--text-muted);
  font-size: 12.5px;
  cursor: pointer;
  flex-shrink: 0;
}
.tab-history:hover { border-color: var(--primary); color: var(--primary); }

.hist-title { margin-right: 10px; }
.hist-time { color: var(--text-muted); font-size: 12px; }
.hist-running { color: var(--primary); font-size: 12px; font-weight: 500; }

/* ---- 主区标签：会话 / 终端 ---- */
/* 标签栏在底部（终端工具常见布局）：容器 4px + 标签内边距 16px = 20px，与聊天内容左对齐 */
.ops-tabs {
  display: flex;
  gap: 4px;
  padding: 6px 4px;
  border-top: 1px solid var(--border);
  background: var(--card-bg);
  flex-shrink: 0;
}
.ops-tab {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 32px;
  padding: 0 16px;
  border: none;
  border-top: 2px solid transparent;
  border-radius: 0 0 6px 6px;
  background: transparent;
  color: var(--text-secondary);
  font-size: 13px;
  cursor: pointer;
  transition: color var(--duration) var(--ease), border-color var(--duration) var(--ease), background var(--duration) var(--ease);
}
.ops-tab:hover:not(.disabled) { background: var(--bg-subtle); color: var(--text); }
.ops-tab.active {
  color: var(--primary);
  font-weight: 600;
  border-top-color: var(--primary);
  background: var(--primary-soft);
}
.ops-tab.disabled { opacity: 0.45; cursor: not-allowed; }

.tab-badge {
  min-width: 16px;
  height: 16px;
  padding: 0 4px;
  border-radius: 8px;
  background: var(--primary);
  color: #fff;
  font-size: 11px;
  line-height: 16px;
  text-align: center;
}
.tab-host {
  max-width: 130px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-muted);
  font-weight: 400;
}

/* ---- 内容区：会话与终端共用同一块 ---- */
.ops-content { flex: 1; min-height: 0; display: flex; }
.ops-content > * { flex: 1; min-width: 0; min-height: 0; }

.term-wrap {
  display: flex;
  padding: 10px;
  background: var(--bg);
}
.term-wrap :deep(.host-terminal) { flex: 1; min-width: 0; }
.term-wrap :deep(.term-box) { min-height: 0; height: 100%; }

.files-wrap { flex: 1; min-width: 0; min-height: 0; display: flex; }

.pane-empty,
.term-empty {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
  text-align: center;
  color: var(--text-muted);
}
.term-empty-icon { font-size: 34px; }
.term-empty-title { font-size: 14px; color: var(--text-secondary); }
.term-empty-desc { font-size: 13px; line-height: 1.8; }
</style>
