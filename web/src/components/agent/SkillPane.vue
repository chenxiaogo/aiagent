<template>
  <div class="pane">
    <div class="pane-head">
      <div>
        <h3>🎯 技能 Skills</h3>
        <p class="pane-desc">
          技能是智能体的可复用能力单元（渐进式披露：名称 + 描述 + 触发条件常驻提示词，完整内容由模型按需调用 load_skill 加载）。提示词类用于沉淀领域知识/话术/约束，工具类用于挂载启用的内置工具集合。
        </p>
      </div>
      <div class="head-actions">
        <el-button size="small" :icon="ShoppingCart" @click="openMarket">从市场导入</el-button>
        <el-button type="primary" size="small" :icon="Plus" @click="openCreate">新增技能</el-button>
      </div>
    </div>

    <el-empty v-if="!loading && list.length === 0" description="尚未配置技能" :image-size="70" />

    <div v-else class="card-list">
      <div v-for="item in list" :key="item.id" class="item-card">
        <div class="item-main">
          <div class="item-title">
            {{ item.name }}
            <el-tag size="small" effect="plain">{{ item.kind === 'tool' ? '工具类' : '提示词类' }}</el-tag>
            <el-tag v-if="item.skillLibId" size="small" type="warning" effect="plain">市场</el-tag>
            <el-tag size="small" :type="item.enabled ? 'success' : 'info'">
              {{ item.enabled ? '已启用' : '已停用' }}
            </el-tag>
          </div>
          <div class="item-desc">{{ item.description || '无描述' }}</div>
        </div>
        <div class="item-actions">
          <el-button size="small" :icon="Edit" @click="openEdit(item)">编辑</el-button>
          <el-button size="small" type="danger" :icon="Delete" @click="handleDelete(item)">删除</el-button>
        </div>
      </div>
    </div>

    <!-- 新增/编辑 -->
    <el-dialog v-model="dialogVisible" :title="editingId ? '编辑技能' : '新增技能'" width="600px">
      <el-form :model="form" label-width="96px">
        <el-form-item label="名称" required>
          <el-input v-model="form.name" placeholder="如：包裹异常识别" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="form.description" placeholder="一句话说明这个技能做什么" />
        </el-form-item>
        <el-form-item label="触发条件">
          <el-input v-model="form.summary" type="textarea" :rows="2" placeholder="什么时候使用该技能（常驻提示词，帮助模型判断何时加载全文），如：当用户询问监控画面中是否有可疑人员时" />
        </el-form-item>
        <el-form-item label="类型">
          <el-radio-group v-model="form.kind">
            <el-radio value="prompt">提示词类</el-radio>
            <el-radio value="tool">工具类</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item :label="form.kind === 'tool' ? '工具列表' : '提示词内容'">
          <el-input
            v-if="form.kind === 'tool'"
            v-model="form.content"
            type="textarea"
            :rows="3"
            placeholder='JSON 数组，如 ["search_camera","search_videos"]'
          />
          <el-input
            v-else
            v-model="form.content"
            type="textarea"
            :rows="7"
            placeholder="当分析监控画面时，优先关注包裹的状态变化…"
          />
        </el-form-item>
        <el-form-item label="排序">
          <el-input-number v-model="form.sortOrder" :min="0" :max="999" />
        </el-form-item>
        <el-form-item label="启用">
          <el-switch v-model="form.enabled" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="handleSubmit">保存</el-button>
      </template>
    </el-dialog>

    <!-- 从能力市场导入：支持勾选单条，也可按类型整组全选 -->
    <el-dialog v-model="marketVisible" title="从能力市场导入技能" width="780px">
      <div class="market-toolbar">
        <el-input
          v-model="marketKeyword"
          placeholder="搜索市场技能"
          clearable
          size="small"
          class="market-search"
          @input="loadMarket"
        />
        <el-select
          v-model="marketKind"
          size="small"
          class="market-kind"
          placeholder="全部类型"
          clearable
          @change="loadMarket"
        >
          <el-option label="提示词类" value="prompt" />
          <el-option label="工具类" value="tool" />
        </el-select>
        <div class="market-actions">
          <el-button size="small" :icon="Check" :disabled="!marketList.length" @click="selectAll('prompt')">
            全选提示词
          </el-button>
          <el-button size="small" :icon="Check" :disabled="!marketList.length" @click="selectAll('tool')">
            全选工具
          </el-button>
          <el-button size="small" :icon="Check" :disabled="!marketList.length" @click="selectAll('')">
            {{ allSelected ? '取消全选' : '全选' }}
          </el-button>
        </div>
      </div>

      <el-table
        ref="marketTableRef"
        :data="marketList"
        v-loading="marketLoading"
        size="small"
        style="width: 100%"
        @selection-change="onMarketSelectionChange"
      >
        <el-table-column type="selection" width="46" />
        <el-table-column prop="name" label="名称" min-width="150">
          <template #default="{ row }">
            {{ row.name }}
            <el-tag v-if="importedLibIds.has(row.id)" size="small" type="info" effect="plain">已导入</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="类型" width="100">
          <template #default="{ row }">{{ row.kind === 'tool' ? '工具类' : '提示词类' }}</template>
        </el-table-column>
        <el-table-column prop="category" label="分类" width="100" />
        <el-table-column prop="description" label="描述" min-width="180" show-overflow-tooltip />
        <el-table-column prop="refCount" label="被引用" width="86" align="center" />
      </el-table>
      <el-empty v-if="!marketLoading && !marketList.length" description="能力市场暂无技能" :image-size="60" />

      <template #footer>
        <span class="import-hint">
          已选 <b>{{ marketSelection.length }}</b> 项
        </span>
        <el-button @click="marketVisible = false">取消</el-button>
        <el-button
          type="primary"
          :loading="importing"
          :disabled="!marketSelection.length"
          @click="handleImport"
        >
          导入选中{{ marketSelection.length > 1 ? `（${marketSelection.length}）` : '' }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Edit, Delete, ShoppingCart, Check } from '@element-plus/icons-vue'
import { listSkills, createSkill, updateSkill, deleteSkill } from '@/api/agentTool'
import { listSkillLibrary } from '@/api/market'

const props = defineProps({ agentId: { type: Number, required: true } })

const list = ref([])
const loading = ref(false)
const dialogVisible = ref(false)
const editingId = ref(null)
const saving = ref(false)

// 能力市场导入
const marketVisible = ref(false)
const marketLoading = ref(false)
const marketList = ref([])
const marketKeyword = ref('')
const marketKind = ref('')
const marketTableRef = ref(null)
const marketSelection = ref([])
const importing = ref(false)

// 已导入过的市场技能 ID，用于在弹窗里标记，避免重复导入
const importedLibIds = computed(() =>
  new Set(list.value.map(s => s.skillLibId).filter(Boolean))
)
const allSelected = computed(
  () => marketList.value.length > 0 && marketSelection.value.length === marketList.value.length
)

const form = reactive({
  name: '',
  description: '',
  summary: '',
  kind: 'prompt',
  content: '',
  sortOrder: 0,
  enabled: true
})

async function loadList() {
  loading.value = true
  try {
    const res = await listSkills(props.agentId)
    list.value = res.data || []
  } catch (e) {
    ElMessage.error('加载失败')
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editingId.value = null
  Object.assign(form, {
    name: '', description: '', summary: '', kind: 'prompt', content: '', sortOrder: 0, enabled: true
  })
  dialogVisible.value = true
}

function openEdit(item) {
  editingId.value = item.id
  Object.assign(form, {
    name: item.name,
    description: item.description || '',
    summary: item.summary || '',
    kind: item.kind || 'prompt',
    content: item.content || '',
    sortOrder: item.sortOrder || 0,
    enabled: item.enabled
  })
  dialogVisible.value = true
}

async function handleSubmit() {
  if (!form.name) {
    ElMessage.warning('名称必填')
    return
  }
  saving.value = true
  try {
    if (editingId.value) {
      await updateSkill(editingId.value, { ...form, agentId: props.agentId })
    } else {
      await createSkill(props.agentId, { ...form })
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

function openMarket() {
  marketSelection.value = []
  marketKeyword.value = ''
  marketKind.value = ''
  marketVisible.value = true
  loadMarket()
}

async function loadMarket() {
  marketLoading.value = true
  try {
    const res = await listSkillLibrary({
      keyword: marketKeyword.value,
      kind: marketKind.value,
    })
    marketList.value = res.data || []
  } catch (e) { /* 忽略 */ } finally { marketLoading.value = false }
}

function onMarketSelectionChange(rows) {
  marketSelection.value = rows || []
}

// 全选：kind 为空表示全部；再次点击「取消全选」会清空选择
function selectAll(kind) {
  const table = marketTableRef.value
  if (!table) return
  const rows = kind ? marketList.value.filter(r => (r.kind || 'prompt') === kind) : marketList.value
  const picked = new Set(marketSelection.value.map(r => r.id))
  const allPicked = rows.length > 0 && rows.every(r => picked.has(r.id))
  rows.forEach(r => table.toggleRowSelection(r, !allPicked))
}

// 导入即复制市场技能内容作为快照，并记录 skillLibId 供市场侧统计引用。
// 逐条串行导入：既能给出准确的成功/失败数，也不会把市场接口打爆。
async function handleImport() {
  const rows = marketSelection.value
  if (!rows.length) return
  importing.value = true
  let ok = 0
  let fail = 0
  for (const src of rows) {
    try {
      await createSkill(props.agentId, {
        name: src.name,
        description: src.description || '',
        summary: src.summary || '',
        kind: src.kind || 'prompt',
        content: src.content || '',
        sortOrder: 0,
        enabled: true,
        skillLibId: src.id
      })
      ok++
    } catch (e) {
      fail++
    }
  }
  importing.value = false

  if (fail === 0) {
    ElMessage.success(ok === 1 ? '已从市场导入' : `已导入 ${ok} 个技能`)
  } else {
    ElMessage.warning(`导入完成：成功 ${ok} 个，失败 ${fail} 个`)
  }
  marketVisible.value = false
  loadList()
}

async function handleDelete(item) {
  await ElMessageBox.confirm(`确定删除技能「${item.name}」？`, '提示', { type: 'warning' })
  try {
    await deleteSkill(item.id)
    ElMessage.success('已删除')
    loadList()
  } catch (e) {
    ElMessage.error('删除失败')
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

.head-actions { display: flex; gap: 8px; flex-shrink: 0; }

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

.item-desc {
  font-size: 12px;
  color: var(--text-secondary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.item-actions { display: flex; gap: 6px; flex-shrink: 0; }

/* 市场导入弹窗 */
.market-toolbar { display: flex; align-items: center; gap: 8px; margin-bottom: 12px; }
.market-search { width: 220px; }
.market-kind { width: 130px; }
.market-actions { margin-left: auto; display: flex; gap: 6px; flex-wrap: wrap; justify-content: flex-end; }

.import-hint { margin-right: 12px; font-size: 12px; color: var(--text-secondary); }
</style>
