<template>
  <div class="page-container">
    <div class="page-header">
      <div>
        <div class="page-title">工具库</div>
        <div class="page-subtitle">平台级工具定义，智能体通过挂载使用</div>
      </div>
      <el-button type="primary" @click="handleCreate">新建工具</el-button>
    </div>

    <div class="filter-bar">
      <el-input v-model="keyword" placeholder="搜索名称/描述" clearable style="width:240px" @change="load" />
      <!-- 分组下拉包含数据里已存在的自定义分组，新建分组后也能筛到 -->
      <el-select v-model="category" placeholder="全部分组" clearable filterable style="width:160px" @change="load">
        <el-option
          v-for="c in categoryOptions"
          :key="c.value"
          :label="`${c.icon} ${c.label}`"
          :value="c.value"
        >
          <span class="opt-icon">{{ c.icon }}</span>
          <span>{{ c.label }}</span>
          <span class="opt-desc">{{ c.desc }}</span>
        </el-option>
      </el-select>
    </div>

    <el-table :data="list" v-loading="loading" stripe>
      <el-table-column prop="name" label="名称" min-width="160" />
      <el-table-column label="分组" width="120">
        <template #default="{ row }">
          <span class="cat-tag">{{ categoryMeta(row.category).icon }} {{ categoryMeta(row.category).label }}</span>
        </template>
      </el-table-column>
      <el-table-column label="类型" width="100">
        <template #default="{ row }">
          <el-tag size="small" :type="row.toolType === 'builtin' ? '' : 'warning'">{{ row.toolType === 'builtin' ? '内置' : 'HTTP' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="description" label="描述" min-width="240" show-overflow-tooltip />
      <el-table-column label="策略" min-width="200">
        <template #default="{ row }">
          <el-tag v-if="parseMeta(row.metadata).readOnly" size="small" type="success">只读</el-tag>
          <el-tag v-if="parseMeta(row.metadata).sideEffect" size="small" type="danger">副作用</el-tag>
          <el-tag v-if="parseMeta(row.metadata).approvalRequired" size="small" type="warning">需审批</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="引用" width="80" align="center">
        <template #default="{ row }">{{ row.refCount || 0 }}</template>
      </el-table-column>
      <el-table-column label="状态" width="90">
        <template #default="{ row }">
          <el-tag size="small" :type="row.status === 1 ? 'success' : 'info'">{{ row.status === 1 ? '启用' : '停用' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="160" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" @click="handleEdit(row)">编辑</el-button>
          <el-popconfirm title="确认删除？" @confirm="handleDelete(row)">
            <template #reference>
              <el-button link type="danger">删除</el-button>
            </template>
          </el-popconfirm>
        </template>
      </el-table-column>
    </el-table>

    <!-- 新建/编辑弹窗 -->
    <el-dialog v-model="dialogVisible" :title="editingId ? '编辑工具' : '新建工具'" width="600px">
      <el-form :model="form" label-width="90px">
        <el-form-item label="名称" required>
          <el-input v-model="form.name" placeholder="英文标识，如 doc_search" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="form.description" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item label="分组">
          <!-- 可选预设、也可直接输入新分组名：既避免分组发散，又不挡住自定义 -->
          <el-select
            v-model="form.category"
            placeholder="选择或输入新分组"
            filterable
            allow-create
            default-first-option
            style="width: 100%"
          >
            <el-option
              v-for="c in categoryOptions"
              :key="c.value"
              :label="`${c.icon} ${c.label}`"
              :value="c.value"
            >
              <span class="opt-icon">{{ c.icon }}</span>
              <span>{{ c.label }}</span>
              <span class="opt-desc">{{ c.desc }}</span>
            </el-option>
          </el-select>
          <div class="form-tip">分组会用于智能体挂载时的归类展示，建议从预设里选</div>
        </el-form-item>
        <el-form-item label="类型">
          <el-radio-group v-model="form.toolType">
            <el-radio value="builtin">内置</el-radio>
            <el-radio value="http">HTTP</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="参数定义">
          <el-input v-model="form.parameters" type="textarea" :rows="4" placeholder='JSON，如 {"query":{"type":"string","desc":"查询内容","required":true}}' />
          <div class="form-tip">JSON 格式，key 为参数名，value 含 type/desc/required</div>
        </el-form-item>
        <el-form-item label="元数据">
          <el-input v-model="form.metadata" type="textarea" :rows="3" placeholder='JSON，如 {"readOnly":true,"sideEffect":false,"approvalRequired":false,"resourceTypes":[]}' />
        </el-form-item>
        <el-form-item label="状态">
          <el-switch v-model="form.status" :active-value="1" :inactive-value="0" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSave">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { listToolLibrary, createToolLibrary, updateToolLibrary, deleteToolLibrary } from '@/api/market'
import { ElMessage } from 'element-plus'
import { toolCategoryOptions, toolCategoryMeta } from '@/constants/toolCategories'

const loading = ref(false)
const list = ref([])
const keyword = ref('')
const category = ref('')

// 预设分组 + 已有数据里出现过的自定义分组
const categoryOptions = computed(() =>
  toolCategoryOptions(list.value.map(t => t.category))
)
function categoryMeta(value) {
  return toolCategoryMeta(value)
}

const dialogVisible = ref(false)
const editingId = ref(null)
const form = reactive({
  name: '', description: '', category: '', toolType: 'builtin',
  parameters: '{}', metadata: '{}', status: 1,
})

function parseMeta(raw) {
  try { return raw ? JSON.parse(raw) : {} } catch { return {} }
}

async function load() {
  loading.value = true
  try {
    const res = await listToolLibrary({ keyword: keyword.value, category: category.value })
    if (res.code === 0) list.value = res.data || []
  } finally { loading.value = false }
}

function handleCreate() {
  editingId.value = null
  Object.assign(form, { name: '', description: '', category: '', toolType: 'builtin', parameters: '{}', metadata: '{}', status: 1 })
  dialogVisible.value = true
}

function handleEdit(row) {
  editingId.value = row.id
  Object.assign(form, {
    name: row.name, description: row.description, category: row.category,
    toolType: row.toolType, parameters: row.parameters || '{}',
    metadata: row.metadata || '{}', status: row.status,
  })
  dialogVisible.value = true
}

async function handleSave() {
  if (!form.name.trim()) return ElMessage.warning('请填写名称')
  try {
    const res = editingId.value
      ? await updateToolLibrary(editingId.value, form)
      : await createToolLibrary(form)
    if (res.code !== 0) {
      ElMessage.error(res.message || '保存失败')
      return
    }
    ElMessage.success('已保存')
    dialogVisible.value = false
    load()
  } catch (e) {
    ElMessage.error('保存失败')
  }
}

async function handleDelete(row) {
  const res = await deleteToolLibrary(row.id)
  if (res.code === 0) {
    ElMessage.success('已删除')
    load()
  } else {
    ElMessage.error(res.message || '删除失败')
  }
}

onMounted(load)
</script>

<style scoped>
.page-container { padding: 20px; }
.page-header { display:flex; justify-content:space-between; align-items:flex-start; margin-bottom:16px; }
.page-title { font-size:20px; font-weight:600; color:var(--text); }
.page-subtitle { margin-top:4px; font-size:13px; color:var(--text-muted); }
.filter-bar { display:flex; gap:12px; margin-bottom:16px; }
.form-tip { margin-top:4px; font-size:12px; color:var(--text-muted); }

.opt-icon { margin-right: 6px; }
.opt-desc { float: right; color: var(--text-muted); font-size: 12px; margin-left: 12px; }

.cat-tag {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 8px;
  border-radius: 10px;
  background: var(--bg-subtle);
  font-size: 12px;
  color: var(--text-secondary);
}
</style>
