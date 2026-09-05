<template>
  <div class="agent-page">
    <div class="page-header">
      <h2>智能体管理</h2>
      <el-button type="primary" :icon="Plus" @click="handleCreate">新建智能体</el-button>
    </div>

    <!-- 筛选栏 -->
    <div class="app-card filter-bar">
      <el-form :inline="true" :model="filter">
        <el-form-item label="状态">
          <el-select v-model="filter.status" placeholder="全部" clearable style="width: 140px" @change="loadList">
            <el-option label="草稿" value="draft" />
            <el-option label="已发布" value="published" />
            <el-option label="已下线" value="offline" />
          </el-select>
        </el-form-item>
        <el-form-item label="搜索">
          <el-input v-model="filter.keyword" placeholder="名称/描述" clearable style="width: 220px" @keyup.enter="loadList" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :icon="Search" @click="loadList">搜索</el-button>
        </el-form-item>
      </el-form>
      <div class="filter-tip">按住卡片左侧手柄可拖动排序，排序结果自动保存</div>
    </div>

    <!-- 智能体列表卡片 -->
    <div
      class="agent-grid"
      :class="{ 'is-sorting': dragIndex >= 0 }"
      @dragover.prevent
      @drop.prevent="onDropOutside"
    >
      <div
        v-for="(agent, index) in list"
        :key="agent.id"
        class="agent-card"
        :class="[
          `agent-card--${categoryType(agent.category)}`,
          { 'agent-card--draft': agent.status !== 'published',
            'is-dragging': dragIndex === index,
            'is-drop-target': dragOverIndex === index && dragIndex !== index }
        ]"
        @click="onCardClick(agent)"
        @dragover.prevent="onDragOver(index, $event)"
        @dragleave="onDragLeave($event)"
        @drop.stop.prevent="onDrop(index, $event)"
      >
        <!-- 手柄自身常驻 draggable：不依赖按下的时序，拖起来更稳 -->
        <div
          class="drag-handle"
          draggable="true"
          title="按住拖动排序"
          @dragstart.stop="onDragStart(index, $event)"
          @dragend="onDragEnd"
          @click.stop
        >
          <el-icon><Rank /></el-icon>
        </div>
        <div class="agent-card-accent" />
        <div class="agent-card-body">
          <div class="agent-card-head">
            <div class="agent-avatar ds-icon-badge ds-icon-badge--lg">
              {{ agent.avatar || categoryIcon(agent.category) }}
            </div>
            <div class="agent-info">
              <div class="agent-name">{{ agent.name }}</div>
              <div class="agent-desc">{{ agent.description || '暂无描述' }}</div>
            </div>
            <el-tag :type="statusType(agent.status)" size="small" effect="light" class="status-tag">
              {{ statusText(agent.status) }}
            </el-tag>
          </div>

          <div class="agent-type-chip" :class="`type-tag--${categoryType(agent.category)}`">
            {{ categoryIcon(agent.category) }} {{ categoryName(agent.category) }}
          </div>

          <div class="agent-stats">
            <div class="stat-item" title="已配置的技能数">
              <span class="stat-value">{{ agent.skillCount ?? 0 }}</span>
              <span class="stat-label">技能</span>
            </div>
            <div class="stat-item" title="已启用绑定的模型数">
              <span class="stat-value">{{ agent.modelCount ?? 0 }}</span>
              <span class="stat-label">模型</span>
            </div>
            <div class="stat-item" title="生效的工具数（内置工具 + MCP 远端工具）">
              <span class="stat-value">{{ agent.toolCount ?? 0 }}</span>
              <span class="stat-label">工具</span>
            </div>
          </div>
        </div>

        <div class="agent-card-foot" @click.stop>
          <el-button size="small" type="primary" :icon="Promotion" @click="enterWorkspace(agent)">进入工作台</el-button>
          <div class="foot-ops">
            <el-button size="small" :icon="Setting" circle title="设置" @click="goDetail(agent)" />
            <el-button size="small" :icon="Upload" circle title="版本与发布" @click="goVersions(agent)" />
            <el-dropdown @command="(cmd) => handleStatus(cmd, agent)">
              <el-button size="small" :icon="CaretBottom" circle title="状态" />
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item command="draft">设为草稿</el-dropdown-item>
                  <el-dropdown-item command="published">发布</el-dropdown-item>
                  <el-dropdown-item command="offline">下线</el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
            <el-button size="small" type="danger" :icon="Delete" circle title="删除" @click="handleDelete(agent)" />
          </div>
        </div>
      </div>

      <!-- 新建卡片 -->
      <div class="agent-card agent-card--new" @click="handleCreate">
        <div class="new-plus"><el-icon><Plus /></el-icon></div>
        <div class="new-text">新建智能体</div>
      </div>
    </div>

    <!-- 分页 -->
    <div class="pagination-wrap">
      <el-pagination
        v-model:current-page="filter.page"
        v-model:page-size="filter.pageSize"
        :total="total"
        :page-sizes="[10, 20, 50]"
        layout="total, sizes, prev, pager, next, jumper"
        @size-change="loadList"
        @current-change="loadList"
      />
    </div>

    <!-- 新建智能体：只填简单信息，其余配置在设置页完成 -->
    <el-dialog v-model="dialogVisible" title="新建智能体" width="560px" class="agent-dialog">
      <el-form :model="form" :rules="rules" ref="formRef" label-width="72px">
        <el-form-item label="名称" prop="name">
          <el-input v-model="form.name" placeholder="请输入智能体名称" />
        </el-form-item>
        <el-form-item label="类型" prop="category">
          <el-select v-model="form.category" placeholder="选择智能体类型" style="width: 100%">
            <el-option
              v-for="opt in categoryOptions"
              :key="opt.value"
              :label="opt.label"
              :value="opt.value"
            >
              <span class="opt-icon">{{ opt.icon }}</span>
              <span>{{ opt.label }}</span>
              <span class="opt-desc">{{ opt.desc }}</span>
            </el-option>
          </el-select>
        </el-form-item>
        <el-form-item label="头像">
          <el-input v-model="form.avatar" placeholder="emoji 或图片 URL" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="form.description" type="textarea" :rows="3" placeholder="一句话说明这个智能体做什么" />
        </el-form-item>
      </el-form>

      <template #footer>
        <div class="dialog-footer">
          <span class="footer-tip">创建后到设置页配置模型与技能，再到「版本」页发布</span>
          <div class="footer-actions">
            <el-button @click="dialogVisible = false">取消</el-button>
            <el-button type="primary" :loading="submitting" @click="handleSubmit">创建</el-button>
          </div>
        </div>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Search, Delete, CaretBottom, Promotion, Setting, Upload, Rank } from '@element-plus/icons-vue'
import {
  getAgentList, createAgent, deleteAgent, updateAgentStatus, reorderAgents
} from '@/api/agent'
import { agentTypeOptions, getAgentType } from '@/constants/agentTypes'

const router = useRouter()

// 智能体类型统一由注册表提供（新增类型只需改 constants/agentTypes.js）
const categoryOptions = agentTypeOptions()

function categoryIcon(c) { return getAgentType(c).icon }
function categoryName(c) { return getAgentType(c).label }
function categoryType(c) { return getAgentType(c).color }

// 进入智能体工作台（新标签页打开，一个智能体一个独立平台）
function enterWorkspace(agent) {
  const url = router.resolve(`/agent/${agent.id}`).href
  window.open(url, '_blank')
}

// 进入智能体设置页（基础配置 / 模型 / 技能 / MCP / API Key 都在这里）
function goDetail(agent) {
  router.push(`/agents/${agent.id}/config`)
}

// 直接进入版本与发布
function goVersions(agent) {
  router.push(`/agents/${agent.id}/versions`)
}

const list = ref([])
const total = ref(0)
const filter = reactive({
  status: '',
  keyword: '',
  page: 1,
  pageSize: 20
})

const dialogVisible = ref(false)
const formRef = ref(null)
// 新建只收集简单信息，其余（提示词、模型、技能、发布）都在设置页完成
const form = reactive({
  name: '',
  description: '',
  avatar: '',
  category: 'general'
})
const rules = {
  name: [{ required: true, message: '请输入名称', trigger: 'blur' }],
  category: [{ required: true, message: '请选择类型', trigger: 'change' }]
}

const submitting = ref(false)

async function loadList() {
  try {
    const res = await getAgentList(filter)
    if (res.code === 0) {
      list.value = res.data.list
      total.value = res.data.total
    }
  } catch (e) {
    ElMessage.error('加载失败')
  }
}

/* ---------- 拖拽排序 ---------- */
// 只有手柄是 draggable，卡片本身不可拖，避免影响卡片内的按钮与点击
const dragIndex = ref(-1)        // 正在拖动的卡片下标
const dragOverIndex = ref(-1)    // 当前悬停的目标下标
const dragging = ref(false)      // 本次交互确实发生了拖动（用于抑制误触点击）

function resetDrag() {
  dragIndex.value = -1
  dragOverIndex.value = -1
}

function onDragStart(index, ev) {
  dragIndex.value = index
  dragging.value = true
  const card = ev.currentTarget?.closest?.('.agent-card')
  if (ev.dataTransfer) {
    ev.dataTransfer.effectAllowed = 'move'
    // 必须写数据，否则部分浏览器（Firefox）不会触发 drop
    ev.dataTransfer.setData('text/plain', String(index))
    // 拖动影像用整张卡片，而不是那根细手柄
    if (card) ev.dataTransfer.setDragImage(card, 24, 24)
  }
}

function onDragOver(index, ev) {
  if (dragIndex.value < 0) return
  if (ev.dataTransfer) ev.dataTransfer.dropEffect = 'move'
  dragOverIndex.value = index
}

function onDragLeave(ev) {
  // 在卡片内部子元素之间移动也会触发 dragleave，需排除，否则高亮会闪烁
  const card = ev.currentTarget
  if (ev.relatedTarget && card.contains(ev.relatedTarget)) return
  dragOverIndex.value = -1
}

async function onDrop(index) {
  const from = dragIndex.value
  resetDrag()
  if (from < 0 || from === index) return
  const next = list.value.slice()
  const [moved] = next.splice(from, 1)
  next.splice(index, 0, moved)
  list.value = next
  await persistOrder(next)
}

// 拖到卡片间隙 / 新建卡片等空白处：仅复位，不改变顺序
function onDropOutside() {
  resetDrag()
}

function onDragEnd() {
  resetDrag()
  // 拖动结束后浏览器可能补发 click，短暂屏蔽避免误进工作台
  if (dragging.value) {
    setTimeout(() => { dragging.value = false }, 200)
  }
}

function onCardClick(agent) {
  if (dragging.value) return
  enterWorkspace(agent)
}

async function persistOrder(ordered) {
  const ids = ordered.map(a => a.id)
  try {
    await reorderAgents(ids, (filter.page - 1) * filter.pageSize)
  } catch (e) {
    ElMessage.error('排序保存失败')
    loadList()
  }
}

function handleCreate() {
  Object.assign(form, { name: '', description: '', avatar: '', category: 'general' })
  dialogVisible.value = true
}

async function handleSubmit() {
  await formRef.value.validate()
  submitting.value = true
  try {
    const res = await createAgent(form)
    const agentId = res.data?.id || null
    dialogVisible.value = false
    ElMessage.success('创建成功')
    await loadList()
    if (agentId) {
      // 新建的智能体还是空壳，引导去设置页把模型、技能配好再发布
      try {
        await ElMessageBox.confirm('是否立即进入设置页配置模型、技能并发布？', '创建成功', {
          confirmButtonText: '去设置',
          cancelButtonText: '稍后',
          type: 'success'
        })
        router.push(`/agents/${agentId}/config`)
      } catch (e) {
        // 用户选择「稍后」，留在列表
      }
    }
  } catch (e) {
    ElMessage.error('创建失败')
  } finally {
    submitting.value = false
  }
}

async function handleDelete(agent) {
  await ElMessageBox.confirm(`确定删除智能体「${agent.name}」？`, '提示', { type: 'warning' })
  try {
    await deleteAgent(agent.id)
    ElMessage.success('删除成功')
    loadList()
  } catch (e) {
    ElMessage.error('删除失败')
  }
}

async function handleStatus(cmd, agent) {
  try {
    await updateAgentStatus(agent.id, cmd)
    ElMessage.success('状态更新成功')
    loadList()
  } catch (e) {
    ElMessage.error('操作失败')
  }
}

function statusText(s) {
  return { draft: '草稿', published: '已发布', offline: '已下线' }[s] || s
}
function statusType(s) {
  return { draft: 'info', published: 'success', offline: 'danger' }[s] || 'info'
}

onMounted(loadList)
</script>

<style scoped>
.filter-bar {
  padding: 16px 24px;
}

.filter-tip {
  margin-top: -6px;
  font-size: 12px;
  color: var(--text-muted);
}

.agent-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: 18px;
}

/* 卡片：白底 + 顶部色条 + 悬浮光晕 */
.agent-card {
  position: relative;
  display: flex;
  flex-direction: column;
  background: var(--card-bg);
  border: 1px solid var(--border-light);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-sm);
  overflow: hidden;
  cursor: pointer;
  transition: transform var(--duration) var(--ease), box-shadow var(--duration) var(--ease), border-color var(--duration) var(--ease);
}

.agent-card:hover {
  transform: translateY(-4px);
  box-shadow: var(--shadow-glow);
  border-color: transparent;
}

/* 顶部类型色条 */
.agent-card-accent {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  height: 4px;
  background: var(--primary-gradient);
  opacity: 0.9;
}
.agent-card--warning .agent-card-accent { background: linear-gradient(135deg, #f1b44c, #f6d365); }
.agent-card--success .agent-card-accent { background: linear-gradient(135deg, #34c38f, #4ade80); }
.agent-card--info .agent-card-accent { background: linear-gradient(135deg, #50a5f1, #7cc4ff); }

.agent-card--draft { opacity: 0.92; }

/* ---------- 拖拽排序 ---------- */
/* 左侧竖向手柄：默认淡显，悬浮卡片时凸显，按住即可拖动 */
.drag-handle {
  position: absolute;
  left: 0;
  top: 0;
  bottom: 0;
  width: 20px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 14px;
  color: var(--text-muted);
  background: var(--bg-subtle);
  border-right: 1px solid transparent;
  opacity: 0.45;
  cursor: grab;
  z-index: 2;
  user-select: none;
  -webkit-user-select: none;
  transition: opacity var(--duration) var(--ease), color var(--duration) var(--ease);
}
/* 图标不参与命中，保证按下的一定是手柄本身，拖拽才稳定 */
.drag-handle * { pointer-events: none; }
.agent-card:hover .drag-handle { opacity: 1; color: var(--primary); }
.drag-handle:active { cursor: grabbing; }

/* 拖动进行中：关掉 hover 位移，避免位置跳动干扰投放 */
.agent-grid.is-sorting .agent-card:hover {
  transform: none;
  box-shadow: var(--shadow-sm);
  border-color: var(--border-light);
}
.agent-grid.is-sorting,
.agent-grid.is-sorting * { cursor: grabbing; }

/* 拖动中：源卡片淡出，目标位置高亮描边 */
.agent-card.is-dragging {
  opacity: 0.45;
  transform: scale(0.98);
  box-shadow: var(--shadow-md);
}
.agent-card.is-drop-target {
  border-color: var(--primary);
  box-shadow: var(--shadow-glow);
}
.agent-card.is-drop-target::after {
  content: '';
  position: absolute;
  left: 0;
  top: 0;
  bottom: 0;
  width: 3px;
  background: var(--primary-gradient);
}

.agent-card-body { padding: 18px 18px 8px; flex: 1; }

.agent-card-head {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  margin-bottom: 14px;
}

.agent-info { flex: 1; min-width: 0; }

.agent-name {
  font-size: 16px;
  font-weight: 600;
  color: var(--text);
  margin-bottom: 4px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.agent-desc {
  font-size: 13px;
  color: var(--text-secondary);
  line-height: 1.5;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  min-height: 38px;
}

.status-tag { flex-shrink: 0; }

.agent-type-chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 3px 10px;
  border-radius: var(--radius-full);
  font-size: 12px;
  font-weight: 500;
  margin-bottom: 14px;
}
.type-tag--primary { background: var(--primary-soft); color: var(--primary); }
.type-tag--warning { background: var(--warning-soft); color: #b8860b; }
.type-tag--success { background: var(--success-soft); color: #1f9d6b; }
.type-tag--info { background: var(--info-soft); color: #2f7fc4; }

.agent-stats {
  display: flex;
  padding: 12px 0;
  border-top: 1px solid var(--border-light);
}

.stat-item { flex: 1; text-align: center; }
.stat-item + .stat-item { border-left: 1px solid var(--border-light); }

.stat-value {
  display: block;
  font-size: 18px;
  font-weight: 700;
  background: var(--primary-gradient);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
}

.stat-label { font-size: 12px; color: var(--text-muted); margin-top: 2px; }

/* 底部操作栏 */
.agent-card-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 12px 18px;
  border-top: 1px solid var(--border-light);
  background: var(--bg-subtle);
}

.foot-ops { display: flex; gap: 6px; }

/* 新建卡片 */
.agent-card--new {
  align-items: center;
  justify-content: center;
  gap: 12px;
  border-style: dashed;
  border-color: var(--border);
  color: var(--text-muted);
  min-height: 220px;
  transition: all var(--duration) var(--ease);
}
.agent-card--new:hover {
  border-color: var(--primary);
  color: var(--primary);
  box-shadow: var(--shadow-md);
}
.new-plus {
  width: 56px;
  height: 56px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 28px;
  background: var(--primary-soft);
}
.new-text { font-size: 14px; font-weight: 500; }

/* ---------- 新建智能体弹窗 ---------- */
.dialog-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}
.footer-tip { font-size: 12px; color: var(--text-muted); }
.footer-actions { display: flex; gap: 8px; flex-shrink: 0; }

.opt-icon { margin-right: 6px; }
.opt-desc { float: right; color: var(--text-secondary); font-size: 12px; }

.pagination-wrap {
  display: flex;
  justify-content: center;
  margin-top: 24px;
}

</style>
