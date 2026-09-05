<template>
  <section class="ops-chat">
    <div class="chat-scroll" ref="scrollRef">
      <div v-if="messages.length === 0 && !streaming" class="chat-welcome">
        <div class="welcome-icon">🖥️</div>
        <h3>{{ scopeLabel }}</h3>
        <p>
          {{ scopeHint }}
        </p>
        <div class="welcome-hints">
          <el-tag
            v-for="q in quickCommands"
            :key="q"
            class="hint-tag"
            @click="sendQuick(q)"
          >{{ q }}</el-tag>
        </div>
      </div>

      <div v-for="(msg, idx) in messages" :key="idx" class="msg" :class="msg.role">
        <div class="msg-avatar">
          <el-avatar v-if="msg.role === 'user'" :size="30" style="background: var(--primary-gradient)">U</el-avatar>
          <span v-else class="ai-avatar">🖥️</span>
        </div>
        <div class="msg-body">
          <div v-if="msg.error" class="msg-bubble msg-error">
            <span>⚠️</span><span>{{ msg.error }}</span>
          </div>
          <div v-else-if="!msg.parts || !msg.parts.html" class="msg-bubble" v-html="renderMarkdown(msg.content)"></div>
          <!-- 「引导语 + HTML 文档」混合：引导语正常渲染，HTML 主体渲染成下载卡片 -->
          <template v-else>
            <div v-if="msg.parts.prefix" class="msg-bubble" v-html="renderMarkdown(msg.parts.prefix)"></div>
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
            <div v-if="msg.parts.suffix" class="msg-bubble" v-html="renderMarkdown(msg.parts.suffix)"></div>
          </template>
        </div>
      </div>

      <!-- 流式输出中 -->
      <div v-if="streaming" class="msg assistant">
        <div class="msg-avatar"><span class="ai-avatar">🖥️</span></div>
        <div class="msg-body">
          <div class="stream-status">
            <el-icon class="is-loading"><Loading /></el-icon>
            <span>正在执行…（工具 {{ agentToolCalls.length }} 次）</span>
          </div>

          <!-- 执行轨迹：本次会话调用过的工具与结果摘要 -->
          <div v-if="agentToolCalls.length" class="trace-list">
            <div v-for="(tc, i) in agentToolCalls" :key="i" class="trace-item" :class="tc.status">
              <span class="trace-idx">{{ i + 1 }}</span>
              <span class="trace-name">{{ toolLabel(tc.name) }}</span>
              <el-tag size="small" effect="plain" :type="tagType(tc.status)">{{ statusText(tc.status) }}</el-tag>
              <span class="trace-brief">{{ brief(tc.error || tc.output) }}</span>
            </div>
          </div>

          <!-- 人工确认卡片 -->
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
              <el-button size="small" type="danger" plain :loading="approvalSubmitting" @click="handleApproval(false)">拒绝</el-button>
              <el-button size="small" type="primary" :loading="approvalSubmitting" @click="handleApproval(true)">允许执行</el-button>
            </div>
          </div>

          <div v-if="streamError" class="msg-bubble msg-error">
            <span>⚠️</span><span>{{ streamError }}</span>
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
          <div v-else class="msg-bubble" v-html="renderMarkdown(streamContent)"></div>

          <div v-if="!streamError" class="stream-actions">
            <el-button size="small" :icon="VideoPause" @click="stop">停止</el-button>
          </div>
        </div>
      </div>
    </div>


    <!-- 输入区 -->
    <div class="chat-input">
      <div class="input-toolbar">
        <span class="scope-tip" :title="scopeHint">🎯 {{ scopeLabel }}</span>
        <!-- 模型选择：与通用聊天一致，候选来自智能体「模型」页已启用的对话模型 -->
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
        <div class="mode-picker">
          <span class="mode-label">权限</span>
          <el-select v-model="approvalMode" size="small" class="mode-select">
            <el-option v-for="m in APPROVAL_MODES" :key="m.value" :label="m.label" :value="m.value">
              <span>{{ m.label }}</span>
              <span class="mode-opt-desc">{{ m.desc }}</span>
            </el-option>
          </el-select>
        </div>
      </div>
      <div v-if="approvalMode === 'full_access'" class="mode-tip tip-danger">
        <el-icon><WarningFilled /></el-icon>
        <span>完全权限：高风险操作（重启、kill、写系统路径）将自动执行，仅灾难性命令拦截。</span>
      </div>
      <div v-else-if="approvalMode === 'delegated'" class="mode-tip tip-warning">
        <el-icon><InfoFilled /></el-icon>
        <span>委托审批：常规操作自动执行，高风险操作仍会被拒绝并提示需要完全权限。</span>
      </div>
      <el-input
        v-model="inputText"
        type="textarea"
        :rows="2"
        :autosize="{ minRows: 2, maxRows: 5 }"
        resize="none"
        :placeholder="placeholder"
        @keydown.enter="onEnter"
      >
        <template #append>
          <el-button v-if="streaming" type="danger" :icon="Close" @click="stop">停止</el-button>
          <el-button v-else type="primary" :icon="Promotion" :disabled="!inputText.trim()" @click="send" />
        </template>
      </el-input>
    </div>
  </section>
</template>

<script setup>
import { ref, computed, watch, nextTick, onBeforeUnmount } from 'vue'
import { wsChatUrl, listMessages, resolveApproval, getExportUrl } from '@/api/chat'
import { getAgentModels } from '@/api/agent'
import { createStreamDeltaBuffer } from '@/utils/streaming'
import { marked } from 'marked'
import { ElMessage } from 'element-plus'
import { Promotion, Close, VideoPause, WarningFilled, InfoFilled, Document, Download, View, CopyDocument, Link } from '@element-plus/icons-vue'

const props = defineProps({
  agentId: { type: Number, required: true },
  agent: { type: Object, default: null },
  // 会话作用域：{ type: 'global' | 'host' | 'host_group', id, name }
  scope: { type: Object, required: true },
  sessionId: { type: Number, default: 0 },
})
const emit = defineEmits(['session', 'trace', 'running-change'])

const APPROVAL_MODES = [
  { value: 'manual', label: '人工审批', desc: '危险操作逐个确认后执行' },
  { value: 'delegated', label: '委托审批', desc: '常规操作自动执行，高风险仍拒绝' },
  { value: 'full_access', label: '完全权限', desc: '高风险也自动执行，仅红线阻断' },
]
const APPROVAL_MODE_KEY = 'aiagent_approval_mode'
const approvalMode = ref(localStorage.getItem(APPROVAL_MODE_KEY) || 'manual')
watch(approvalMode, v => localStorage.setItem(APPROVAL_MODE_KEY, v))

// 该智能体已启用的对话模型（绑定关系在智能体「模型」页配置，此处只做本次对话的临时切换）
const chatModels = ref([])
const selectedModelId = ref(0)

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
    selectedModelId.value = (models.find(m => m.isPrimary) || models[0])?.modelId || 0
  } catch { /* 忽略 */ }
}

const scrollRef = ref(null)
const messages = ref([])
const inputText = ref('')
const streaming = ref(false)
const streamContent = ref('')
// 本轮助手回复落库后的消息 ID，用于走后端导出接口下载（不依赖 Blob）
const lastMessageId = ref(0)
const streamError = ref('')
const agentToolCalls = ref([])
const pendingApproval = ref(null)
const approvalRemember = ref(false)
const approvalSubmitting = ref(false)

let ws = null
// 已与界面解绑、但仍在后台跑完并落库的连接（切换主机/会话时产生）。
// 用数组而非 Set：需要响应式，才能在界面上显示「后台仍在生成」的提示。
const backgroundConns = ref([])
// 正在后台生成的会话 ID，上报给父组件，用于在会话下拉里标 running。
// 单独存一份响应式数据——连接对象上的属性变化不会触发视图更新。
const runningSessionIds = ref([])

function markRunning(sessionId, on) {
  if (!sessionId) return
  const set = new Set(runningSessionIds.value)
  if (on) set.add(sessionId)
  else set.delete(sessionId)
  runningSessionIds.value = [...set]
}

// 后台生成中的会话变化时上报，父组件据此在会话标签上标 running。
// 注意：必须在 runningSessionIds 声明之后注册，const 有暂时性死区，提前访问会抛 ReferenceError。
watch(runningSessionIds, (ids) => {
  emit('running-change', ids || [])
})

// 由本组件首条消息创建出来的会话 ID：回填后跳过一次历史重载，避免覆盖刚生成的回答
let selfCreatedSessionId = 0

// cancelStreaming 真正中断：关闭连接并丢弃未落定的内容。
// 只在组件卸载时用——那是连接必须结束的场景。
function cancelStreaming() {
  if (ws) {
    const conn = ws
    // 先断开引用：onclose 据此判断为「主动取消」，不把半截回答落进（新的）会话
    ws = null
    try { conn.close() } catch { /* ignore */ }
  }
  backgroundConns.value.forEach(conn => {
    try { conn.close() } catch { /* ignore */ }
  })
  backgroundConns.value = []
  runningSessionIds.value = []
  resetStreamState()
}

// detachStreaming 与界面解绑，但不打断生成。
//
// 切换主机 / 切换会话时用它而不是 cancelStreaming：
// 直接关闭连接会让后端按「中断」处理，只留下半截回答，
// 而界面又把已生成的内容清掉，用户切回来就什么都看不到了。
// 正确做法是：已生成的内容先落到消息列表，连接交给后台跑完并写入对应会话。
function detachStreaming() {
  if (!streaming.value) {
    resetStreamState()
    return
  }
  // 1. 有待确认的操作：主动取消，否则后台连接会一直挂到审批超时
  if (pendingApproval.value) {
    resolveApproval(pendingApproval.value.id, {
      approved: false,
      comment: '用户已切换会话，操作取消',
    }).catch(() => { /* 已超时则忽略 */ })
  }
  // 2. 连接移交后台：进度原地保存在连接上，切回来原样恢复，
  //    包括「正在执行…（工具 N 次）」和「停止」按钮，不被清掉
  if (ws) {
    ws._detached = true
    ws._sessionId = props.sessionId || 0
    ws._streamContent = streamContent.value
    ws._streamError = streamError.value
    ws._toolCalls = agentToolCalls.value
    // 连整份消息列表一起冻结：里面有用户刚发出去、后端还没回的内容，
    // 只保存流式内容的话，切回来用户消息就丢了
    ws._messages = [...messages.value]
    backgroundConns.value.push(ws)
    markRunning(ws._sessionId, true)
    ws = null
  }
  resetStreamState()
}

// reattach 切回某个会话时，把它后台仍在跑的连接重新接管到界面上，
// 恢复流式进度与「正在执行 / 停止」状态；没有在跑的连接则返回 false。
function reattach(sessionId) {
  if (!sessionId) return false
  const conn = backgroundConns.value.find(
    c => c._sessionId === sessionId && c.readyState === WebSocket.OPEN
  )
  if (!conn) return false

  conn._detached = false
  ws = conn
  backgroundConns.value.splice(backgroundConns.value.indexOf(conn), 1)
  // 回到前台，不再是后台生成中
  markRunning(sessionId, false)

  streaming.value = true
  // 先恢复整份消息（含用户消息），再接上流式进度，顺序不能反
  messages.value = conn._messages || []
  streamContent.value = conn._streamContent || ''
  streamError.value = conn._streamError || ''
  agentToolCalls.value = conn._toolCalls || []
  emit('trace', agentToolCalls.value)
  scrollToBottom()
  return true
}

function resetStreamState() {
  streaming.value = false
  streamContent.value = ''
  streamError.value = ''
  agentToolCalls.value = []
  pendingApproval.value = null
  approvalRemember.value = false
}

const scopeLabel = computed(() => {
  if (props.scope.type === 'host') return props.scope.name || '主机'
  if (props.scope.type === 'host_group') return `主机组 · ${props.scope.name || ''}`
  return '全部主机'
})
const scopeHint = computed(() => {
  if (props.scope.type === 'host') {
    return `当前会话固定操作「${props.scope.name}」，不指定主机时默认就是它。下方「主机」标签页可直接开 SSH 终端。`
  }
  if (props.scope.type === 'host_group') {
    return `当前会话面向主机组「${props.scope.name}」，Agent 会先列出组内主机再操作；组有多台机器，因此不提供 SSH 终端。`
  }
  return '未锁定具体机器，Agent 会在你授权的主机范围内自行判断。'
})
const placeholder = computed(() => {
  if (props.scope.type === 'host') return `对 ${props.scope.name} 下达指令，例如：查看磁盘占用前 5 的目录…`
  if (props.scope.type === 'host_group') return `对主机组 ${props.scope.name} 下达指令，例如：检查所有主机的负载…`
  return '输入运维指令，例如：查看服务器磁盘使用情况…'
})
const quickCommands = computed(() => {
  if (props.scope.type === 'host') return ['查看磁盘使用情况', '查看内存与负载', '查看最近的系统日志错误']
  if (props.scope.type === 'host_group') return ['列出组内所有主机', '检查所有主机的负载', '检查所有主机的磁盘']
  return ['列出所有可用主机', '检查所有主机的负载']
})

// 超长文本直接交给 marked 解析会长时间占用主线程：浏览器随之停止消费 WebSocket 数据，
// TCP 窗口关闭后后端写入就会超时（表现为前端一直卡在「正在执行」）。
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

// ---------- HTML 页面：下载 / 预览 ----------
// 提取智能体返回的 HTML 文档：支持直接输出，或被 ```html 代码块包裹；非 HTML 返回空串
function extractHtmlDocument(text) {
  const t = (text || '').trim()
  if (/^<!DOCTYPE\s+html/i.test(t) || (/<html[\s>]/i.test(t) && /<\/html>\s*$/i.test(t))) return t
  const fence = t.match(/^```(?:html)?\s*([\s\S]*?)```\s*$/i)
  if (fence) {
    const inner = fence[1].trim()
    if (/^<!DOCTYPE\s+html/i.test(inner) || /<html[\s>]/i.test(inner)) return inner
  }
  return ''
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
// 不能直接把 exportUrl()（带 token）发给别人——那等于泄露自己的登录凭据，
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

function scrollToBottom() {
  nextTick(() => {
    const el = scrollRef.value
    if (el) el.scrollTop = el.scrollHeight
  })
}

async function loadHistory() {
  if (!props.sessionId) {
    messages.value = []
    return
  }
  try {
    const res = await listMessages(props.sessionId)
    messages.value = (res.data || []).map(m => ({
      role: m.role, content: m.content, id: m.id || 0, parts: splitHtmlDocument(m.content)
    }))
    scrollToBottom()
  } catch { messages.value = [] }
}

// 会话 ID 变化：通常意味着切换/新建会话，需要中断进行中的生成并重新拉历史。
// 例外：会话是本组件首条消息刚创建的（后端回填 ID），此时历史已在界面上，
// 重载会把刚流出来的回答覆盖掉（还可能因为保存时序丢内容），必须跳过。
watch(() => props.sessionId, (id) => {
  if (id && id === selfCreatedSessionId) {
    selfCreatedSessionId = 0
    return
  }
  // 切换会话：解绑而非中断，正在生成的回答继续在后台跑完。
  // 切回同一个会话时重新接管，恢复「正在执行 / 停止」的进度。
  detachStreaming()
  if (!reattach(id)) loadHistory()
}, { immediate: true })
watch(() => props.agentId, loadAgentModels, { immediate: true })
watch(() => props.scope.id + props.scope.type, () => {
  // 切换操作对象：解绑当前生成（内容保留），历史由父组件按作用域重新拉取
  detachStreaming()
  emit('trace', [])
})

function onEnter(e) {
  if (e.shiftKey) return
  e.preventDefault()
  if (streaming.value) stop()
  else send()
}

function sendQuick(q) {
  inputText.value = q
  send()
}

// 点停止后等待后端确认的兜底计时器：连接拥塞时后端可能发不出 done，
// 超时后在本地收尾，保证「停止」一定生效，不会一直转圈。
let stopTimer = null

function stop() {
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

function send() {
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
  streamError.value = ''
  agentToolCalls.value = []
  pendingApproval.value = null
  approvalRemember.value = false

  const conn = new WebSocket(wsChatUrl())
  ws = conn
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

  // 看门狗挂在 conn 上，onclose / 卸载时统一清理
  conn._watchdog = setInterval(() => {
    if (conn._doneReceived || conn._detached) return
    if (Date.now() - lastActivityAt <= STALE_TIMEOUT_MS) return
    if (pollTimes >= MAX_POLL_TIMES) {
      // 轮询次数用尽仍未拿到结果：收尾并提示，避免界面永久转圈
      if (!conn._pollGaveUp) {
        conn._pollGaveUp = true
        ElMessage.warning('生成耗时较长，结果仍在后台处理，可稍后刷新页面查看')
        finishStream()
      }
      return
    }
    pollTimes++
    const sid = props.sessionId || selfCreatedSessionId
    if (!sid) return
    listMessages(sid).then(res => {
      if (res.code !== 0 || conn._doneReceived || conn._detached) return
      // 本轮新落库的 assistant 回复：后端已入库即代表这一轮生成完毕
      const list = res.data || []
      const reply = list.find(m => m.role === 'assistant' && (m.id || 0) > baselineMaxId)
      if (!reply || !reply.content) return
      messages.value = list.map(m => ({
        role: m.role, content: m.content, id: m.id || 0, parts: splitHtmlDocument(m.content)
      }))
      conn._doneReceived = true
      scrollToBottom()
      // 已用后端权威结果覆盖，不再把本地残缺流内容补进会话
      finalizeStream()
    }).catch(() => { /* 网络异常，下一轮继续轮询 */ })
  }, POLL_INTERVAL_MS)

  conn.onopen = () => {
    lastActivityAt = Date.now()
    conn.send(JSON.stringify({
      type: 'agent',
      sessionId: props.sessionId || 0,
      agentId: props.agentId,
      knowledgeId: 0,
      modelId: selectedModelId.value || 0,
      approvalMode: approvalMode.value || 'manual',
      // 运维会话绑定操作对象：首轮由后端据此创建会话
      scopeType: props.scope.type || 'global',
      scopeId: props.scope.id || 0,
      scopeName: props.scope.name || '',
      message: text,
    }))
  }

  conn.onmessage = (event) => {
    lastActivityAt = Date.now()
    let data
    try { data = JSON.parse(event.data) } catch { return }
    // 已与界面解绑（切换主机/会话）：进度累积在连接上，绝不更新界面，
    // 否则旧会话的审批卡片、流式内容会跑到新会话的界面上。
    // 切回来时 reattach 会把这些进度原样恢复。
    if (conn._detached) {
      switch (data.type) {
        case 'session':
          // 后台连接同样记住新会话 ID，否则切回来 reattach 找不到它
          if (data.sessionId) conn._sessionId = data.sessionId
          break
        case 'text':
          conn._streamContent = (conn._streamContent || '') + (data.content || '')
          break
        case 'error':
          conn._streamError = data.error || 'Agent 执行出错'
          break
        case 'tool':
          conn._toolCalls = [...(conn._toolCalls || []), { name: data.name, output: '执行中...', status: 'running' }]
          break
        case 'tool_result': {
          const list = [...(conn._toolCalls || [])]
          for (let i = list.length - 1; i >= 0; i--) {
            if (!data.name || list[i].name === data.name) {
              list[i] = {
                name: list[i].name,
                output: data.output || '',
                error: data.error || '',
                status: data.error ? 'error' : 'done',
              }
              break
            }
          }
          conn._toolCalls = list
          break
        }
        case 'done':
          // done 即生成结束：清掉 running 标记，不再标成运行中，
          // 避免「回答出来了、会话还挂着生成中」。
          if (data.sessionId) {
            markRunning(conn._sessionId, false)
            conn._sessionId = data.sessionId
          }
          markRunning(conn._sessionId, false)
          if (Array.isArray(data.toolCalls) && data.toolCalls.length) {
            conn._toolCalls = data.toolCalls.map(t => ({ ...t, status: t.error ? 'error' : 'done' }))
          }
          // 主动关连接：detached 分支 return 后走不到正常分支的 close，
          // 不关则连接悬挂，会话上的「生成中」状态清不掉。
          try { conn.close() } catch { /* ignore */ }
          break
      }
      return
    }
    switch (data.type) {
      case 'session':
        // 新会话已在后端落库：立刻把 ID 交给父组件，左侧会话列表随即出现该条目，
        // 不必等到生成结束的 done 事件才出现。
        if (data.sessionId && !props.sessionId && !selfCreatedSessionId) {
          selfCreatedSessionId = data.sessionId
          emit('session', data.sessionId)
        }
        break
      case 'tool':
        agentToolCalls.value.push({ name: data.name, output: '执行中...', status: 'running' })
        emit('trace', agentToolCalls.value)
        break
      case 'tool_result': {
        const list = agentToolCalls.value
        for (let i = list.length - 1; i >= 0; i--) {
          if (!data.name || list[i].name === data.name) {
            list[i] = {
              name: list[i].name,
              output: data.output || '',
              error: data.error || '',
              status: data.error ? 'error' : 'done',
            }
            break
          }
        }
        emit('trace', agentToolCalls.value)
        break
      }
      case 'text':
        deltaBuffer.push(data.content || '')
        break
      case 'approval_request':
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
        streamError.value = data.error || 'Agent 执行出错'
        break
      case 'done':
        conn._doneReceived = true
        deltaBuffer.flushNow()
        if (Array.isArray(data.toolCalls) && data.toolCalls.length) {
          agentToolCalls.value = data.toolCalls.map(t => ({ ...t, status: t.error ? 'error' : 'done' }))
          emit('trace', agentToolCalls.value)
        }
        lastMessageId.value = data.messageId || 0
        if (data.sessionId) {
          // 首条消息创建出来的会话：记住 ID，避免父组件回填后重载历史、覆盖刚生成的回答
          if (!props.sessionId) selfCreatedSessionId = data.sessionId
          emit('session', data.sessionId)
        }
        conn.close()
        break
    }
  }

  conn.onerror = () => {
    if (!conn._detached) ElMessage.error('连接失败，请检查后端服务是否可用')
  }
  conn.onclose = () => {
    clearInterval(conn._watchdog)
    const idx = backgroundConns.value.indexOf(conn)
    if (idx >= 0) backgroundConns.value.splice(idx, 1)
    // 后台跑完的连接：如果它属于当前正查看的会话，刷新一次历史拿到完整结果
    if (conn._detached) {
      markRunning(conn._sessionId, false)
      if (conn._sessionId && conn._sessionId === props.sessionId) loadHistory()
      return
    }
    // ws 已被置空 = 主动中断（组件卸载），丢弃未落定的内容；
    // 否则是正常结束或用户点停止，把已生成的部分落进消息列表
    if (ws !== conn) return
    ws = null
    // 正常收到 done：内容已在 done 分支 flush 并准备好，直接落进消息列表
    if (conn._doneReceived) {
      finishStream()
      return
    }
    // 异常关闭（没收到 done）：先 flush 缓冲尾段，再优先用后端已落库的完整结果覆盖界面；
    // 拉取失败或还没有会话 ID 则用本地流内容兜底。避免 UI 永久卡在「正在执行」，
    // 或残留一段没收到 done 的残缺回复。
    deltaBuffer.flushNow()
    const sid = props.sessionId || selfCreatedSessionId
    if (sid) {
      listMessages(sid).then(res => {
        if (res.code === 0) {
          messages.value = (res.data || []).map(m => ({
            role: m.role, content: m.content, id: m.id || 0, parts: splitHtmlDocument(m.content)
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

onBeforeUnmount(cancelStreaming)

function finishStream() {
  if (stopTimer) { clearTimeout(stopTimer); stopTimer = null }
  streaming.value = false
  if (streamContent.value) {
    messages.value.push({
      role: 'assistant',
      content: streamContent.value,
      // 后端消息 ID：有它就能走导出接口下载，比 Blob 下载兼容性更好
      id: lastMessageId.value || 0,
      // 预计算拆分结果：HTML 文档渲染成下载卡片，引导语按 markdown 显示
      parts: splitHtmlDocument(streamContent.value)
    })
  }
  if (streamError.value) {
    messages.value.push({ role: 'assistant', content: '', error: streamError.value })
  }
  streamContent.value = ''
  streamError.value = ''
  pendingApproval.value = null
  scrollToBottom()
}

// 仅收尾、不把本地残缺流内容落进会话：异常关闭且已从后端拉到权威结果时调用
function finalizeStream() {
  if (stopTimer) { clearTimeout(stopTimer); stopTimer = null }
  streaming.value = false
  streamContent.value = ''
  streamError.value = ''
  pendingApproval.value = null
  scrollToBottom()
}

async function handleApproval(approved) {
  const pending = pendingApproval.value
  if (!pending || approvalSubmitting.value) return
  approvalSubmitting.value = true
  try {
    await resolveApproval(pending.id, { approved, remember: approved && approvalRemember.value })
    pendingApproval.value = null
    approvalRemember.value = false
  } catch {
    ElMessage.error('提交确认结果失败，请重试')
  } finally {
    approvalSubmitting.value = false
  }
}

function statusText(s) {
  return { running: '执行中', done: '完成', error: '失败' }[s] || '完成'
}
function tagType(s) {
  return { running: 'warning', done: 'success', error: 'danger' }[s] || 'info'
}
function brief(text) {
  const first = String(text || '').split('\n').find(l => l.trim()) || ''
  return first.length > 80 ? first.slice(0, 80) + '…' : first
}

function toolLabel(name) {
  const m = {
    list_hosts: '列出主机', exec_command: '执行命令', list_dir: '列出目录',
    read_file: '读取文件', write_file: '写入文件', get_time: '获取时间',
  }
  return m[name] || name
}

defineExpose({ sendQuick })
</script>

<style scoped>
.ops-chat { flex: 1; display: flex; flex-direction: column; min-height: 0; }

/* 与上方标签栏、头部统一 20px 边距，保证内容左边缘在同一条线上 */
.chat-scroll { flex: 1; overflow: auto; padding: 18px 20px; }

.chat-welcome { text-align: center; padding: 48px 20px; color: var(--text-secondary); }
.welcome-icon { font-size: 40px; margin-bottom: 10px; }
.chat-welcome h3 { margin: 0 0 8px; font-size: 16px; color: var(--text); }
.chat-welcome p { margin: 0 auto 16px; max-width: 460px; font-size: 13px; line-height: 1.6; }
.welcome-hints { display: flex; flex-wrap: wrap; gap: 8px; justify-content: center; }
.hint-tag { cursor: pointer; }

.msg { display: flex; gap: 10px; margin-bottom: 18px; }
/* 用户消息靠右：头像与气泡整体翻转，气泡内文字仍左对齐 */
.msg.user { flex-direction: row-reverse; }
.msg.user .msg-body { display: flex; justify-content: flex-end; }
.msg-body { flex: 1; min-width: 0; }
.msg-avatar { flex-shrink: 0; }
.ai-avatar {
  display: inline-flex; align-items: center; justify-content: center;
  width: 30px; height: 30px; border-radius: 50%; background: var(--bg-subtle); font-size: 15px;
}

/* 气泡：AI 灰底居左，用户主色渐变居右 */
.msg-bubble {
  display: inline-block;
  max-width: 88%;
  padding: 9px 13px;
  border-radius: 10px;
  font-size: 14px;
  line-height: 1.7;
  word-break: break-word;
  text-align: left;
}
.msg.assistant .msg-bubble {
  background: var(--bg-subtle);
  color: var(--text);
  border-top-left-radius: 3px;
}
.msg.user .msg-bubble {
  background: var(--primary-gradient);
  color: #fff;
  border-top-right-radius: 3px;
}
/* 用户气泡里的链接与代码保持可读 */
.msg.user .msg-bubble :deep(a) { color: #fff; text-decoration: underline; }

.msg-bubble :deep(p) { margin: 0 0 8px; }
.msg-bubble :deep(p:last-child) { margin-bottom: 0; }
.msg-bubble :deep(pre) {
  background: #1e1e2e; color: #cdd6f4; padding: 10px 12px; border-radius: 6px;
  overflow: auto; font-size: 12.5px;
}
.msg-bubble :deep(code) { font-family: 'JetBrains Mono', Consolas, monospace; }

.msg-error {
  display: flex; gap: 6px; padding: 10px 12px; border-radius: 8px;
  background: rgba(245, 108, 108, 0.1); color: #c45656; font-size: 13px;
}

.stream-status {
  display: flex; align-items: center; gap: 6px;
  font-size: 12.5px; color: var(--text-secondary); margin-bottom: 8px;
}
/* 执行轨迹 */
.trace-list {
  margin: 8px 0 12px;
  padding: 8px 10px;
  border-radius: 8px;
  background: linear-gradient(135deg, rgba(79, 110, 247, 0.06), rgba(124, 92, 240, 0.06));
  border: 1px solid rgba(79, 110, 247, 0.15);
}
.trace-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 0;
  font-size: 12.5px;
}
.trace-item + .trace-item { border-top: 1px dashed rgba(79, 110, 247, 0.15); }
.trace-item.error .trace-brief { color: #c45656; }
.trace-idx {
  width: 18px; height: 18px; border-radius: 50%;
  background: var(--primary-soft); color: var(--primary);
  font-size: 11px; line-height: 18px; text-align: center; flex-shrink: 0;
}
.trace-name { font-weight: 600; color: var(--text); flex-shrink: 0; }
.trace-brief {
  flex: 1; min-width: 0;
  color: var(--text-secondary);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  font-family: 'JetBrains Mono', Consolas, monospace;
}

.stream-actions { margin-top: 8px; }

/* 审批确认卡 */
.approval-card {
  margin: 10px 0; padding: 14px 16px; border-radius: 10px;
  border: 1px solid rgba(230, 162, 60, 0.35);
  background: linear-gradient(135deg, rgba(230, 162, 60, 0.08), rgba(245, 108, 108, 0.06));
}
.approval-card.risk-high {
  border-color: rgba(245, 108, 108, 0.45);
  background: linear-gradient(135deg, rgba(245, 108, 108, 0.1), rgba(230, 162, 60, 0.06));
}
.ap-head { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; }
.ap-title { font-size: 14px; font-weight: 600; color: var(--text); }
.ap-reason { font-size: 13px; color: var(--text-secondary); margin-bottom: 8px; }
.ap-tool { font-size: 13px; color: var(--text-secondary); margin-bottom: 8px; }
.ap-tool-name {
  margin-left: 6px; font-family: 'JetBrains Mono', Consolas, monospace;
  font-size: 12px; color: var(--text-muted);
}
.ap-cmd {
  margin: 0 0 10px; padding: 10px 12px; border-radius: 6px;
  background: #1e1e2e; color: #cdd6f4;
  font-family: 'JetBrains Mono', Consolas, monospace;
  font-size: 12.5px; line-height: 1.6; white-space: pre-wrap;
  word-break: break-all; max-height: 200px; overflow: auto;
}
.ap-remember { display: flex; margin-bottom: 10px; }
.ap-actions { display: flex; justify-content: flex-end; gap: 8px; }

/* 输入区 */
.chat-input { padding: 12px 20px 16px; border-top: 1px solid var(--border); }
.input-toolbar { display: flex; align-items: center; gap: 12px; margin-bottom: 8px; }
.scope-tip {
  font-size: 12.5px; color: var(--text-secondary);
  max-width: 46%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.model-select { width: 200px; flex-shrink: 0; }
.model-warn { font-size: 13px; color: #e6a23c; flex-shrink: 0; }
.mode-picker { display: flex; align-items: center; gap: 6px; margin-left: auto; }
.mode-label { font-size: 13px; color: var(--text-secondary); }
.mode-select { width: 128px; }
.mode-opt-desc { float: right; color: var(--text-muted); font-size: 12px; margin-left: 12px; }

.mode-tip {
  display: flex; align-items: center; gap: 6px; margin-bottom: 8px;
  padding: 6px 10px; border-radius: 6px; font-size: 12.5px;
}
.tip-danger { color: #c45656; background: rgba(245, 108, 108, 0.1); border: 1px solid rgba(245, 108, 108, 0.3); }
.tip-warning { color: #b88230; background: rgba(230, 162, 60, 0.1); border: 1px solid rgba(230, 162, 60, 0.3); }

/* ---------- HTML 页面下载卡片 ---------- */
.html-export-card {
  display: flex;
  align-items: center;
  gap: 12px;
  max-width: 88%;
  padding: 12px 14px;
  border: 1px solid var(--border, #e4e7ed);
  border-radius: 10px;
  background: var(--bg-subtle, #f7f9fc);
}
.html-export-icon { font-size: 26px; color: #409eff; flex-shrink: 0; }
.html-export-body { flex: 1; min-width: 0; }
.html-export-name {
  font-size: 13.5px; font-weight: 600; color: var(--text);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.html-export-desc { font-size: 12px; color: var(--text-secondary); margin-top: 2px; }
.html-export-actions { display: flex; gap: 8px; flex-shrink: 0; }

/* 流式生成中：占位卡片 */
.html-export-pending .html-export-desc {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: var(--primary);
}

/* 内容被截断的提示 */
.html-export-warn { color: #e6a23c; }
</style>
