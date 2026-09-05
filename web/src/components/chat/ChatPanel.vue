<template>
  <div class="chat-panel">
    <!-- 会话侧边栏 -->
    <aside class="session-sidebar">
      <div class="session-toolbar">
        <el-button size="small" type="primary" :icon="Plus" class="new-session-btn" @click="newSession">新对话</el-button>
      </div>
      <div class="session-list" v-loading="sessionsLoading">
        <div
          v-for="s in sessions"
          :key="s.id"
          class="session-item"
          :class="{ active: s.id === currentSessionId }"
          @click="openSession(s)"
        >
          <el-icon v-if="s.isPinned" class="pin-icon" title="已置顶"><Star filled /></el-icon>
          <div class="session-main">
            <div class="session-title">{{ s.title || '未命名会话' }}</div>
            <div class="session-meta">
              <span v-if="isRunning(s.id)" class="session-running">
                <el-icon class="is-loading"><Loading /></el-icon>
                生成中
              </span>
              <span v-else>{{ formatTime(s.updatedAt) }}</span>
            </div>
          </div>
          <div class="session-actions" @click.stop>
            <el-button
              size="small" :icon="s.isPinned ? Star : StarFilled" text
              :title="s.isPinned ? '取消置顶' : '置顶'"
              @click="togglePinSession(s)"
            />
            <el-button size="small" :icon="Delete" text type="danger" title="删除" @click="removeSession(s)" />
          </div>
        </div>
        <el-empty v-if="!sessionsLoading && !sessions.length" description="暂无会话" :image-size="60" />
      </div>
    </aside>

    <!-- 中间聊天主体（消息列表 + 输入区） -->
    <div class="chat-body">
    <!-- 消息列表 -->
    <div class="message-list" ref="messageListRef">
      <div v-if="messages.length === 0" class="welcome">
        <div class="welcome-icon">{{ welcomeIcon }}</div>
        <h2>{{ welcomeTitle }}</h2>
        <p>{{ welcomeDesc }}</p>
        <div v-if="presetQuestions.length" class="welcome-hints">
          <el-tag
            v-for="q in presetQuestions"
            :key="q.id"
            class="hint-tag"
            @click="sendPreset(q.question)"
          >{{ q.question }}</el-tag>
        </div>
      </div>

      <div v-for="(msg, idx) in messages" :key="idx" class="message" :class="msg.role">
        <div class="message-avatar">
          <el-avatar :size="32" v-if="msg.role === 'user'" style="background: var(--primary-gradient)">U</el-avatar>
          <span v-else class="ai-avatar">{{ welcomeIcon }}</span>
        </div>
        <div class="message-content">
          <div v-if="msg.error" class="message-text message-error">
            <span class="error-icon">⚠️</span>
            <span class="error-text">{{ msg.error }}</span>
          </div>
          <div v-else-if="!msg.parts || !msg.parts.html" class="message-text" v-html="renderMarkdown(msg.content)"></div>
          <!-- 「引导语 + HTML 文档」混合：引导语正常渲染，HTML 主体渲染成下载卡片 -->
          <template v-else>
            <div v-if="msg.parts.prefix" class="message-text" v-html="renderMarkdown(msg.parts.prefix)"></div>
            <div class="html-export-card">
              <el-icon class="html-export-icon"><Document /></el-icon>
              <div class="html-export-body">
                <div class="html-export-name">{{ htmlExportName(msg.parts.html) }}</div>
                <div v-if="isHtmlTruncated(msg.parts.html)" class="html-export-desc html-export-warn">
                  ⚠️ 内容可能被截断（未闭合），建议调大模型的 maxTokens 后重新生成
                </div>
                <div v-else class="html-export-desc">智能体生成了一个 HTML 页面，可下载后在浏览器中打开</div>
              </div>
              <div class="html-export-actions">
                <!-- 有后端消息 ID 时走导出接口：普通 HTTP 链接下载，不受 Blob 限制 -->
                <template v-if="msg.id">
                  <el-button size="small" :icon="Download" tag="a" :href="exportUrl(msg.id)" target="_blank">下载</el-button>
                  <el-button size="small" :icon="Link" @click="copyShareLink(msg)">复制链接</el-button>
                </template>
                <template v-else>
                  <el-button size="small" :icon="Download" @click="downloadHtml(msg.parts.html, htmlExportName(msg.parts.html))">下载</el-button>
                  <el-button size="small" :icon="CopyDocument" @click="copyHtml(msg.parts.html)">复制</el-button>
                </template>
                <el-button size="small" :icon="View" @click="previewMessage(msg)">预览</el-button>
              </div>
            </div>
            <div v-if="msg.parts.suffix" class="message-text" v-html="renderMarkdown(msg.parts.suffix)"></div>
          </template>
        </div>
      </div>

      <!-- 流式输出中 -->
      <div v-if="streaming" class="message assistant">
        <div class="message-avatar"><span class="ai-avatar">{{ welcomeIcon }}</span></div>
        <div class="message-content">
          <div v-if="agentMode" class="agent-trace">
            <div class="agent-status">
              <el-icon class="is-loading"><Loading /></el-icon>
              <span>正在分析中…</span>
            </div>
            <div v-for="(tc, i) in agentToolCalls" :key="i" class="agent-tool-item">
              <span class="agent-tool-icon">🔧</span>
              <span class="agent-tool-name">{{ toolLabel(tc.name) }}</span>
              <span class="agent-tool-brief">{{ brief(tc.output || tc.error) }}</span>
            </div>
          </div>
          <!-- 人工确认：Agent 要执行危险操作，等用户在聊天框拍板后才继续 -->
          <div v-if="pendingApproval" class="approval-card" :class="`risk-${pendingApproval.risk || 'medium'}`">
            <div class="ap-head">
              <span class="ap-icon">🛡️</span>
              <span class="ap-title">需要你确认后执行</span>
              <el-tag size="small" :type="pendingApproval.risk === 'high' ? 'danger' : 'warning'" effect="dark">
                {{ pendingApproval.risk === 'high' ? '高风险' : '需确认' }}
              </el-tag>
            </div>
            <div class="ap-reason">{{ pendingApproval.reason }}</div>
            <div class="ap-tool">
              工具：<b>{{ toolLabel(pendingApproval.toolName) }}</b>
              <span class="ap-tool-name">{{ pendingApproval.toolName }}</span>
            </div>
            <pre v-if="pendingApproval.summary" class="ap-cmd">{{ pendingApproval.summary }}</pre>
            <el-checkbox v-model="approvalRemember" size="small" class="ap-remember">
              本次会话内同类操作不再询问
            </el-checkbox>
            <div class="ap-actions">
              <el-button size="small" type="danger" plain :loading="approvalSubmitting" @click="handleApproval(false)">
                拒绝
              </el-button>
              <el-button size="small" type="primary" :loading="approvalSubmitting" @click="handleApproval(true)">
                允许执行
              </el-button>
            </div>
          </div>

          <div v-if="streamError" class="message-text message-error">
            <span class="error-icon">⚠️</span>
            <span class="error-text">{{ streamError }}</span>
          </div>
          <!-- 流式中一旦开始输出 HTML 文档，就换成占位卡片：
               否则半截 HTML 会被 v-html 当成真元素渲染，糊在聊天框里 -->
          <div v-else-if="isHtmlDocStarted(streamContent)" class="html-export-card html-export-pending">
            <el-icon class="html-export-icon"><Document /></el-icon>
            <div class="html-export-body">
              <div class="html-export-name">正在生成 HTML 页面</div>
              <div class="html-export-desc">
                <el-icon class="is-loading"><Loading /></el-icon>
                已输出 {{ streamContent.length }} 字符，完成后可直接下载
              </div>
            </div>
          </div>
          <div v-else class="message-text" v-html="renderMarkdown(streamContent)"></div>
          <span v-if="!agentMode" class="typing-indicator">●</span>
          <div v-if="!streamError" class="stream-actions">
            <el-button size="small" :icon="VideoPause" @click="handleStop">停止生成</el-button>
          </div>
        </div>
      </div>

      <div v-if="lastAgentState && !streaming" class="agent-summary">
        <el-tag v-if="lastRuntime" size="small" type="info">
          {{ lastRuntime === 'eino_v2' ? 'Eino ADK V2' : '兼容 Agent' }}
        </el-tag>
        <el-tag v-if="memoryEnabled" size="small" type="success">会话记忆已启用</el-tag>
        <el-tag v-if="interrupted" size="small" type="warning">已停止</el-tag>
        <span class="agent-summary-text">
          工具调用 {{ lastAgentState.toolCalls || 0 }} 次 · {{ agentStateText(lastAgentState.current) }}
        </span>
      </div>
    </div>

    <!-- 输入区 -->
    <div class="input-area">
      <div class="input-toolbar">
        <span class="mode-tip">🤖 Agent 模式</span>
        <!-- 可临时切换本次对话使用的模型，候选来自智能体「模型」页已启用的对话模型 -->
        <el-select
          v-if="chatModels.length"
          v-model="selectedModelId"
          size="small"
          class="model-select"
          placeholder="选择对话模型"
        >
          <el-option
            v-for="m in chatModels"
            :key="m.modelId"
            :label="m.modelName + (m.isPrimary ? '（主）' : '')"
            :value="m.modelId"
          />
        </el-select>
        <span v-else class="model-warn">未绑定对话模型，请到智能体「模型」页配置</span>

        <!-- 会话权限模式：控制 Agent 能否自动执行副作用操作 -->
        <div class="mode-picker">
          <span class="mode-picker-label">权限</span>
          <el-select v-model="approvalMode" size="small" class="mode-select">
            <el-option v-for="m in APPROVAL_MODES" :key="m.value" :label="m.label" :value="m.value">
              <span class="mode-opt-label">{{ m.label }}</span>
              <span class="mode-opt-desc">{{ m.desc }}</span>
            </el-option>
          </el-select>
        </div>
      </div>

      <!-- 完全权限：显式提示当前会话不再逐步确认 -->
      <div v-if="approvalMode === 'full_access'" class="full-access-tip">
        <el-icon><WarningFilled /></el-icon>
        <span>
          完全权限已开启：本次会话的高风险操作（重启、kill、写系统路径等）将自动执行，仅灾难性命令会被拦截。
        </span>
      </div>
      <div v-else-if="approvalMode === 'delegated'" class="delegated-tip">
        <el-icon><InfoFilled /></el-icon>
        <span>委托审批：常规操作自动执行，高风险操作仍会被拒绝并提示需要完全权限。</span>
      </div>
      <el-input
        v-model="inputText"
        :placeholder="placeholder"
        @keydown.enter="handleSend($event)"
        size="large"
        type="textarea"
        :rows="2"
        :autosize="{ minRows: 2, maxRows: 4 }"
      >
        <template #append>
          <el-button v-if="streaming" type="danger" :icon="Close" @click="handleStop">
            停止
          </el-button>
          <el-button v-else type="primary" :icon="Promotion" @click="sendAgentMessage" />
        </template>
      </el-input>
    </div>
    </div>

    <!-- 运维型 Agent：右侧主机控制面板 -->
    <HostSidebar
      v-if="isOpsAgent"
      :agent-id="agentId"
      :agent="agent"
      @send-message="handleSidebarSend"
    />
  </div>
</template>

<script setup>
import { ref, computed, nextTick, watch, onMounted, onBeforeUnmount } from 'vue'
import { wsChatUrl, listSessions, listMessages, deleteSession, togglePin, resolveApproval, getExportUrl } from '@/api/chat'
import { getPresetQuestions, getAgentModels } from '@/api/agent'
import { createStreamDeltaBuffer } from '@/utils/streaming'
import { ElMessage } from 'element-plus'
import { Plus, Delete, Star, StarFilled, Promotion, Close, VideoPause, WarningFilled, InfoFilled, Document, Download, View, CopyDocument, Link } from '@element-plus/icons-vue'
import { marked } from 'marked'
import HostSidebar from './HostSidebar.vue'

const props = defineProps({
  agentId: { type: Number, default: 0 },
  agent: { type: Object, default: null }
})

const messageListRef = ref(null)
const messages = ref([])
const inputText = ref('')
const streaming = ref(false)
const streamContent = ref('')
const streamSources = ref([])
const currentSessionId = ref(null)
// 平台已把通用对话收敛为智能体的一种形态，对话能力统一由 Agent 运行时提供
const mode = ref('agent')
const agentToolCalls = ref([])
const lastAgentState = ref(null)
const lastRuntime = ref('')
// 本轮助手回复落库后的消息 ID，用于走后端导出接口下载（不依赖 Blob）
const lastMessageId = ref(0)
const memoryEnabled = ref(false)
const interrupted = ref(false)
// 执行过程中的错误（模型不可用、额度冻结等），会渲染成错误气泡
const streamError = ref('')
// 当前 WebSocket 连接，用于发送 stop 指令
const wsRef = ref(null)
// 已与界面解绑、但仍在后台跑完并落库的连接（切换/新建会话时产生）
const backgroundConns = ref([])
// 正在后台生成的会话 ID：用于在会话列表里标 running。
// 单独存一份响应式数据——连接对象上的属性变化不会触发视图更新。
const runningSessionIds = ref([])

function markRunning(sessionId, on) {
  if (!sessionId) return
  const set = new Set(runningSessionIds.value)
  if (on) set.add(sessionId)
  else set.delete(sessionId)
  runningSessionIds.value = [...set]
}

function isRunning(sessionId) {
  return runningSessionIds.value.includes(sessionId)
}
const presetQuestions = ref([])

// 人工确认：Agent 请求执行危险操作时挂起等待，用户在此拍板后才继续
const pendingApproval = ref(null)
const approvalRemember = ref(false)
const approvalSubmitting = ref(false)

// 会话权限模式：决定副作用工具是逐步确认、委托自动执行，还是完全放行
// 三种模式都不会绕过红线规则（rm -rf /、mkfs 等灾难性命令一律拒绝）
const APPROVAL_MODES = [
  { value: 'manual', label: '人工审批', desc: '危险操作逐个确认后执行', type: 'success' },
  { value: 'delegated', label: '委托审批', desc: '常规操作自动执行，高风险操作仍拒绝', type: 'warning' },
  { value: 'full_access', label: '完全权限', desc: '高风险操作也自动执行，仅红线阻断', type: 'danger' },
]
const APPROVAL_MODE_KEY = 'aiagent_approval_mode'
const approvalMode = ref(localStorage.getItem(APPROVAL_MODE_KEY) || 'manual')
const currentModeMeta = computed(
  () => APPROVAL_MODES.find(m => m.value === approvalMode.value) || APPROVAL_MODES[0]
)

// 模式按会话生效，记住上次选择方便下次沿用；切回人工审批立即收紧
watch(approvalMode, (val) => {
  localStorage.setItem(APPROVAL_MODE_KEY, val)
})

// 该智能体已启用的对话模型（绑定关系在智能体「模型」页配置，此处只做本次对话的临时切换）
const chatModels = ref([])
const selectedModelId = ref(0)

// 检索范围由智能体「资源」页绑定决定，工作台不再临时选择知识库
async function loadAgentModels() {
  chatModels.value = []
  selectedModelId.value = 0
  if (!props.agentId) return
  try {
    const res = await getAgentModels(props.agentId)
    if (res.code !== 0) return
    const models = (res.data?.models || [])
      .filter(m => m.role === 'chat' && m.enabled !== false)
      .sort((a, b) => {
        if (a.isPrimary !== b.isPrimary) return a.isPrimary ? -1 : 1
        return (a.priority || 0) - (b.priority || 0)
      })
    chatModels.value = models
    // 默认选中主模型；没有主模型时选第一个
    selectedModelId.value = (models.find(m => m.isPrimary) || models[0])?.modelId || 0
  } catch (e) { /* 忽略 */ }
}

// 会话侧边栏
const sessions = ref([])
const sessionsLoading = ref(false)
const activeSessionId = ref(null)

const agentMode = computed(() => mode.value === 'agent')
const isOpsAgent = computed(() => props.agent?.category === 'ops')
const welcomeIcon = computed(() => props.agent?.avatar || '🤖')
const welcomeTitle = computed(() => props.agent?.name || 'AI 对话')
const welcomeDesc = computed(() => props.agent?.description || '用自然语言提问，我会基于已有资料回答')
const placeholder = computed(() => agentMode.value
  ? 'Agent 会自主检索后再回答… 例如：昨天前门有没有人取包裹？'
  : '输入消息，开始对话…')

// ---------- 会话列表 ----------
async function loadSessions() {
  sessionsLoading.value = true
  try {
    const res = await listSessions(props.agentId || 0)
    if (res.code === 0) {
      sessions.value = res.data || []
      // 若当前会话已不存在于列表中，清空选中
      if (activeSessionId.value && !sessions.value.find(s => s.id === activeSessionId.value)) {
        activeSessionId.value = null
      }
    }
  } catch (e) { /* 忽略 */ }
  finally {
    sessionsLoading.value = false
  }
}

// 静默刷新列表（发送消息后拿到新 sessionId 时调用，不打断输入）
async function refreshSessionsSilently() {
  try {
    const res = await listSessions(props.agentId || 0)
    if (res.code === 0) sessions.value = res.data || []
  } catch (e) { /* 忽略 */ }
}

// detachStreaming 与界面解绑但不打断生成。
// 切换/新建会话时用它：直接关连接会让后端按中断处理只留半截，
// 界面又把已生成内容清掉，切回来就什么都不剩了。
function detachStreaming() {
  const ws = wsRef.value
  if (!streaming.value || !ws) {
    resetStreamState()
    return
  }
  // 有待确认的操作：主动取消，避免后台连接挂到审批超时
  if (pendingApproval.value) {
    resolveApproval(pendingApproval.value.id, {
      approved: false,
      comment: '用户已切换会话，操作取消',
    }).catch(() => { /* 已超时则忽略 */ })
  }
  // 进度原地冻结在连接上，切回来原样恢复（含「正在分析 / 停止生成」状态）
  ws._detached = true
  ws._sessionId = currentSessionId.value || 0
  ws._streamContent = streamContent.value
  ws._streamError = streamError.value
  ws._toolCalls = agentToolCalls.value
  // 连整份消息一起冻结：里面有用户刚发出去、后端还没回的内容
  ws._messages = [...messages.value]
  backgroundConns.value.push(ws)
  // 在会话列表上标 running（会话已有 ID 时才能标，新会话等后端返回 ID 再标）
  markRunning(ws._sessionId, true)
  wsRef.value = null
  resetStreamState()
}

// reattach 切回某个会话时，把后台仍在跑的连接重新接管到界面上，
// 恢复消息、流式进度与「正在分析 / 停止生成」状态。
function reattach(sessionId) {
  if (!sessionId) return false
  const conn = backgroundConns.value.find(
    c => c._sessionId === sessionId && c.readyState === WebSocket.OPEN
  )
  if (!conn) return false

  conn._detached = false
  wsRef.value = conn
  backgroundConns.value.splice(backgroundConns.value.indexOf(conn), 1)
  // 已回到前台，不再是「后台生成中」
  markRunning(sessionId, false)

  streaming.value = true
  // 先恢复整份消息（含用户消息），再接上流式进度
  messages.value = conn._messages || []
  streamContent.value = conn._streamContent || ''
  streamError.value = conn._streamError || ''
  agentToolCalls.value = conn._toolCalls || []
  scrollToBottom()
  return true
}

function resetStreamState() {
  streaming.value = false
  streamContent.value = ''
  streamSources.value = []
  streamError.value = ''
  agentToolCalls.value = []
  pendingApproval.value = null
  approvalRemember.value = false
}

// cancelStreaming 真正中断：关闭所有连接。仅在组件卸载时使用。
function cancelStreaming() {
  if (wsRef.value) {
    try { wsRef.value.close() } catch { /* ignore */ }
    wsRef.value = null
  }
  backgroundConns.value.forEach(conn => {
    try { conn.close() } catch { /* ignore */ }
  })
  backgroundConns.value = []
  runningSessionIds.value = []
  resetStreamState()
}

// 打开历史会话：恢复消息
async function openSession(s) {
  // 切走前先把正在进行的生成移交后台，避免内容丢失
  detachStreaming()
  activeSessionId.value = s.id
  currentSessionId.value = s.id
  // 切回正在后台生成的会话：重新接管，进度与「停止生成」按钮原样恢复
  if (reattach(s.id)) return
  messages.value = []
  try {
    const res = await listMessages(s.id)
    if (res.code === 0) {
      messages.value = (res.data || []).map(m => ({
        role: m.role,
        content: m.content,
        sources: m.sources ? safeParseSources(m.sources) : undefined,
        id: m.id || 0, parts: splitHtmlDocument(m.content)
      }))
    }
  } catch (e) {
    ElMessage.error('加载历史消息失败')
  }
  scrollToBottom()
}

function safeParseSources(raw) {
  try {
    const v = typeof raw === 'string' ? JSON.parse(raw) : raw
    return Array.isArray(v) ? v : []
  } catch (e) { return [] }
}

// 新建对话：清空当前上下文，连同上一轮的执行摘要
function newSession() {
  // 正在生成的回答移交后台继续跑完，不打断也不丢
  detachStreaming()
  activeSessionId.value = null
  currentSessionId.value = null
  messages.value = []
  lastAgentState.value = null
  lastRuntime.value = ''
  memoryEnabled.value = false
  interrupted.value = false
  streamError.value = ''
}

async function removeSession(s) {
  try {
    const res = await deleteSession(s.id)
    if (res.code === 0) {
      if (activeSessionId.value === s.id) newSession()
      await loadSessions()
    } else {
      ElMessage.error(res.message || '删除失败')
    }
  } catch (e) {
    ElMessage.error('删除失败')
  }
}

async function togglePinSession(s) {
  try {
    const res = await togglePin(s.id)
    if (res.code === 0) {
      await loadSessions()
    } else {
      ElMessage.error(res.message || '操作失败')
    }
  } catch (e) {
    ElMessage.error('操作失败')
  }
}

function formatTime(t) {
  if (!t) return ''
  const d = new Date(t)
  if (isNaN(d.getTime())) return ''
  const now = new Date()
  const diff = (now - d) / 1000
  if (diff < 60) return '刚刚'
  if (diff < 3600) return Math.floor(diff / 60) + ' 分钟前'
  if (diff < 86400) return Math.floor(diff / 3600) + ' 小时前'
  if (diff < 86400 * 7) return Math.floor(diff / 86400) + ' 天前'
  const pad = n => String(n).padStart(2, '0')
  return `${d.getMonth() + 1}-${pad(d.getDate())}`
}

onMounted(async () => {
  await loadPreset()
  await loadSessions()
  await loadAgentModels()
})

// 离开页面时才真正断开：后台连接不留下悬挂的 WebSocket
onBeforeUnmount(cancelStreaming)

watch(() => props.agentId, () => {
  messages.value = []
  currentSessionId.value = null
  activeSessionId.value = null
  lastAgentState.value = null
  lastRuntime.value = ''
  memoryEnabled.value = false
  interrupted.value = false
  loadPreset()
  loadSessions()
  loadAgentModels()
})

async function loadPreset() {
  if (!props.agentId) return
  try {
    const res = await getPresetQuestions(props.agentId)
    presetQuestions.value = (res.data || []).filter(q => q.isActive !== false)
  } catch (e) { /* 无预设问题，忽略 */ }
}

function sendPreset(q) {
  inputText.value = q
  handleSend()
}

function handleSend(e) {
  if (e && e.shiftKey) return
  e && e.preventDefault()
  // 执行中点击等于停止
  if (streaming.value) handleStop()
  else sendAgentMessage()
}

// 点停止后等待后端确认的兜底计时器：连接拥塞时后端可能发不出 done，
// 超时后在本地收尾，保证「停止」一定生效，不会一直转圈。
let stopTimer = null

// 停止当前 Agent 执行：复用同一条 WebSocket 发 stop 指令
function handleStop() {
  const ws = wsRef.value
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({ type: 'stop' }))
  }
  // 兜底：后端若因写阻塞迟迟发不出 done，5 秒后本地收尾
  if (stopTimer) clearTimeout(stopTimer)
  stopTimer = setTimeout(() => {
    stopTimer = null
    if (streaming.value) finishStream()
  }, 5000)
}

// 用户在聊天框对「危险操作」拍板：允许则 Agent 继续执行，拒绝则把拒绝结果交回模型
async function handleApproval(approved) {
  const pending = pendingApproval.value
  if (!pending || approvalSubmitting.value) return
  approvalSubmitting.value = true
  try {
    await resolveApproval(pending.id, {
      approved,
      remember: approved && approvalRemember.value,
    })
    pendingApproval.value = null
    approvalRemember.value = false
  } catch (e) {
    ElMessage.error('提交确认结果失败，请重试')
  } finally {
    approvalSubmitting.value = false
  }
}

// Agent 执行：WebSocket 流式通道，推送 thinking / tool / text / done，支持中途停止
function sendAgentMessage() {
  const text = inputText.value.trim()
  if (!text || streaming.value) return
  inputText.value = ''

  // 本轮之前的最大消息 ID：轮询恢复时据此从后端列表里定位「本轮」新生成的回复。
  // 用 ID 而不是下标——本地可能存在未落库的错误气泡，按下标切会错位。
  const baselineMaxId = messages.value.reduce((max, m) => Math.max(max, m.id || 0), 0)
  messages.value.push({ role: 'user', content: text })
  scrollToBottom()

  streaming.value = true
  streamContent.value = ''
  streamSources.value = []
  agentToolCalls.value = []
  lastAgentState.value = null
  lastRuntime.value = ''
  memoryEnabled.value = false
  interrupted.value = false
  streamError.value = ''
  pendingApproval.value = null
  approvalRemember.value = false

  const ws = new WebSocket(wsChatUrl())
  wsRef.value = ws
  let opened = false
  // 无活动看门狗 + 轮询续期：后端 WS 写超时 / 前端掉线时可能长时间收不到推送。
  // 这里刻意不关闭连接——关闭会让后端中断还没跑完的生成，回复只剩半截。
  // 改为超过阈值后主动轮询后端已落库的消息，拿到本轮新回复就更新界面并收尾。
  const STALE_TIMEOUT_MS = 45 * 1000
  const POLL_INTERVAL_MS = 8000
  const MAX_POLL_TIMES = 15
  let pollTimes = 0
  let lastActivityAt = Date.now()

  const deltaBuffer = createStreamDeltaBuffer((delta) => {
    streamContent.value += delta
    scrollToBottom()
  })

  // 看门狗挂在 ws 上，onclose / 卸载时统一清理
  ws._watchdog = setInterval(() => {
    if (ws._doneReceived || ws._detached) return
    if (Date.now() - lastActivityAt <= STALE_TIMEOUT_MS) return
    if (pollTimes >= MAX_POLL_TIMES) {
      // 轮询次数用尽仍未拿到结果：收尾并提示，避免界面永久转圈
      if (!ws._pollGaveUp) {
        ws._pollGaveUp = true
        ElMessage.warning('生成耗时较长，结果仍在后台处理，可稍后刷新页面查看')
        finishStream()
      }
      return
    }
    pollTimes++
    const sid = currentSessionId.value || ws._sessionId
    if (!sid) return
    listMessages(sid).then(res => {
      if (res.code !== 0 || ws._doneReceived || ws._detached) return
      // 本轮新落库的 assistant 回复：后端已入库即代表这一轮生成完毕
      const list = res.data || []
      const reply = list.find(m => m.role === 'assistant' && (m.id || 0) > baselineMaxId)
      if (!reply || !reply.content) return
      messages.value = list.map(m => ({
        role: m.role,
        content: m.content,
        sources: m.sources ? safeParseSources(m.sources) : undefined,
        id: m.id || 0, parts: splitHtmlDocument(m.content)
      }))
      ws._doneReceived = true
      scrollToBottom()
      // 已用后端权威结果覆盖，不再把本地残缺流内容补进会话
      finalizeStream()
    }).catch(() => { /* 网络异常，下一轮继续轮询 */ })
  }, POLL_INTERVAL_MS)

  ws.onopen = () => {
    opened = true
    lastActivityAt = Date.now()
    ws.send(JSON.stringify({
      type: 'agent',
      sessionId: currentSessionId.value || 0,
      agentId: props.agentId || 0,
      // 检索范围由智能体绑定的资源决定，不在此处临时指定
      knowledgeId: 0,
      // 下拉里选的对话模型，0 表示按智能体默认（主模型→回退链→全局）
      modelId: selectedModelId.value || 0,
      // 本次会话的权限模式，决定副作用工具是否需要逐步确认
      approvalMode: approvalMode.value || 'manual',
      message: text
    }))
  }

  ws.onmessage = (event) => {
    lastActivityAt = Date.now()
    let data
    try {
      data = JSON.parse(event.data)
    } catch (e) { return }

    // 已与界面解绑（切换/新建会话）：进度累积在连接上，不更新界面，
    // 否则旧会话的审批卡、流式内容会跑到新会话里。切回来时 reattach 原样恢复。
    if (ws._detached) {
      switch (data.type) {
        case 'session':
          // 后台连接也要记住新会话 ID，否则切回该会话时 reattach 找不到它
          if (data.sessionId) ws._sessionId = data.sessionId
          break
        case 'text':
          ws._streamContent = (ws._streamContent || '') + (data.content || '')
          break
        case 'error':
          ws._streamError = data.error || 'Agent 执行出错'
          break
        case 'tool':
          ws._toolCalls = [...(ws._toolCalls || []), { name: data.name, output: '执行中...' }]
          break
        case 'tool_result': {
          const list = [...(ws._toolCalls || [])]
          for (let i = list.length - 1; i >= 0; i--) {
            if (!data.name || list[i].name === data.name) {
              list[i] = { name: list[i].name, output: data.output || '', error: data.error || '' }
              break
            }
          }
          ws._toolCalls = list
          break
        }
        case 'done':
          // done 即代表生成结束：把 running 标记清掉，不能反过来标成运行中，
          // 否则会出现「回答已经出来了，会话上还挂着生成中」的诡异状态。
          if (data.sessionId) {
            markRunning(ws._sessionId, false)
            ws._sessionId = data.sessionId
          }
          markRunning(ws._sessionId, false)
          if (Array.isArray(data.toolCalls) && data.toolCalls.length) ws._toolCalls = data.toolCalls
          // 必须主动关：detached 分支会 return，走不到正常分支里的 ws.close()。
          // 不关的话连接一直挂着，会话列表上的「生成中」永远消不掉。
          try { ws.close() } catch { /* ignore */ }
          break
      }
      return
    }

    switch (data.type) {
      case 'session':
        // 新会话已在后端落库：立刻写回会话 ID 并刷新左侧列表，
        // 让会话条目马上出现，不必等到生成结束的 done 事件。
        if (data.sessionId && !currentSessionId.value) {
          currentSessionId.value = data.sessionId
          activeSessionId.value = data.sessionId
          ws._sessionId = data.sessionId
          refreshSessionsSilently()
        }
        break
      case 'tool':
        agentToolCalls.value.push({ name: data.name, output: '执行中...' })
        break
      case 'tool_result':
        updateLastToolCall(data.name, data.output, data.error)
        break
      case 'text':
        deltaBuffer.push(data.content || '')
        break
      case 'approval_request':
        // Agent 要执行副作用操作，挂起等待用户在此确认
        pendingApproval.value = {
          id: data.id,
          toolName: data.toolName,
          summary: data.summary,
          detail: data.detail,
          risk: data.risk,
          reason: data.reason || '该操作会改变外部状态，需要你确认后执行',
        }
        scrollToBottom()
        break
      case 'error':
        // 错误直接进聊天框（带错误样式），不再只弹一个提示
        streamError.value = data.error || 'Agent 执行出错'
        break
      case 'done':
        ws._doneReceived = true
        deltaBuffer.flushNow()
        if (Array.isArray(data.toolCalls) && data.toolCalls.length) {
          agentToolCalls.value = data.toolCalls
        }
        lastAgentState.value = {
          current: data.interrupted ? 'interrupted' : 'finalize',
          toolCalls: (data.toolCalls || []).length
        }
        lastRuntime.value = data.runtime || props.agent?.runtimeType || ''
        lastMessageId.value = data.messageId || 0
        memoryEnabled.value = data.memoryEnabled === true
        interrupted.value = data.interrupted === true
        if (data.sessionId) {
          currentSessionId.value = data.sessionId
          activeSessionId.value = data.sessionId
          refreshSessionsSilently()
        }
        ws.close()
        break
    }
  }

  ws.onerror = () => {
    if (!opened && !ws._detached) ElMessage.error('连接失败，请检查后端服务是否可用')
  }

  ws.onclose = () => {
    clearInterval(ws._watchdog)
    const idx = backgroundConns.value.indexOf(ws)
    if (idx >= 0) backgroundConns.value.splice(idx, 1)
    // 后台跑完的连接：如果它属于当前正查看的会话，刷新一次历史拿到完整结果
    if (ws._detached) {
      markRunning(ws._sessionId, false)
      if (ws._sessionId && ws._sessionId === currentSessionId.value) {
        const sid = ws._sessionId
        listMessages(sid).then(res => {
          if (res.code === 0 && currentSessionId.value === sid) {
            messages.value = (res.data || []).map(m => ({
              role: m.role,
              content: m.content,
              sources: m.sources ? safeParseSources(m.sources) : undefined,
              id: m.id || 0, parts: splitHtmlDocument(m.content)
            }))
            scrollToBottom()
          }
        }).catch(() => {})
      }
      return
    }
    if (wsRef.value === ws) wsRef.value = null
    // 正常收到 done：内容已在 done 分支 flush 并准备好，直接收尾
    if (ws._doneReceived) {
      finishStream()
      return
    }
    // 异常关闭（没收到 done）：先把缓冲尾段 flush，再优先用后端已落库的完整结果
    // 覆盖界面；拉取失败或尚无会话 ID 则用本地流内容兜底。避免 UI 永久卡在「生成中」，
    // 或残留一段没收到 done 的残缺回复。
    deltaBuffer.flushNow()
    if (currentSessionId.value) {
      const sid = currentSessionId.value
      listMessages(sid).then(res => {
        if (res.code === 0 && currentSessionId.value === sid) {
          messages.value = (res.data || []).map(m => ({
            role: m.role,
            content: m.content,
            sources: m.sources ? safeParseSources(m.sources) : undefined,
            id: m.id || 0, parts: splitHtmlDocument(m.content)
          }))
          scrollToBottom()
        }
        finalizeStream()
      }).catch(() => finishStream())
    } else {
      finishStream()
    }
  }
}

// 用工具结果回填最后一条处于「执行中」的工具调用
function updateLastToolCall(name, output, error) {
  const list = agentToolCalls.value
  for (let i = list.length - 1; i >= 0; i--) {
    if (!name || list[i].name === name) {
      list[i] = { name: list[i].name, output: output || '', error: error || '' }
      return
    }
  }
  agentToolCalls.value.push({ name: name || 'tool', output: output || '', error: error || '' })
}

function finishStream() {
  if (stopTimer) { clearTimeout(stopTimer); stopTimer = null }
  streaming.value = false
  const errText = streamError.value
  if (streamContent.value) {
    messages.value.push({
      role: 'assistant',
      content: streamContent.value,
      sources: streamSources.value,
      // 预计算拆分结果：HTML 文档渲染成下载卡片，引导语按 markdown 显示
      parts: splitHtmlDocument(streamContent.value)
    })
  }
  // 一条内容都没生成时出错，把错误单独留在会话里，方便回溯
  if (errText) {
    messages.value.push({ role: 'assistant', content: '', error: errText })
  }
  streamContent.value = ''
  streamSources.value = []
  streamError.value = ''
  pendingApproval.value = null
  scrollToBottom()
}

// 仅收尾、不把本地残缺流内容落进会话：异常关闭且已从后端拉到权威结果时调用
function finalizeStream() {
  if (stopTimer) { clearTimeout(stopTimer); stopTimer = null }
  streaming.value = false
  streamContent.value = ''
  streamSources.value = []
  streamError.value = ''
  pendingApproval.value = null
  scrollToBottom()
}

// 从主机侧边栏发送消息（快速命令等）
function handleSidebarSend(text) {
  if (!text) return
  inputText.value = text
  sendAgentMessage()
}

function toolLabel(name) {
  const m = {
    doc_search: '授权知识库检索',
    search_camera: '摄像头事件检索',
    search_videos: '视频片段检索',
    get_time: '获取时间',
    generate_report: '生成报告',
    list_hosts: '列出主机',
    exec_command: '执行命令',
    list_dir: '列出目录',
    read_file: '读取文件',
    write_file: '写入文件',
  }
  return m[name] || name
}

function brief(text) {
  if (!text) return ''
  const first = String(text).split('\n').find(l => l.trim()) || ''
  return first.length > 60 ? first.slice(0, 60) + '…' : first
}

function agentStateText(current) {
  const m = { finalize: '已完成', blocked: '受预算限制中止', error: '出错', observe: '观察中', act: '执行中' }
  return m[current] || current || '未知'
}

function scrollToBottom() {
  nextTick(() => {
    if (messageListRef.value) {
      messageListRef.value.scrollTop = messageListRef.value.scrollHeight
    }
  })
}

// 超长文本直接交给 marked 解析会长时间占用主线程：浏览器随之停止消费 WebSocket 数据，
// TCP 窗口关闭后后端写入就会超时（表现为前端一直卡在「生成中」）。
// 超过阈值先降级为纯文本，生成结束后（一次性渲染）再还原完整排版。
const MAX_MARKDOWN_RENDER_LEN = 30000

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]))
}

function renderMarkdown(text) {
  if (!text) return ''
  if (text.length > MAX_MARKDOWN_RENDER_LEN) {
    return '<pre style="white-space:pre-wrap;word-break:break-word">' + escapeHtml(text) + '</pre>'
  }
  try { return marked.parse(text) } catch { return escapeHtml(text) }
}

// 提取智能体返回的 HTML 文档内容：支持直接输出，或被 ```html 代码块包裹，
// 非 HTML 返回空串。识别到 HTML 文档时聊天框渲染下载卡片，
// 避免把整篇源码显示出来，也规避直接 v-html 渲染的 XSS 风险。
function extractHtmlDocument(text) {
  const t = (text || '').trim()
  if (/^<!DOCTYPE\s+html/i.test(t) || (/<html[\s>]/i.test(t) && /<\/html>\s*$/i.test(t))) {
    return t
  }
  const fence = t.match(/^```(?:html)?\s*([\s\S]*?)```\s*$/i)
  if (fence) {
    const inner = fence[1].trim()
    if (/^<!DOCTYPE\s+html/i.test(inner) || /<html[\s>]/i.test(inner)) {
      return inner
    }
  }
  return ''
}

// 是否为完整 HTML 文档（用于决定是否渲染下载卡片而非 markdown）
function isHtmlDocument(text) {
  return extractHtmlDocument(text) !== ''
}

// 把「引导语 + HTML 文档」拆成三段：prefix 引导语、html 文档主体、suffix 尾部文本。
// 没有检测到 HTML 文档时 html 为空，整体按 markdown 渲染。
// 模型常先说一段「已收集信息，为您生成…」再输出 <!DOCTYPE html>，
// 只判断「整篇是否 HTML」会把这种最常见的形式漏掉，导致源码被显示在聊天框里。
function splitHtmlDocument(text) {
  const t = text || ''
  if (!t) return { prefix: '', html: '', suffix: '' }

  // 代码块包裹：```html ... ```（流式未闭合时匹配到末尾）
  const fence = t.match(/```(?:html)?\s*[\s\S]*?(?:```|$)/i)
  if (fence) {
    const inner = fence[0].replace(/^```(?:html)?\s*/i, '').replace(/```\s*$/i, '').trim()
    if (/^<!DOCTYPE\s+html/i.test(inner) || /^<html[\s>]/i.test(inner)) {
      const start = t.indexOf(fence[0])
      return { prefix: t.slice(0, start), html: inner, suffix: t.slice(start + fence[0].length) }
    }
  }

  // 裸 HTML：从 <!DOCTYPE html> 或 <html> 开始，到 </html> 结束；未闭合则取到末尾
  const m = t.match(/<!DOCTYPE\s+html[\s\S]*$/i) || t.match(/<html[\s>][\s\S]*$/i)
  if (!m) {
    // 兜底：模型有时直接从 <div> / <section> 片段开始吐页面，缺 <!DOCTYPE> 与 <html>。
    // 此时按 HTML 标签密度判定，从第一个标签处切分，避免整篇源码糊在聊天框里。
    const tags = t.match(/<[a-zA-Z!\/][^>]*>/g) || []
    if (tags.length < 6 || !/<\/div>|<\/html>|<style|<body|<section/i.test(t)) {
      return { prefix: t, html: '', suffix: '' }
    }
    return cutHtmlFrom(t, t.indexOf(tags[0]))
  }
  return cutHtmlFrom(t, t.indexOf(m[0]))
}

// 从 start 处切出 HTML 主体：以 </html> 收尾则其后的内容算 suffix
function cutHtmlFrom(t, start) {
  let html = t.slice(start)
  let suffix = ''
  const closeTag = html.match(/<\/html\s*>/i)
  if (closeTag) {
    const end = html.indexOf(closeTag[0]) + closeTag[0].length
    suffix = html.slice(end)
    html = html.slice(0, end)
  }
  return { prefix: t.slice(0, start), html, suffix }
}

// 流式中是否已经「开始」输出 HTML 文档：用于提前切换成占位卡片，
// 避免半截 HTML 被 v-html 当成真元素渲染进聊天框。
function isHtmlDocStarted(text) {
  const t = text || ''
  if (!t) return false
  // 强信号：已输出文档声明
  if (/<!DOCTYPE\s+html/i.test(t)) return true
  // 弱信号：<html 开头且后面已有一定内容，避免把单纯提到 <html> 标签的回答误判
  const m = t.match(/<html[\s>][\s\S]*$/i)
  if (m && m[0].length > 200) return true
  if (/```(?:html)?\s*(?:<!DOCTYPE\s+html|<html[\s>])/i.test(t)) return true
  // 兜底：已输出较多 HTML 标签片段（模型缺 DOCTYPE/<html> 直接吐 <div> 时也能提前切换占位）
  const tags = t.match(/<[a-zA-Z!\/][^>]*>/g) || []
  return tags.length >= 6
}

// 从 <title> 取文件名，取不到则使用默认名。
// 文件名中的非法字符统一替换：含 / : * ? " < > | 时浏览器可能静默拒绝下载。
function htmlExportName(text) {
  const html = extractHtmlDocument(text) || text || ''
  const m = html.match(/<title[^>]*>([\s\S]*?)<\/title>/i)
  const name = m && m[1] ? m[1].trim() : '智能体生成的页面'
  return name.replace(/[\\/:*?"<>|\r\n\t]/g, '_') + '.html'
}

// 页面是否未生成完整（缺少 </html>）：模型单次输出受 maxTokens 限制时常被截断
function isHtmlTruncated(text) {
  const t = text || ''
  if (!t) return false
  return !/<\/html\s*>/i.test(t)
}

// 页面里指向「另一个本地 html」的链接（如 /output/shanghai-1day-trip.html）
// 对应的文件从未生成过，点了必然 404。走前端本地路径（Blob 下载 / 复制 / 预览）
// 时拿不到后端导出的文件名，因此统一改写：
// 下载时用下载的文件名（保存到本地后同目录相对引用有效），预览 / 复制时降级为 #。
function rewriteDeadLocalLinks(html, selfHref) {
  if (!html) return html
  return String(html).replace(/href\s*=\s*["']([^"']+\.html)["']/gi, (m, u) => {
    const lower = String(u).toLowerCase()
    if (lower.startsWith('http://') || lower.startsWith('https://') ||
        lower.startsWith('mailto:') || lower.startsWith('data:')) return m
    return `href="${selfHref}"`
  })
}

function downloadHtml(content, filename) {
  try {
    // 下载到本地后，同目录相对引用是有效的
    const html = rewriteDeadLocalLinks(extractHtmlDocument(content) || content, filename)
    const blob = new Blob([html], { type: 'text/html;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = filename
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    setTimeout(() => URL.revokeObjectURL(url), 1000)
    ElMessage.success(`已下载：${filename}（在浏览器默认下载目录）`)
  } catch (e) {
    ElMessage.error('下载失败，可改用「复制」把源码粘贴保存为 .html 文件')
  }
}

// 复制源码兜底：部分环境（IDE 内置浏览器 / webview）不支持 Blob 下载，
// 此时复制后手动保存同样可用。
async function copyHtml(content) {
  // 复制出去后对方保存成什么名字不确定，无法指向本页，降级为不跳转
  const html = rewriteDeadLocalLinks(extractHtmlDocument(content) || content, '#')
  try {
    await navigator.clipboard.writeText(html)
    ElMessage.success('已复制 HTML 源码，粘贴保存为 .html 即可打开')
  } catch (e) {
    ElMessage.error('复制失败，请改用「预览」后在页面里手动保存')
  }
}

function previewHtml(content) {
  // Blob 预览没有真实文件路径，相对引用无效，降级为不跳转
  const html = rewriteDeadLocalLinks(extractHtmlDocument(content) || content, '#')
  const blob = new Blob([html], { type: 'text/html;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  window.open(url, '_blank')
  // 预览页需要时间加载，延迟回收，避免打开即失效
  setTimeout(() => URL.revokeObjectURL(url), 60000)
}

// 后端导出接口：普通 HTTP 链接，不依赖前端 Blob。
// 鉴权中间件支持 ?token=，所以可以直接放进 <a href> 使用。
function exportUrl(id, inline) {
  const token = localStorage.getItem('aiagent_token') || ''
  // 带后端域名：链接可直接复制分享，接收方不用自己拼地址
  return `${location.origin}/api/chat/messages/${id}/export?token=${encodeURIComponent(token)}${inline ? '&inline=1' : ''}`
}

// 预览：有消息 ID 时走后端接口（新标签页直接打开），否则退回 Blob 预览
function previewMessage(msg) {
  if (msg && msg.id) {
    window.open(exportUrl(msg.id, true), '_blank')
    return
  }
  previewHtml(msg?.parts?.html || msg?.content || '')
}

// 复制免登录分享链接。
// 注意不能直接把 exportUrl()（带 token）发给别人——那等于泄露自己的登录凭据，
// 这里先向后端换取静态地址再复制。
async function copyShareLink(msg) {
  if (!msg?.id) return
  try {
    const res = await getExportUrl(msg.id)
    const url = res?.data?.url
    if (!url) throw new Error('empty url')
    await navigator.clipboard.writeText(url)
    ElMessage.success('已复制分享链接，对方无需登录即可打开')
  } catch (e) {
    ElMessage.error('复制链接失败，可点「下载」后分享下载到的文件')
  }
}

watch(() => messages.value.length, scrollToBottom)
</script>

<style scoped>
.html-export-card {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 14px;
  border: 1px solid var(--border-color, #e4e7ed);
  border-radius: 10px;
  background: #f7f9fc;
}
.html-export-icon { font-size: 28px; color: #409eff; flex-shrink: 0; }
.html-export-body { flex: 1; min-width: 0; }
.html-export-name { font-weight: 600; color: #303133; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.html-export-desc { font-size: 12px; color: #909399; margin-top: 2px; }
.html-export-actions { display: flex; gap: 8px; flex-shrink: 0; }

/* 生成中的 HTML 页面占位卡片（流式输出期间） */
.html-export-pending .html-export-desc {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: var(--primary);
}

/* 内容被截断的提示 */
.html-export-warn { color: #e6a23c; }

.chat-panel {
  display: flex;
  flex-direction: row;
  height: 100%;
  background: #fff;
}

/* 会话侧边栏 */
.session-sidebar {
  width: 248px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  min-height: 0;
  border-right: 1px solid var(--border);
  background: #fafbfc;
}

.session-toolbar {
  padding: 12px;
  border-bottom: 1px solid var(--border);
}

.new-session-btn { width: 100%; }

.session-list {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 8px;
}

.session-item {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 10px;
  margin-bottom: 4px;
  border-radius: 8px;
  cursor: pointer;
  transition: background 0.2s;
}

.session-item:hover { background: rgba(79, 110, 247, 0.08); }
.session-item:hover .session-actions { opacity: 1; }

.session-item.active {
  background: linear-gradient(135deg, rgba(79, 110, 247, 0.12), rgba(124, 92, 240, 0.12));
  box-shadow: inset 2px 0 0 var(--primary);
}

.session-main { flex: 1; min-width: 0; }

.session-title {
  font-size: 13px;
  color: var(--text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.session-meta {
  font-size: 11px;
  color: var(--text-secondary);
  margin-top: 2px;
}

/* 后台生成中：直接标在会话条目上，不再用全局提示条 */
.session-running {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: var(--primary);
  font-weight: 500;
}

.session-actions {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  opacity: 0;
  transition: opacity 0.2s;
}

.pin-icon {
  flex-shrink: 0;
  font-size: 12px;
  color: #e6a23c;
}

/* 右侧聊天主体 */
.chat-body {
  flex: 1;
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.message-list {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 24px;
}

.welcome {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
  text-align: center;
}

.welcome-icon { font-size: 56px; margin-bottom: 14px; }

.welcome h2 {
  font-size: 22px;
  margin-bottom: 6px;
  background: var(--primary-gradient);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
}

.welcome p { color: var(--text-secondary); font-size: 13px; margin-bottom: 20px; }

.welcome-hints {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  justify-content: center;
  max-width: 520px;
}

.hint-tag { cursor: pointer; transition: all 0.2s; }
.hint-tag:hover { background: var(--primary); color: #fff; border-color: var(--primary); }

.message { display: flex; margin-bottom: 20px; gap: 12px; }
.message.user { flex-direction: row-reverse; }
.message-avatar { flex-shrink: 0; }
.ai-avatar { font-size: 24px; }
.message-content { max-width: 75%; }
.message.user .message-content { text-align: right; }

.message-text {
  background: var(--bg);
  padding: 12px 16px;
  border-radius: 12px;
  line-height: 1.7;
  font-size: 14px;
}

.message.user .message-text { background: var(--primary-gradient); color: #fff; }

.message-error {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  background: #fef0f0;
  border: 1px solid #fcd3d3;
  color: #c45656;
  white-space: pre-wrap;
  word-break: break-word;
}

.message-error .error-icon { flex-shrink: 0; }
.message-error .error-text { flex: 1; min-width: 0; }

.agent-trace {
  margin-bottom: 10px;
  padding: 10px 12px;
  background: linear-gradient(135deg, rgba(79, 110, 247, 0.06), rgba(124, 92, 240, 0.06));
  border: 1px solid rgba(79, 110, 247, 0.2);
  border-radius: 10px;
  font-size: 12px;
}

.agent-status {
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--primary);
  font-weight: 600;
  margin-bottom: 6px;
}

.agent-tool-item {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px 0;
  color: var(--text-secondary);
  border-top: 1px dashed rgba(79, 110, 247, 0.15);
}

.agent-tool-item:first-of-type { border-top: none; }
.agent-tool-name { font-weight: 600; color: var(--text); flex-shrink: 0; }

.agent-tool-brief {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* ---------- 人工确认卡片 ---------- */
.approval-card {
  margin: 12px 0;
  padding: 14px 16px;
  border-radius: 10px;
  border: 1px solid rgba(230, 162, 60, 0.35);
  background: linear-gradient(135deg, rgba(230, 162, 60, 0.08), rgba(245, 108, 108, 0.06));
}
.approval-card.risk-high {
  border-color: rgba(245, 108, 108, 0.45);
  background: linear-gradient(135deg, rgba(245, 108, 108, 0.1), rgba(230, 162, 60, 0.06));
}

.ap-head { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; }
.ap-icon { font-size: 16px; }
.ap-title { font-size: 14px; font-weight: 600; color: var(--text); }

.ap-reason { font-size: 13px; color: var(--text-secondary); margin-bottom: 8px; }

.ap-tool { font-size: 13px; color: var(--text-secondary); margin-bottom: 8px; }
.ap-tool-name {
  margin-left: 6px;
  font-family: 'JetBrains Mono', Consolas, monospace;
  font-size: 12px;
  color: var(--text-muted);
}

.ap-cmd {
  margin: 0 0 10px;
  padding: 10px 12px;
  border-radius: 6px;
  background: #1e1e2e;
  color: #cdd6f4;
  font-family: 'JetBrains Mono', Consolas, monospace;
  font-size: 12.5px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 220px;
  overflow: auto;
}

.ap-remember { display: flex; margin-bottom: 10px; }
.ap-actions { display: flex; justify-content: flex-end; gap: 8px; }

.agent-summary {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: -8px 0 16px;
  padding-left: 44px;
}

.agent-summary-text { font-size: 12px; color: var(--text-secondary); }

.typing-indicator { animation: blink 1s infinite; color: var(--primary); }

@keyframes blink {
  0%, 100% { opacity: 0; }
  50% { opacity: 1; }
}

.input-area {
  padding: 16px 24px;
  border-top: 1px solid var(--border);
}

.input-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 10px;
}

.mode-tip { font-size: 13px; color: var(--text-secondary); flex-shrink: 0; }
.model-warn { font-size: 13px; color: #e6a23c; }
.model-select { width: 220px; }

/* ---------- 权限模式选择 ---------- */
.mode-picker { display: flex; align-items: center; gap: 6px; margin-left: auto; }
.mode-picker-label { font-size: 13px; color: var(--text-secondary); }
.mode-select { width: 128px; }
.mode-opt-desc { float: right; color: var(--text-muted); font-size: 12px; margin-left: 12px; }

.full-access-tip,
.delegated-tip {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 10px;
  padding: 7px 10px;
  border-radius: 6px;
  font-size: 12.5px;
  line-height: 1.5;
}
.full-access-tip {
  color: #c45656;
  background: rgba(245, 108, 108, 0.1);
  border: 1px solid rgba(245, 108, 108, 0.3);
}
.delegated-tip {
  color: #b88230;
  background: rgba(230, 162, 60, 0.1);
  border: 1px solid rgba(230, 162, 60, 0.3);
}

.stream-actions { margin-top: 10px; }

@media (max-width: 768px) {
  .session-sidebar { display: none; }
}
</style>
