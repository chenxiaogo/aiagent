<template>
  <div class="menu-page">
    <div class="app-card">
      <div class="card-header">
        <div class="card-title">
          <el-icon color="var(--primary-color)"><Grid /></el-icon>
          <span>菜单管理</span>
        </div>
        <div class="card-actions">
          <el-button :icon="Refresh" @click="loadMenus">刷新</el-button>
          <el-button type="primary" :icon="Plus" @click="openCreate(null)">新增顶级菜单</el-button>
        </div>
      </div>

      <el-table
        :data="menuTree"
        row-key="id"
        :tree-props="{ children: 'children' }"
        v-loading="loading"
        style="width: 100%"
        empty-text="暂无菜单"
      >
        <el-table-column prop="title" label="菜单名" min-width="160" />
        <el-table-column prop="path" label="路由路径" min-width="140">
          <template #default="{ row }">{{ row.path || '-' }}</template>
        </el-table-column>
        <el-table-column label="类型" width="90">
          <template #default="{ row }">
            <el-tag size="small" :type="row.type === 'dir' ? 'warning' : row.type === 'button' ? 'info' : 'primary'" effect="plain">
              {{ row.type === 'dir' ? '目录' : row.type === 'button' ? '按钮' : '菜单' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="sort" label="排序" width="70" />
        <el-table-column prop="permCode" label="权限码" min-width="120">
          <template #default="{ row }">{{ row.permCode || '-' }}</template>
        </el-table-column>
        <el-table-column label="操作" width="200" fixed="right">
          <template #default="{ row }">
            <el-button size="small" type="primary" link @click="openCreate(row)">新增子项</el-button>
            <el-button size="small" type="primary" link @click="openEdit(row)">编辑</el-button>
            <el-button size="small" type="warning" link @click="openBtns(row)">按钮</el-button>
            <el-button size="small" type="danger" link @click="removeMenu(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- 新增/编辑菜单 -->
    <el-dialog v-model="dialogVisible" :title="form.id ? '编辑菜单' : '新增菜单'" width="520px">
      <el-form :model="form" label-width="90px">
        <el-form-item label="菜单类型">
          <el-radio-group v-model="form.type">
            <el-radio value="dir">目录</el-radio>
            <el-radio value="menu">菜单</el-radio>
            <el-radio value="button">按钮</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="菜单名" required>
          <el-input v-model="form.title" placeholder="菜单显示名称" />
        </el-form-item>
        <el-form-item label="路由名称">
          <el-input v-model="form.name" placeholder="如 Agents" />
        </el-form-item>
        <el-form-item label="路由路径">
          <el-input v-model="form.path" placeholder="如 /agents 或 /settings/users" />
        </el-form-item>
        <el-form-item v-if="form.type === 'menu'" label="组件路径">
          <el-input v-model="form.component" placeholder="如 view/agent/AgentList" />
        </el-form-item>
        <el-form-item label="图标">
          <el-input v-model="form.icon" placeholder="Element Plus 图标名，如 MagicStick" />
        </el-form-item>
        <el-form-item label="权限码">
          <el-input v-model="form.permCode" placeholder="如 task:view" />
        </el-form-item>
        <el-form-item label="排序">
          <el-input-number v-model="form.sort" :min="0" :max="999" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="saveMenu">保存</el-button>
      </template>
    </el-dialog>

    <!-- 菜单按钮配置 -->
    <el-dialog v-model="btnsVisible" :title="`按钮配置 - ${btnMenu?.title || ''}`" width="520px">
      <el-alert type="info" :closable="false" title="配置该菜单下的按钮，供角色授权使用" style="margin-bottom: 12px" />
      <el-table :data="btnRows" style="width: 100%">
        <el-table-column label="按钮Key" min-width="120">
          <template #default="{ row }">
            <el-input v-model="row.name" placeholder="如 add/edit/delete" size="small" />
          </template>
        </el-table-column>
        <el-table-column label="按钮权限码" min-width="140">
          <template #default="{ row }">
            <el-input v-model="row.permCode" placeholder="如 task:create" size="small" />
          </template>
        </el-table-column>
        <el-table-column label="备注" min-width="140">
          <template #default="{ row }">
            <el-input v-model="row.desc" placeholder="按钮描述" size="small" />
          </template>
        </el-table-column>
        <el-table-column label="操作" width="60">
          <template #default="{ $index }">
            <el-button size="small" type="danger" link @click="btnRows.splice($index, 1)">删</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div style="margin-top: 12px">
        <el-button :icon="Plus" size="small" @click="addBtnRow">添加按钮</el-button>
      </div>
      <template #footer>
        <el-button @click="btnsVisible = false">取消</el-button>
        <el-button type="primary" @click="saveBtns">保存按钮</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { Refresh, Plus, Grid } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { listMenus, createMenu, updateMenu, deleteMenu, saveMenuBtns } from '@/api/menu'

const loading = ref(false)
const menuTree = ref([])
const dialogVisible = ref(false)
const btnsVisible = ref(false)
const btnMenu = ref(null)
const btnRows = ref([])

const form = reactive({
  id: null, type: 'menu', title: '', name: '', path: '', component: '',
  icon: '', permCode: '', sort: 0, parentId: 0
})

function resetForm() {
  Object.assign(form, {
    id: null, type: 'menu', title: '', name: '', path: '', component: '',
    icon: '', permCode: '', sort: 0, parentId: 0
  })
}

async function loadMenus() {
  loading.value = true
  try {
    const res = await listMenus()
    menuTree.value = res.data || []
  } finally {
    loading.value = false
  }
}

function openCreate(parent) {
  resetForm()
  if (parent) {
    form.parentId = parent.id
    form.type = 'menu'
  }
  dialogVisible.value = true
}

function openEdit(row) {
  Object.assign(form, {
    id: row.id, type: row.type, title: row.title, name: row.name, path: row.path,
    component: row.component, icon: row.icon, permCode: row.permCode, sort: row.sort,
    parentId: row.parentId
  })
  dialogVisible.value = true
}

async function saveMenu() {
  if (!form.title) return ElMessage.warning('请输入菜单名')
  const payload = { ...form }
  if (form.id) {
    await updateMenu(form.id, payload)
    ElMessage.success('菜单已更新')
  } else {
    await createMenu(payload)
    ElMessage.success('菜单已创建')
  }
  dialogVisible.value = false
  loadMenus()
}

async function removeMenu(row) {
  await ElMessageBox.confirm(`确认删除菜单「${row.title}」？其子菜单与关联将被一并删除。`, '删除确认', { type: 'error' })
  await deleteMenu(row.id)
  ElMessage.success('菜单已删除')
  loadMenus()
}

function openBtns(row) {
  btnMenu.value = row
  btnRows.value = (row.btnAll || []).map((b) => ({ name: b.name, desc: b.desc, permCode: b.permCode }))
  if (!btnRows.value.length) addBtnRow()
  btnsVisible.value = true
}

function addBtnRow() {
  btnRows.value.push({ name: '', desc: '', permCode: '' })
}

async function saveBtns() {
  const btns = btnRows.value.filter((b) => b.name)
  await saveMenuBtns(btnMenu.value.id, btns)
  ElMessage.success('按钮已保存')
  btnsVisible.value = false
  loadMenus()
}

onMounted(loadMenus)
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
