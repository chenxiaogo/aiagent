<template>
  <div class="pane">
    <div class="pane-head">
      <div>
        <h3>❓ 预设问题</h3>
        <p class="pane-desc">配置后会在对话框空状态时展示，引导用户快速开始。</p>
      </div>
      <el-button type="primary" size="small" :icon="Plus" @click="openCreate">新增问题</el-button>
    </div>

    <el-empty v-if="!loading && list.length === 0" description="尚未配置预设问题" :image-size="70" />

    <div v-else class="card-list">
      <div v-for="item in list" :key="item.id" class="item-card">
        <div class="item-main">
          <div class="item-title">
            {{ item.question }}
            <el-tag size="small" :type="item.isActive ? 'success' : 'info'">
              {{ item.isActive ? '已启用' : '已停用' }}
            </el-tag>
          </div>
        </div>
        <div class="item-actions">
          <el-button size="small" :icon="Edit" @click="openEdit(item)">编辑</el-button>
          <el-button size="small" type="danger" :icon="Delete" @click="handleDelete(item)">删除</el-button>
        </div>
      </div>
    </div>

    <el-dialog v-model="dialogVisible" :title="editingId ? '编辑问题' : '新增问题'" width="520px">
      <el-form :model="form" label-width="72px">
        <el-form-item label="问题" required>
          <el-input v-model="form.question" type="textarea" :rows="3" placeholder="如：昨天有人在门口取包裹吗" />
        </el-form-item>
        <el-form-item label="排序">
          <el-input-number v-model="form.sortOrder" :min="0" :max="999" />
        </el-form-item>
        <el-form-item label="启用">
          <el-switch v-model="form.isActive" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="handleSubmit">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Edit, Delete } from '@element-plus/icons-vue'
import {
  getPresetQuestions, createPresetQuestion, updatePresetQuestion, deletePresetQuestion
} from '@/api/agent'

const props = defineProps({ agentId: { type: Number, required: true } })

const list = ref([])
const loading = ref(false)
const dialogVisible = ref(false)
const editingId = ref(null)
const saving = ref(false)

const form = reactive({ question: '', sortOrder: 0, isActive: true })

async function loadList() {
  loading.value = true
  try {
    const res = await getPresetQuestions(props.agentId)
    list.value = res.data || []
  } catch (e) {
    ElMessage.error('加载失败')
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editingId.value = null
  Object.assign(form, { question: '', sortOrder: 0, isActive: true })
  dialogVisible.value = true
}

function openEdit(item) {
  editingId.value = item.id
  Object.assign(form, {
    question: item.question,
    sortOrder: item.sortOrder || 0,
    isActive: item.isActive !== false
  })
  dialogVisible.value = true
}

async function handleSubmit() {
  if (!form.question.trim()) {
    ElMessage.warning('问题内容必填')
    return
  }
  saving.value = true
  try {
    if (editingId.value) {
      await updatePresetQuestion(editingId.value, { ...form })
    } else {
      await createPresetQuestion(props.agentId, { ...form })
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

async function handleDelete(item) {
  await ElMessageBox.confirm('确定删除该预设问题？', '提示', { type: 'warning' })
  try {
    await deletePresetQuestion(item.id)
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
}

.item-actions { display: flex; gap: 6px; flex-shrink: 0; }
</style>
