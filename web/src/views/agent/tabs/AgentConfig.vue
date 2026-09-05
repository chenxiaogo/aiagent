<template>
  <div v-loading="loading">
    <!-- 基础信息 -->
    <div class="app-card section">
      <div class="card-title">基础信息</div>
      <el-form :model="form" label-width="100px" class="cfg-form">
        <el-form-item label="名称" required>
          <el-input v-model="form.name" placeholder="智能体名称" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="form.description" type="textarea" :rows="2" placeholder="一句话说明这个智能体做什么" />
        </el-form-item>
        <el-form-item label="头像">
          <el-input v-model="form.avatar" placeholder="emoji 或图片 URL" style="max-width: 320px" />
        </el-form-item>
        <el-form-item label="类型">
          <el-select v-model="form.category" style="width: 260px">
            <el-option v-for="opt in categoryOptions" :key="opt.value" :label="opt.label" :value="opt.value">
              <span class="opt-icon">{{ opt.icon }}</span>
              <span>{{ opt.label }}</span>
            </el-option>
          </el-select>
          <span class="tip">类型决定工作台形态与默认暴露的工具</span>
        </el-form-item>
        <el-form-item label="标签">
          <el-input v-model="form.tags" placeholder="逗号分隔，如：视频,监控" />
        </el-form-item>
        <el-form-item label="对外路径">
          <el-input :model-value="agent.slug || '未生成'" disabled style="max-width: 320px" />
          <span class="tip">客户通过它访问 MCP 端点，创建后自动生成</span>
        </el-form-item>
        <el-form-item label="可见性">
          <el-radio-group v-model="form.visibility">
            <el-radio value="private">仅授权客户</el-radio>
            <el-radio value="org">登录用户</el-radio>
            <el-radio value="public">公开</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="运行时">
          <el-select v-model="form.runtimeType" style="width:260px">
            <el-option label="Eino ADK V2（原生工具调用）" value="eino_v2" />
            <el-option label="兼容运行时（JSON 工具协议）" value="legacy" />
          </el-select>
          <span class="tip">推荐 Eino ADK V2；兼容运行时仅用于回退</span>
        </el-form-item>
        <el-form-item label="最大步骤">
          <el-input-number v-model="form.maxSteps" :min="1" :max="30" />
          <span class="tip">限制模型与工具循环次数，防止失控调用</span>
        </el-form-item>
        <el-form-item label="会话记忆">
          <el-switch v-model="form.memoryEnabled" />
          <span class="tip">启用会话摘要、长期偏好与向量记忆召回</span>
        </el-form-item>
        <!-- 记忆细粒度参数：只在启用会话记忆时才需要配置 -->
        <template v-if="form.memoryEnabled">
          <el-form-item label="摘要触发条数">
            <el-input-number v-model="form.memory.summaryThreshold" :min="2" :max="50" />
            <span class="tip">累积多少条消息后生成一次会话摘要；调小可更快看到记忆效果</span>
          </el-form-item>
          <el-form-item label="保留最近消息">
            <el-input-number v-model="form.memory.recentTail" :min="0" :max="20" />
            <span class="tip">摘要时保留不压缩的最新消息数，保证近期对话原样可见</span>
          </el-form-item>
          <el-form-item label="上下文历史条数">
            <el-input-number v-model="form.memory.historyLimit" :min="2" :max="50" />
            <span class="tip">注入模型上下文的原始历史消息条数</span>
          </el-form-item>
          <el-form-item label="长期记忆抽取">
            <el-switch v-model="form.memory.longTermAlways" />
            <span class="tip">开启后每条消息都尝试抽取长期记忆（由模型判断是否有价值）；关闭则只在用户说「请记住 / 我喜欢」等表述时抽取，可省一次模型调用</span>
          </el-form-item>
        </template>
        <el-form-item label="系统提示词">
          <div class="prompt-field">
            <div class="prompt-toolbar">
              <el-dropdown @command="onImportCommand">
                <el-button size="small" :icon="Download">导入</el-button>
                <template #dropdown>
                  <el-dropdown-menu>
                    <el-dropdown-item command="library">从提示词库导入</el-dropdown-item>
                    <el-dropdown-item command="file">从本地文件导入（.md / .txt）</el-dropdown-item>
                  </el-dropdown-menu>
                </template>
              </el-dropdown>
              <el-button
                v-if="form.prompt"
                size="small"
                :icon="DocumentCopy"
                title="复制当前提示词"
                @click="copyPrompt"
              >复制</el-button>
              <span class="tip">{{ (form.prompt || '').length }} 字</span>
            </div>
            <el-input
              v-model="form.prompt"
              type="textarea"
              :rows="8"
              placeholder="定义角色、能力边界与回答风格；可从提示词库或本地文件导入"
            />
          </div>
        </el-form-item>
      </el-form>
    </div>

    <!-- MCP 是「本智能体去连接谁」，对外凭据在「发布与交付」页 -->
    <div class="app-card section">
      <McpPane :agent-id="agentId" />
      <p class="pane-tip">
        MCP 配置的是本智能体去调用的外部服务；给外部客户的访问凭据、接入端点与示例代码在「发布与交付」页管理。
      </p>
    </div>

    <div class="app-card section">
      <ToolCatalog :agent-id="agentId" :editable="true" />
    </div>

    <div class="action-bar">
      <el-button type="primary" :loading="saving" @click="handleSave">保存基础信息</el-button>
      <span class="tip">保存后进入草稿状态，需点右上角「发布新版本」才会对线上生效</span>
    </div>

    <!-- 从提示词库导入系统提示词 -->
    <el-dialog v-model="libraryVisible" title="从提示词库导入" width="780px">
      <div class="lib-toolbar">
        <el-input
          v-model="libKeyword"
          placeholder="搜索名称 / 描述 / 内容"
          clearable
          size="small"
          style="width: 240px"
          @input="loadLibrary"
        />
        <el-select
          v-model="libType"
          placeholder="全部类型"
          clearable
          size="small"
          style="width: 180px"
          @change="loadLibrary"
        >
          <el-option v-for="t in PROMPT_TYPES" :key="t.value" :label="t.label" :value="t.value" />
        </el-select>
        <div class="lib-actions">
          <el-button size="small" :icon="Check" :disabled="!libList.length" @click="toggleSelectAllLib">
            {{ libAllSelected ? '取消全选' : '全选' }}
          </el-button>
        </div>
      </div>
      <el-table
        ref="libTableRef"
        :data="libList"
        v-loading="libLoading"
        size="small"
        max-height="360"
        style="width: 100%"
        @selection-change="onLibSelectionChange"
      >
        <el-table-column type="selection" width="46" />
        <el-table-column prop="name" label="名称" min-width="140" />
        <el-table-column label="类型" width="110">
          <template #default="{ row }">{{ promptTypeText(row.promptType) }}</template>
        </el-table-column>
        <el-table-column prop="description" label="描述" min-width="150" show-overflow-tooltip />
        <el-table-column prop="systemPrompt" label="内容预览" min-width="190" show-overflow-tooltip />
      </el-table>
      <el-empty v-if="!libLoading && !libList.length" description="提示词库暂无数据" :image-size="60" />
      <template #footer>
        <span class="lib-hint">
          已选 <b>{{ libSelection.length }}</b> 条，按表格顺序拼接
        </span>
        <el-button @click="libraryVisible = false">取消</el-button>
        <el-button :disabled="!libSelection.length" @click="insertPrompt('append')">
          追加到末尾{{ libSelection.length > 1 ? `（${libSelection.length}）` : '' }}
        </el-button>
        <el-button type="primary" :disabled="!libSelection.length" @click="insertPrompt('replace')">
          替换当前内容
        </el-button>
      </template>
    </el-dialog>

    <!-- 本地文件导入：隐藏输入框，由下拉菜单触发 -->
    <input
      ref="fileInputRef"
      type="file"
      accept=".md,.txt,.text,text/plain,text/markdown"
      class="hidden-file"
      @change="onFileChange"
    />
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Download, DocumentCopy, Check } from '@element-plus/icons-vue'
import { getAgent, updateAgent } from '@/api/agent'
import { getPromptConfigList } from '@/api/promptConfig'
import { agentTypeOptions } from '@/constants/agentTypes'
import { PROMPT_TYPES, promptTypeText } from '@/constants/promptTypes'
import McpPane from '@/components/agent/McpPane.vue'
import ToolCatalog from '@/components/agent/ToolCatalog.vue'

const route = useRoute()
const agentId = computed(() => Number(route.params.id))
// 保存成功后通知详情页刷新「未发布改动」状态
const emit = defineEmits(['saved'])

const loading = ref(false)
const saving = ref(false)
const agent = ref({})
const categoryOptions = agentTypeOptions()

const form = reactive({
  name: '', description: '', avatar: '', category: 'general',
  tags: '', prompt: '', visibility: 'private',
  runtimeType: 'eino_v2', maxSteps: 8, memoryEnabled: true,
  // 记忆细粒度参数（后端以 memoryParams JSON 字符串存储）
  memory: { summaryThreshold: 6, recentTail: 4, historyLimit: 12, longTermAlways: true }
})

// 解析智能体级记忆参数：缺省字段用内置默认值补齐
function parseMemoryParams(raw) {
  const defaults = { summaryThreshold: 6, recentTail: 4, historyLimit: 12, longTermAlways: true }
  if (!raw) return defaults
  try {
    const parsed = typeof raw === 'string' ? JSON.parse(raw) : raw
    return { ...defaults, ...(parsed || {}) }
  } catch (e) {
    return defaults
  }
}

async function load() {
  loading.value = true
  try {
    const res = await getAgent(agentId.value)
    if (res.code === 0) {
      agent.value = res.data || {}
      Object.assign(form, {
        name: agent.value.name || '',
        description: agent.value.description || '',
        avatar: agent.value.avatar || '',
        category: agent.value.category || 'general',
        tags: agent.value.tags || '',
        prompt: agent.value.prompt || '',
        visibility: agent.value.visibility || 'private',
        runtimeType: agent.value.runtimeType || 'eino_v2',
        maxSteps: agent.value.maxSteps || 8,
        memoryEnabled: agent.value.memoryEnabled !== false
      })
      Object.assign(form.memory, parseMemoryParams(agent.value.memoryParams))
    }
  } finally {
    loading.value = false
  }
}

/* ---------- 系统提示词导入 ---------- */
// 提示词库（平台级资产，只读引用）+ 本地文件两种来源，导入的都是快照，后续库里改动不回传
const libraryVisible = ref(false)
const libLoading = ref(false)
const libList = ref([])
const libKeyword = ref('')
const libType = ref('')
const libTableRef = ref(null)
const libSelection = ref([])
const fileInputRef = ref(null)

// 全选状态：用于把按钮在「全选 / 取消全选」之间切换
const libAllSelected = computed(
  () => libList.value.length > 0 && libSelection.value.length === libList.value.length
)

async function loadLibrary() {
  libLoading.value = true
  try {
    // 后端只支持按类型过滤，关键字在前端过滤（库里条目量小）
    const res = await getPromptConfigList({ agentId: agentId.value, promptType: libType.value })
    const list = res.code === 0 ? (res.data || []) : []
    const kw = libKeyword.value.trim().toLowerCase()
    libList.value = kw
      ? list.filter(p =>
        [p.name, p.description, p.systemPrompt]
          .filter(Boolean)
          .some(v => String(v).toLowerCase().includes(kw)))
      : list
  } catch (e) {
    ElMessage.error('加载提示词库失败')
  } finally {
    libLoading.value = false
  }
}

function openLibrary() {
  libSelection.value = []
  libKeyword.value = ''
  libType.value = ''
  libraryVisible.value = true
  loadLibrary()
}

function onLibSelectionChange(rows) {
  libSelection.value = rows || []
}

// 全选：勾选当前列表全部条目；已全选时再点一次则清空
function toggleSelectAllLib() {
  const table = libTableRef.value
  if (!table) return
  const clear = libAllSelected.value
  libList.value.forEach(row => table.toggleRowSelection(row, !clear))
}

function onImportCommand(cmd) {
  if (cmd === 'library') openLibrary()
  else if (cmd === 'file') fileInputRef.value?.click()
}

// mode: append 追加到末尾 / replace 替换当前内容
// 支持一次导入多条：按表格里的顺序拼接，每条之间空一行分隔。
function insertPrompt(mode) {
  const parts = libSelection.value
    .map(p => String(p.systemPrompt || '').trim())
    .filter(Boolean)
  if (!parts.length) return

  const merged = parts.join('\n\n')
  const current = (form.prompt || '').trim()
  form.prompt = mode === 'replace' || !current ? merged : `${current}\n\n${merged}`

  libraryVisible.value = false
  if (mode === 'replace') {
    ElMessage.success(parts.length > 1 ? `已用 ${parts.length} 条提示词替换` : '已替换系统提示词')
  } else {
    ElMessage.success(parts.length > 1 ? `已追加 ${parts.length} 条提示词` : '已追加到系统提示词')
  }
}

// 本地文件：读纯文本后追加（内容为空则直接写入）
function onFileChange(ev) {
  const file = ev.target.files?.[0]
  ev.target.value = '' // 允许连续选同一个文件
  if (!file) return
  const reader = new FileReader()
  reader.onload = () => {
    const text = String(reader.result || '')
    if (!text.trim()) return ElMessage.warning('文件内容为空')
    const current = (form.prompt || '').trim()
    form.prompt = current ? `${current}\n\n${text.trim()}` : text.trim()
    ElMessage.success('已从文件导入')
  }
  reader.onerror = () => ElMessage.error('读取文件失败')
  reader.readAsText(file)
}

async function copyPrompt() {
  const text = form.prompt || ''
  if (!text) return
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success('已复制到剪贴板')
  } catch (e) {
    ElMessage.warning('复制失败，请手动选择复制')
  }
}

async function handleSave() {
  if (!form.name) return ElMessage.warning('请填写智能体名称')
  saving.value = true
  try {
    // memory 是前端的结构化表单，后端以 JSON 字符串接收
    const { memory, ...rest } = form
    const res = await updateAgent(agentId.value, {
      ...rest,
      memoryParams: JSON.stringify(memory)
    })
    if (res.code !== 0) {
      ElMessage.error(res.message || '保存失败')
      return
    }
    ElMessage.success('已保存为草稿，发布后生效')
    load()
    emit('saved')
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.section { padding: 20px 24px; margin-bottom: 16px; }
.card-title { font-size: 15px; font-weight: 600; color: var(--text); margin-bottom: 14px; }
.cfg-form { max-width: 760px; }
.tip { font-size: 12px; color: var(--text-muted); margin-left: 12px; }
.opt-icon { margin-right: 6px; }
.action-bar { display: flex; align-items: center; }

/* ---------- 系统提示词导入 ---------- */
.prompt-field { width: 100%; }
.prompt-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}
.prompt-toolbar .tip { margin-left: auto; }
.lib-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
}
.lib-actions { margin-left: auto; display: flex; gap: 8px; }
.lib-hint { margin-right: 12px; font-size: 12px; color: var(--text-secondary); }
.lib-toolbar .tip { margin-left: auto; }
/* 文件选择框不占版面，由「导入」下拉菜单触发点击 */
.hidden-file { display: none; }

.pane-tip {
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px dashed var(--border-light);
  font-size: 12px;
  line-height: 1.6;
  color: var(--text-muted);
}
</style>
