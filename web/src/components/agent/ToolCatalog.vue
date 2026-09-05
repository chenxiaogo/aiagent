<template>
  <div v-loading="loading">
    <div class="section-head">
      <div>
        <div class="card-title">有效工具</div>
        <div class="tip">
          内置工具来自<a href="#/market/tools" class="link">能力市场·工具库</a>，
          MCP 工具在「MCP 服务」Tab 管理。
        </div>
      </div>
      <div class="head-actions" v-if="editable">
        <span class="mount-count">已挂载 {{ mountedCount }} / {{ builtinCount }}</span>
        <el-button type="primary" :disabled="!dirty" @click="handleSave">保存挂载</el-button>
        <el-button @click="load">刷新</el-button>
      </div>
      <div v-else class="head-actions">
        <el-button @click="load">刷新</el-button>
      </div>
    </div>

    <div v-if="editable && hasBuiltin" class="hint-bar">
      <el-icon><InfoFilled /></el-icon>
      <span>按分组勾选需要挂载的内置工具，未勾选的不会出现在智能体运行时。留空=全部启用（默认）。</span>
    </div>

    <!-- 分组浏览：工具按工具库里的分组归类，可整组勾选 -->
    <div class="group-list">
      <div v-for="g in groups" :key="g.key" class="tool-group">
        <div class="group-head" @click="toggleGroup(g.key)">
          <span class="group-caret">
            <el-icon>
              <CaretRight v-if="collapsed[g.key]" />
              <CaretBottom v-else />
            </el-icon>
          </span>
          <span class="group-icon">{{ g.icon }}</span>
          <span class="group-name">{{ g.label }}</span>
          <span class="group-count">
            <template v-if="g.mcp">MCP 远端</template>
            <template v-else>{{ g.selected }} / {{ g.items.length }}</template>
          </span>
          <div class="group-actions" @click.stop>
            <el-button v-if="editable && !g.mcp" link size="small" @click="selectGroup(g, true)">全选</el-button>
            <el-button v-if="editable && !g.mcp" link size="small" @click="selectGroup(g, false)">清空</el-button>
          </div>
        </div>

        <div v-show="!collapsed[g.key]" class="group-body">
          <div v-for="row in g.items" :key="row.id" class="tool-item">
            <el-checkbox
              v-if="editable && !g.mcp"
              v-model="row._enabled"
              class="tool-check"
            />
            <span v-else-if="!g.mcp" class="tool-check tool-check--static">
              <el-icon><Select /></el-icon>
            </span>
            <div class="tool-main">
              <div class="tool-name">
                {{ row.name }}
                <el-tag v-if="g.mcp" size="small" type="warning">MCP</el-tag>
              </div>
              <div class="tool-desc">{{ row.description || '—' }}</div>
            </div>
            <div class="tool-tags">
              <el-tag v-if="row.readOnly" size="small" type="success">只读</el-tag>
              <el-tag v-if="row.sideEffect" size="small" type="danger">副作用</el-tag>
              <el-tag v-if="row.approvalRequired" size="small" type="warning">需审批</el-tag>
            </div>
            <div class="tool-res">{{ (row.resourceTypes || []).join('、') || '-' }}</div>
          </div>
        </div>
      </div>

      <el-empty v-if="!loading && tools.length === 0" description="暂无可用工具" :image-size="60" />
    </div>
  </div>
</template>

<script setup>
import { ref, computed, reactive, watch } from 'vue'
import { InfoFilled, CaretRight, CaretBottom, Select } from '@element-plus/icons-vue'
import { getAgentTools, saveToolLibMounts } from '@/api/agent'
import { ElMessage } from 'element-plus'
import { TOOL_CATEGORIES, toolCategoryMeta } from '@/constants/toolCategories'

const props = defineProps({
  agentId: { type: Number, required: true },
  editable: { type: Boolean, default: false },
})

const MCP_KEY = '__mcp__'

const loading = ref(false)
const saving = ref(false)
const tools = ref([])
// 初始挂载的工具库 ID 列表，null 表示未配置（全部启用）
const initialToolIds = ref(null)
// 分组折叠状态：默认全展开
const collapsed = reactive({})

const hasBuiltin = computed(() => tools.value.some(t => t.source === 'builtin'))
const builtinCount = computed(() => tools.value.filter(t => t.source === 'builtin').length)
const mountedCount = computed(() =>
  tools.value.filter(t => t.source === 'builtin' && t._enabled).length
)

// 按分组归类：内置工具按工具库分组，MCP 工具单独一组排在最后
const groups = computed(() => {
  const order = TOOL_CATEGORIES.map(c => c.value)
  const buckets = new Map()

  for (const t of tools.value) {
    const key = t.source === 'mcp' ? MCP_KEY : (t.category || '其他')
    if (!buckets.has(key)) {
      const meta = key === MCP_KEY
        ? { icon: '🔌', label: 'MCP 服务' }
        : toolCategoryMeta(key)
      buckets.set(key, { key, icon: meta.icon, label: meta.label, items: [], mcp: key === MCP_KEY })
    }
    buckets.get(key).items.push(t)
  }

  const list = [...buckets.values()]
  list.forEach(g => {
    g.items.sort((a, b) => String(a.name).localeCompare(String(b.name)))
    g.selected = g.items.filter(t => t._enabled).length
  })
  // 预设分组按注册表顺序，自定义分组紧随其后，MCP 永远在最后
  list.sort((a, b) => {
    if (a.mcp !== b.mcp) return a.mcp ? 1 : -1
    const ia = order.indexOf(a.key)
    const ib = order.indexOf(b.key)
    if (ia === -1 && ib === -1) return String(a.key).localeCompare(String(b.key))
    if (ia === -1) return 1
    if (ib === -1) return -1
    return ia - ib
  })
  return list
})

const dirty = computed(() => {
  if (initialToolIds.value === null) {
    // 初始为全部启用：只要不是全部勾选就是 dirty
    const builtin = tools.value.filter(t => t.source === 'builtin')
    return !builtin.every(t => t._enabled)
  }
  const current = tools.value.filter(t => t.source === 'builtin' && t._enabled).map(t => t.id)
  if (current.length !== initialToolIds.value.length) return true
  const set = new Set(initialToolIds.value)
  return current.some(id => !set.has(id))
})

function toggleGroup(key) {
  collapsed[key] = !collapsed[key]
}

function selectGroup(group, value) {
  group.items.forEach(t => {
    if (t.source === 'builtin') t._enabled = value
  })
}

async function load() {
  if (!props.agentId) return
  loading.value = true
  try {
    const res = await getAgentTools(props.agentId)
    if (res.code === 0) {
      const list = res.data || []
      list.forEach(t => { t._enabled = t.enabled !== false })
      tools.value = list
      // 初始状态：如果全部启用则记为 null（未配置），否则记为 ID 列表
      const builtin = list.filter(t => t.source === 'builtin')
      const allEnabled = builtin.every(t => t._enabled)
      initialToolIds.value = allEnabled ? null : builtin.filter(t => t._enabled).map(t => t.id)
      // 新出现的分组默认展开
      tools.value.forEach(t => {
        const key = t.source === 'mcp' ? MCP_KEY : (t.category || '其他')
        if (collapsed[key] === undefined) collapsed[key] = false
      })
    }
  } finally { loading.value = false }
}

async function handleSave() {
  if (!props.agentId) return
  const builtin = tools.value.filter(t => t.source === 'builtin')
  const allEnabled = builtin.every(t => t._enabled)
  const ids = allEnabled ? null : builtin.filter(t => t._enabled).map(t => t.id)

  saving.value = true
  try {
    const res = await saveToolLibMounts(props.agentId, ids)
    if (res.code !== 0) {
      ElMessage.error(res.message || '保存失败')
      return
    }
    ElMessage.success('已保存')
    initialToolIds.value = ids
    load()
  } finally { saving.value = false }
}

watch(() => props.agentId, load, { immediate: true })
defineExpose({ load })
</script>

<style scoped>
.section-head { display:flex; align-items:flex-start; justify-content:space-between; gap:16px; margin-bottom:12px; }
.card-title { font-size:15px; font-weight:600; color:var(--text); }
.tip { margin-top:4px; font-size:12px; color:var(--text-muted); }
.tip .link { color: var(--primary); text-decoration: none; }
.head-actions { display:flex; align-items:center; gap:8px; }
.mount-count { font-size:12px; color:var(--text-secondary); }
.hint-bar {
  display:flex; align-items:center; gap:8px;
  padding:8px 12px; margin-bottom:12px;
  background:var(--bg-soft); border-radius:6px;
  font-size:12px; color:var(--text-muted);
}
.hint-bar .el-icon { color:var(--primary); }

/* ---------- 分组 ---------- */
.group-list { display:flex; flex-direction:column; gap:10px; }

.tool-group {
  border:1px solid var(--border-light);
  border-radius:8px;
  overflow:hidden;
  background:var(--card-bg);
}

.group-head {
  display:flex; align-items:center; gap:8px;
  padding:9px 12px;
  background:var(--bg-subtle);
  cursor:pointer;
  user-select:none;
}
.group-caret { display:flex; color:var(--text-muted); }
.group-icon { font-size:14px; }
.group-name { font-size:13px; font-weight:600; color:var(--text); }
.group-count {
  font-size:12px; color:var(--text-muted);
  padding:1px 8px; border-radius:10px; background:var(--card-bg);
}
.group-actions { margin-left:auto; display:flex; gap:2px; }

.group-body { padding:4px 0; }
.tool-item {
  display:flex; align-items:flex-start; gap:10px;
  padding:8px 14px;
  border-top:1px solid var(--border-light);
}
.tool-item:first-child { border-top:none; }

.tool-check { margin-top:2px; height:auto; }
.tool-check--static { color:var(--text-muted); font-size:14px; line-height:22px; }

.tool-main { flex:1; min-width:0; }
.tool-name { font-size:13px; font-weight:500; color:var(--text); display:flex; align-items:center; gap:6px; }
.tool-desc { font-size:12px; color:var(--text-muted); margin-top:2px; line-height:1.5; }

.tool-tags { display:flex; gap:4px; flex-shrink:0; max-width:220px; flex-wrap:wrap; justify-content:flex-end; }
.tool-res {
  width:130px; flex-shrink:0; text-align:right;
  font-size:12px; color:var(--text-muted);
  overflow:hidden; text-overflow:ellipsis; white-space:nowrap;
}
</style>
