<template>
  <div class="api-page">
    <div class="app-card">
      <div class="card-header">
        <div class="card-title">
          <el-icon color="var(--primary-color)"><Link /></el-icon>
          <span>接口管理</span>
        </div>
        <div class="card-actions">
          <el-button :icon="Refresh" @click="loadApis">刷新</el-button>
          <el-button type="primary" :icon="Plus" @click="openCreate">新增接口</el-button>
        </div>
      </div>

      <el-table :data="apis" v-loading="loading" style="width: 100%" empty-text="暂无接口">
        <el-table-column prop="id" label="ID" width="70" />
        <el-table-column prop="group" label="分组" width="120" />
        <el-table-column label="方法" width="90">
          <template #default="{ row }">
            <el-tag size="small" :type="methodType(row.method)">{{ row.method }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="path" label="接口路径" min-width="220" />
        <el-table-column prop="description" label="描述" min-width="160">
          <template #default="{ row }">{{ row.description || '-' }}</template>
        </el-table-column>
        <el-table-column label="操作" width="120" fixed="right">
          <template #default="{ row }">
            <el-button size="small" type="primary" link @click="openEdit(row)">编辑</el-button>
            <el-button size="small" type="danger" link @click="removeApi(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- 新增/编辑接口 -->
    <el-dialog v-model="dialogVisible" :title="form.id ? '编辑接口' : '新增接口'" width="500px">
      <el-form :model="form" label-width="80px">
        <el-form-item label="接口路径" required>
          <el-input v-model="form.path" placeholder="如 /api/agents/:id" />
        </el-form-item>
        <el-form-item label="方法" required>
          <el-select v-model="form.method" style="width: 100%">
            <el-option v-for="m in ['GET', 'POST', 'PUT', 'DELETE']" :key="m" :label="m" :value="m" />
          </el-select>
        </el-form-item>
        <el-form-item label="分组">
          <el-input v-model="form.group" placeholder="如 智能体管理" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="form.description" placeholder="接口用途说明" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="saveApi">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { Refresh, Plus, Link } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { listApis, createApi, updateApi, deleteApi } from '@/api/api'

const loading = ref(false)
const apis = ref([])
const dialogVisible = ref(false)
const form = reactive({ id: null, path: '', method: 'GET', group: '', description: '' })

function methodType(m) {
  if (m === 'GET') return 'success'
  if (m === 'POST') return 'primary'
  if (m === 'PUT') return 'warning'
  if (m === 'DELETE') return 'danger'
  return 'info'
}

function resetForm() {
  Object.assign(form, { id: null, path: '', method: 'GET', group: '', description: '' })
}

async function loadApis() {
  loading.value = true
  try {
    const res = await listApis()
    apis.value = res.data || []
  } finally {
    loading.value = false
  }
}

function openCreate() {
  resetForm()
  dialogVisible.value = true
}

function openEdit(row) {
  Object.assign(form, { id: row.id, path: row.path, method: row.method, group: row.group, description: row.description })
  dialogVisible.value = true
}

async function saveApi() {
  if (!form.path || !form.method) return ElMessage.warning('请输入路径和方法')
  if (form.id) {
    await updateApi(form.id, form)
    ElMessage.success('接口已更新')
  } else {
    await createApi(form)
    ElMessage.success('接口已创建')
  }
  dialogVisible.value = false
  loadApis()
}

async function removeApi(row) {
  await ElMessageBox.confirm(`确认删除接口 ${row.method} ${row.path}？相关角色授权将同步清除。`, '删除确认', { type: 'error' })
  await deleteApi(row.id)
  ElMessage.success('接口已删除')
  loadApis()
}

onMounted(loadApis)
</script>

<style scoped>
.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}
.card-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 16px;
  font-weight: 600;
  color: #303133;
}
.card-actions {
  display: flex;
  gap: 8px;
}
</style>
