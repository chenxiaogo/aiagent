<template>
  <div class="page-container">
    <div class="page-header">
      <div>
        <h2 class="page-title">MCP 注册表</h2>
        <p class="page-sub">平台级可复用 MCP 服务目录，智能体引用后挂载，避免重复配置连接。</p>
      </div>
      <el-button type="primary" :icon="Plus" @click="openCreate">新建注册项</el-button>
    </div>

    <el-card shadow="never">
      <el-table :data="list" v-loading="loading" stripe>
        <el-table-column prop="name" label="名称" min-width="140" />
        <el-table-column prop="category" label="分类" width="110" />
        <el-table-column prop="transport" label="传输" width="130" />
        <el-table-column prop="url" label="地址" min-width="220" show-overflow-tooltip />
        <el-table-column prop="version" label="版本" width="90" />
        <el-table-column label="引用" width="80">
          <template #default="{ row }">{{ row.refCount || 0 }}</template>
        </el-table-column>
        <el-table-column label="免审批" width="90">
          <template #default="{ row }">
            <el-tag v-if="row.approvalRequired === false" type="warning" size="small">免审批</el-tag>
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag :type="row.status === 1 ? 'success' : 'info'" size="small">
              {{ row.status === 1 ? '启用' : '停用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="160" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
            <el-button link type="danger" @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog v-model="dialog" :title="form.id ? '编辑注册项' : '新建注册项'" width="520px">
      <el-form :model="form" label-width="90px">
        <el-form-item label="名称" required>
          <el-input v-model="form.name" placeholder="如 高德地图 MCP" />
        </el-form-item>
        <el-form-item label="分类">
          <el-input v-model="form.category" placeholder="如 map / search" />
        </el-form-item>
        <el-form-item label="传输方式">
          <el-select v-model="form.transport" style="width:100%">
            <el-option label="SSE" value="sse" />
            <el-option label="Streamable HTTP" value="streamable_http" />
          </el-select>
        </el-form-item>
        <el-form-item label="地址" required>
          <el-input v-model="form.url" placeholder="https://.../mcp" />
        </el-form-item>
        <el-form-item label="版本">
          <el-input v-model="form.version" placeholder="v1" />
        </el-form-item>
        <el-form-item label="Headers">
          <el-input v-model="form.headers" type="textarea" :rows="2" placeholder='{"Authorization":"Bearer xxx"}' />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="form.description" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item label="状态">
          <el-switch v-model="form.status" :active-value="1" :inactive-value="0" />
        </el-form-item>
        <el-form-item label="免审批">
          <el-switch v-model="form.noApproval" />
          <div class="form-tip">
            开启后，智能体导入该服务时会自动继承免审批，其工具不再弹人工确认、直接执行。
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialog = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { Plus } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  listMCPRegistry, createMCPRegistry, updateMCPRegistry, deleteMCPRegistry
} from '@/api/market'

const list = ref([])
const loading = ref(false)
const dialog = ref(false)
const saving = ref(false)
const form = ref(emptyForm())

function emptyForm() {
  return {
    id: null, name: '', category: '', transport: 'sse', url: '',
    version: '', headers: '', description: '', status: 1, noApproval: false
  }
}

async function load() {
  loading.value = true
  try {
    const res = await listMCPRegistry()
    if (res.code === 0) list.value = res.data
  } finally {
    loading.value = false
  }
}

function openCreate() {
  form.value = emptyForm()
  dialog.value = true
}
function openEdit(row) {
  // 后端 approvalRequired 为 false 才表示免审批；null / true 均视为需审批
  form.value = { ...row, noApproval: row.approvalRequired === false }
  dialog.value = true
}
async function save() {
  if (!form.value.name || !form.value.url) return ElMessage.warning('名称与地址必填')
  saving.value = true
  try {
    const payload = { ...form.value, approvalRequired: !form.value.noApproval }
    delete payload.noApproval
    const api = form.value.id ? updateMCPRegistry(form.value.id, payload)
                            : createMCPRegistry(payload)
    const res = await api
    if (res.code === 0) {
      ElMessage.success('已保存')
      dialog.value = false
      load()
    }
  } finally {
    saving.value = false
  }
}
async function remove(row) {
  await ElMessageBox.confirm(`确认删除「${row.name}」？`, '提示', { type: 'warning' })
  const res = await deleteMCPRegistry(row.id)
  if (res.code === 0) { ElMessage.success('已删除'); load() }
}

onMounted(load)
</script>

<style scoped>
.page-container { padding: 20px; }
.page-header { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 16px; }
.page-title { margin: 0 0 4px; font-size: 20px; }
.page-sub { margin: 0; color: #8a8f99; font-size: 13px; }

.form-tip {
  font-size: 12px;
  color: #8a8f99;
  line-height: 1.5;
  margin-top: 4px;
}
</style>
