<template>
  <div class="user-page">
    <div class="app-card">
      <div class="card-header">
        <div class="card-title">
          <el-icon color="var(--primary-color)"><User /></el-icon>
          <span>用户管理</span>
        </div>
        <div class="card-actions">
          <el-button :icon="Refresh" @click="loadUsers">刷新</el-button>
          <el-button type="primary" :icon="Plus" @click="openCreate">新增用户</el-button>
        </div>
      </div>

      <el-table :data="users" v-loading="loading" style="width: 100%" empty-text="暂无用户">
        <el-table-column prop="id" label="ID" width="70" />
        <el-table-column prop="username" label="用户名" min-width="120" />
        <el-table-column prop="nickname" label="昵称" min-width="120" />
        <el-table-column prop="email" label="邮箱" min-width="160">
          <template #default="{ row }">{{ row.email || '-' }}</template>
        </el-table-column>
        <el-table-column label="角色" width="130">
          <template #default="{ row }">
            <el-tag v-if="row.isAdmin" type="danger" size="small" effect="dark">管理员</el-tag>
            <el-tag v-else type="info" size="small">{{ row.roleName || '未分配' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag :type="row.status === 1 ? 'success' : 'info'" size="small">{{ row.status === 1 ? '启用' : '禁用' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="createdAt" label="创建时间" width="160" />
        <el-table-column label="操作" width="200" fixed="right">
          <template #default="{ row }">
            <el-button size="small" type="primary" link @click="openEdit(row)">编辑</el-button>
            <el-button size="small" type="warning" link @click="openReset(row)">重置密码</el-button>
            <el-button size="small" type="danger" link :disabled="row.id === userStore.user?.uid" @click="removeUser(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- 新增/编辑用户 -->
    <el-dialog v-model="dialogVisible" :title="form.id ? '编辑用户' : '新增用户'" width="480px">
      <el-form :model="form" label-width="80px">
        <el-form-item label="用户名" required>
          <el-input v-model="form.username" :disabled="!!form.id" placeholder="登录用户名" />
        </el-form-item>
        <el-form-item v-if="!form.id" label="密码" required>
          <el-input v-model="form.password" type="password" show-password placeholder="初始密码" />
        </el-form-item>
        <el-form-item label="昵称">
          <el-input v-model="form.nickname" placeholder="昵称" />
        </el-form-item>
        <el-form-item label="邮箱">
          <el-input v-model="form.email" placeholder="邮箱" />
        </el-form-item>
        <el-form-item label="角色">
          <el-select v-model="form.roleId" style="width: 100%">
            <el-option v-for="r in roles" :key="r.id" :label="r.name" :value="r.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="管理员">
          <el-switch v-model="form.isAdmin" />
          <span class="form-tip">管理员拥有全部权限</span>
        </el-form-item>
        <el-form-item label="状态">
          <el-switch v-model="form.statusOn" active-text="启用" inactive-text="禁用" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="saveUser">保存</el-button>
      </template>
    </el-dialog>

    <!-- 重置密码 -->
    <el-dialog v-model="resetVisible" title="重置密码" width="420px">
      <el-form label-width="80px">
        <el-form-item label="新密码" required>
          <el-input v-model="resetPwd" type="password" show-password placeholder="请输入新密码" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="resetVisible = false">取消</el-button>
        <el-button type="primary" @click="saveReset">确认重置</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { Refresh, Plus, User } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { listUsers, createUser, updateUser, resetPassword, deleteUser } from '@/api/user'
import { listRoles } from '@/api/role'
import { useUserStore } from '@/stores/user'

const userStore = useUserStore()
const loading = ref(false)
const users = ref([])
const roles = ref([])
const dialogVisible = ref(false)
const resetVisible = ref(false)
const resetTargetId = ref(null)
const resetPwd = ref('')

const form = reactive({
  id: null, username: '', password: '', nickname: '', email: '',
  roleId: null, isAdmin: false, statusOn: true
})

function resetForm() {
  Object.assign(form, {
    id: null, username: '', password: '', nickname: '', email: '',
    roleId: null, isAdmin: false, statusOn: true
  })
}

async function loadUsers() {
  loading.value = true
  try {
    const res = await listUsers()
    users.value = res.data || []
  } finally {
    loading.value = false
  }
}

async function loadRoles() {
  const res = await listRoles()
  roles.value = res.data || []
}

function openCreate() {
  resetForm()
  // 未选角色时默认落到 viewer，避免新建账号无任何权限
  const viewer = roles.value.find((r) => r.code === 'viewer')
  if (viewer) form.roleId = viewer.id
  dialogVisible.value = true
}

function openEdit(row) {
  Object.assign(form, {
    id: row.id, username: row.username, password: '', nickname: row.nickname,
    email: row.email, roleId: row.roleId, isAdmin: row.isAdmin, statusOn: row.status === 1
  })
  dialogVisible.value = true
}

async function saveUser() {
  if (form.id) {
    await updateUser(form.id, {
      nickname: form.nickname, email: form.email,
      roleId: form.roleId, isAdmin: form.isAdmin,
      status: form.statusOn ? 1 : 0
    })
    ElMessage.success('用户已更新')
  } else {
    if (!form.username || !form.password) return ElMessage.warning('请输入用户名和密码')
    await createUser({
      username: form.username, password: form.password, nickname: form.nickname,
      email: form.email, roleId: form.roleId, isAdmin: form.isAdmin,
      status: form.statusOn ? 1 : 0
    })
    ElMessage.success('用户已创建')
  }
  dialogVisible.value = false
  loadUsers()
}

function openReset(row) {
  resetTargetId.value = row.id
  resetPwd.value = ''
  resetVisible.value = true
}

async function saveReset() {
  if (!resetPwd.value) return ElMessage.warning('请输入新密码')
  await resetPassword(resetTargetId.value, resetPwd.value)
  ElMessage.success('密码已重置')
  resetVisible.value = false
}

async function removeUser(row) {
  await ElMessageBox.confirm(`确认删除用户「${row.username}」？`, '删除确认', { type: 'error' })
  await deleteUser(row.id)
  ElMessage.success('用户已删除')
  loadUsers()
}

onMounted(() => {
  loadRoles()
  loadUsers()
})
</script>

<style scoped>
.user-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
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
.form-tip {
  font-size: 12px;
  color: #909399;
  margin-left: 8px;
}
</style>
