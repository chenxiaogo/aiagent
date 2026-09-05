<template>
  <div class="page-container">
    <div class="page-header">
      <div>
        <h2 class="page-title">技能库</h2>
        <p class="page-sub">平台级可复用技能（提示词片段 / 工具集合），智能体引用后生效。</p>
      </div>
      <el-button type="primary" :icon="Plus" @click="openCreate">新建技能</el-button>
    </div>

    <el-card shadow="never">
      <el-table :data="list" v-loading="loading" stripe>
        <el-table-column prop="name" label="名称" min-width="140" />
        <el-table-column prop="category" label="分类" width="110" />
        <el-table-column label="类型" width="100">
          <template #default="{ row }">
            <el-tag size="small">{{ row.kind === 'tool' ? '工具集合' : '提示词' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="description" label="描述" min-width="200" show-overflow-tooltip />
        <el-table-column label="引用" width="80">
          <template #default="{ row }">{{ row.refCount || 0 }}</template>
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

    <el-dialog v-model="dialog" :title="form.id ? '编辑技能' : '新建技能'" width="560px">
      <el-form :model="form" label-width="90px">
        <el-form-item label="名称" required>
          <el-input v-model="form.name" />
        </el-form-item>
        <el-form-item label="分类">
          <el-input v-model="form.category" placeholder="如 report / search" />
        </el-form-item>
        <el-form-item label="类型">
          <el-select v-model="form.kind" style="width:100%">
            <el-option label="提示词片段" value="prompt" />
            <el-option label="工具集合" value="tool" />
          </el-select>
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="form.description" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item label="触发条件">
          <el-input v-model="form.summary" type="textarea" :rows="2" placeholder="何时使用该技能（常驻提示词摘要），如：当用户询问监控画面中是否有可疑人员时" />
        </el-form-item>
        <el-form-item label="内容" required>
          <el-input v-model="form.content" type="textarea" :rows="5"
            :placeholder="form.kind === 'tool' ? '[&quot;tool_a&quot;,&quot;tool_b&quot;]' : '提示词内容'" />
        </el-form-item>
        <el-form-item label="状态">
          <el-switch v-model="form.status" :active-value="1" :inactive-value="0" />
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
  listSkillLibrary, createSkillLibrary, updateSkillLibrary, deleteSkillLibrary
} from '@/api/market'

const list = ref([])
const loading = ref(false)
const dialog = ref(false)
const saving = ref(false)
const form = ref(emptyForm())

function emptyForm() {
  return { id: null, name: '', category: '', kind: 'prompt', description: '', summary: '', content: '', status: 1 }
}

async function load() {
  loading.value = true
  try {
    const res = await listSkillLibrary()
    if (res.code === 0) list.value = res.data
  } finally {
    loading.value = false
  }
}
function openCreate() { form.value = emptyForm(); dialog.value = true }
function openEdit(row) { form.value = { ...row }; dialog.value = true }
async function save() {
  if (!form.value.name || !form.value.kind) return ElMessage.warning('名称与类型必填')
  saving.value = true
  try {
    const api = form.value.id ? updateSkillLibrary(form.value.id, form.value) : createSkillLibrary(form.value)
    const res = await api
    if (res.code === 0) { ElMessage.success('已保存'); dialog.value = false; load() }
  } finally { saving.value = false }
}
async function remove(row) {
  await ElMessageBox.confirm(`确认删除「${row.name}」？`, '提示', { type: 'warning' })
  const res = await deleteSkillLibrary(row.id)
  if (res.code === 0) { ElMessage.success('已删除'); load() }
}
onMounted(load)
</script>

<style scoped>
.page-container { padding: 20px; }
.page-header { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 16px; }
.page-title { margin: 0 0 4px; font-size: 20px; }
.page-sub { margin: 0; color: #8a8f99; font-size: 13px; }
</style>
