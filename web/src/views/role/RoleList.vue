<template>
  <div class="role-page">
    <div class="app-card">
      <div class="card-header">
        <div class="card-title">
          <el-icon color="var(--primary-color)"><Key /></el-icon>
          <span>角色管理</span>
        </div>
        <div class="card-actions">
          <el-button :icon="Refresh" @click="loadRoles">刷新</el-button>
          <el-button type="primary" :icon="Plus" @click="openCreate">新增角色</el-button>
        </div>
      </div>

      <el-table :data="roles" v-loading="loading" style="width: 100%" empty-text="暂无角色">
        <el-table-column prop="id" label="ID" width="70" />
        <el-table-column prop="code" label="编码" width="120">
          <template #default="{ row }">
            <el-tag :type="row.builtIn ? 'danger' : 'primary'" size="small">{{ row.code }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="name" label="角色名" min-width="120" />
        <el-table-column prop="description" label="描述" min-width="180">
          <template #default="{ row }">{{ row.description || '-' }}</template>
        </el-table-column>
        <el-table-column label="权限数" width="90">
          <template #default="{ row }">{{ row.permIds?.length || 0 }}</template>
        </el-table-column>
        <el-table-column label="操作" width="180" fixed="right">
          <template #default="{ row }">
            <el-button size="small" type="primary" link @click="openPerm(row)">授权</el-button>
            <el-button size="small" type="primary" link @click="openEdit(row)">编辑</el-button>
            <el-button size="small" type="danger" link :disabled="row.builtIn" @click="removeRole(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- 新增/编辑角色 -->
    <el-dialog v-model="dialogVisible" :title="form.id ? '编辑角色' : '新增角色'" width="460px">
      <el-form :model="form" label-width="80px">
        <el-form-item label="编码" required>
          <el-input v-model="form.code" placeholder="如 ops_manager" />
        </el-form-item>
        <el-form-item label="角色名" required>
          <el-input v-model="form.name" placeholder="角色名称" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="form.description" type="textarea" :rows="2" placeholder="角色描述" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="saveRole">保存</el-button>
      </template>
    </el-dialog>

    <!-- 授权对话框 -->
    <el-dialog v-model="permVisible" :title="`授权 - ${permTarget?.name || ''}`" width="760px" top="6vh">
      <el-tabs v-model="permTab" type="border-card">
        <!-- 权限点 -->
        <el-tab-pane label="权限点" name="perms">
          <div class="tab-body">
            <div class="perm-group" v-for="group in permGroups" :key="group.title">
              <div class="perm-group-title">{{ group.title }}</div>
              <el-checkbox-group v-model="permTargetPerms">
                <el-checkbox v-for="p in group.items" :key="p.id" :value="p.id" :label="p.name">
                  <span class="perm-code">{{ p.code }}</span>
                </el-checkbox>
              </el-checkbox-group>
            </div>
          </div>
        </el-tab-pane>

        <!-- 菜单 -->
        <el-tab-pane label="菜单" name="menus">
          <div class="tab-body">
            <el-tree
              ref="menuTreeRef"
              :data="menuTreeData"
              show-checkbox
              node-key="id"
              :props="{ label: 'title', children: 'children' }"
              default-expand-all
              :check-strictly="false"
            />
          </div>
        </el-tab-pane>

        <!-- 按钮 -->
        <el-tab-pane label="按钮" name="btns">
          <div class="tab-body">
            <div v-if="!btnMenuList.length" class="empty-tip">请先在「菜单」中勾选菜单</div>
            <div class="perm-group" v-for="m in btnMenuList" :key="m.id">
              <div class="perm-group-title">{{ m.title }}</div>
              <el-checkbox-group v-model="btnMap[m.id]">
                <el-checkbox v-for="b in m.btnAll" :key="b.id" :value="b.id" :label="b.name">
                  <span class="perm-code">{{ b.permCode || b.desc }}</span>
                </el-checkbox>
              </el-checkbox-group>
            </div>
          </div>
        </el-tab-pane>

        <!-- 接口 -->
        <el-tab-pane label="接口" name="apis">
          <div class="tab-body">
            <el-alert type="info" :closable="false" title="接口权限控制角色可调用的后端 API，仅对已登记为受管接口的请求生效" style="margin-bottom: 12px" />
            <div class="perm-group" v-for="group in apiGroups" :key="group.title">
              <div class="perm-group-title">{{ group.title }}</div>
              <el-checkbox-group v-model="permTargetApis">
                <el-checkbox v-for="a in group.items" :key="a.id" :value="a.id">
                  <el-tag size="small" :type="methodType(a.method)" class="method-tag">{{ a.method }}</el-tag>
                  <span class="api-path">{{ a.path }}</span>
                </el-checkbox>
              </el-checkbox-group>
            </div>
          </div>
        </el-tab-pane>
      </el-tabs>
      <template #footer>
        <el-button @click="permVisible = false">取消</el-button>
        <el-button type="primary" @click="savePerms">保存授权</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, nextTick } from 'vue'
import { Refresh, Plus, Key } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { listRoles, createRole, updateRole, deleteRole, listPermissions, setRolePerms, setRoleMenus, setRoleMenuBtns, setRoleApis } from '@/api/role'
import { listMenus } from '@/api/menu'
import { listApis } from '@/api/api'

const loading = ref(false)
const roles = ref([])
const permissions = ref([])
const allMenus = ref([]) // 全部菜单（扁平+树）
const apis = ref([]) // 全部接口
const dialogVisible = ref(false)
const permVisible = ref(false)
const permTarget = ref(null)
const permTargetPerms = ref([])
const permTargetApis = ref([])
const permTab = ref('perms')
const menuTreeRef = ref(null)
const btnMap = reactive({}) // 角色各菜单下已勾选的按钮

const form = reactive({ id: null, code: '', name: '', description: '' })

// 按模块分组权限（权限码沿用 scheduler-platform 语义，见后端 model 包常量注释）
const permGroups = computed(() => {
  const groups = [
    { title: '智能体', keys: ['task:'] },
    { title: '执行与日志', keys: ['exec:', 'log:'] },
    { title: '运维主机', keys: ['node:', 'host:'] },
    { title: '能力市场', keys: ['market:'] },
    { title: '运行观测', keys: ['ops:'] },
    { title: '系统管理', keys: ['user:', 'role:', 'system:', 'notify:'] }
  ]
  return groups.map((g) => ({
    title: g.title,
    items: permissions.value.filter((p) => g.keys.some((k) => p.code.startsWith(k)))
  })).filter((g) => g.items.length)
})

// 菜单树（用于勾选角色菜单）
const menuTreeData = computed(() => allMenus.value)

// 当前勾选的菜单（含按钮定义），用于按钮授权标签页
const btnMenuList = computed(() => {
  const checked = menuTreeRef.value?.getCheckedKeys?.() || []
  return allMenus.value.filter((m) => checked.includes(m.id) && (m.btnAll?.length || 0))
})

// 按接口分组
const apiGroups = computed(() => {
  const map = {}
  apis.value.forEach((a) => {
    const g = a.group || '其他'
    ;(map[g] = map[g] || []).push(a)
  })
  return Object.keys(map).map((title) => ({ title, items: map[title] }))
})

function methodType(method) {
  const map = { GET: 'success', POST: 'primary', PUT: 'warning', DELETE: 'danger' }
  return map[method] || 'info'
}

function resetForm() {
  Object.assign(form, { id: null, code: '', name: '', description: '' })
}

async function loadRoles() {
  loading.value = true
  try {
    const res = await listRoles()
    roles.value = res.data || []
  } finally {
    loading.value = false
  }
}

async function loadPermissions() {
  const res = await listPermissions()
  permissions.value = res.data || []
}

async function loadMenusAndApis() {
  const menuRes = await listMenus()
  allMenus.value = menuRes.data || []
  const apiRes = await listApis()
  apis.value = apiRes.data || []
}

function openCreate() {
  resetForm()
  dialogVisible.value = true
}

function openEdit(row) {
  Object.assign(form, {
    id: row.id, code: row.code, name: row.name, description: row.description
  })
  dialogVisible.value = true
}

async function saveRole() {
  if (!form.code || !form.name) return ElMessage.warning('请输入编码和角色名')
  if (form.id) {
    await updateRole(form.id, form)
    ElMessage.success('角色已更新')
  } else {
    await createRole(form)
    ElMessage.success('角色已创建')
  }
  dialogVisible.value = false
  loadRoles()
}

async function openPerm(row) {
  permTarget.value = row
  permTargetPerms.value = [...(row.permIds || [])]
  permTab.value = 'perms'
  permVisible.value = true
  // 清空按钮选择
  Object.keys(btnMap).forEach((k) => delete btnMap[k])
  await nextTick()
  // 回填菜单勾选状态
  menuTreeRef.value?.setCheckedKeys(row.menuIds || [])
  // 回填按钮勾选状态
  const rBtn = row.btnMap || {}
  Object.keys(rBtn).forEach((mid) => {
    btnMap[mid] = [...(rBtn[mid] || [])]
  })
  // 回填接口勾选状态
  permTargetApis.value = [...(row.apiIds || [])]
}

async function savePerms() {
  const id = permTarget.value.id
  await setRolePerms(id, permTargetPerms.value)
  // 菜单
  const menuIds = menuTreeRef.value?.getCheckedKeys?.() || []
  await setRoleMenus(id, menuIds)
  // 按钮：逐菜单保存
  const btnMapToSave = {}
  Object.keys(btnMap).forEach((mid) => {
    const ids = btnMap[mid]
    if (ids?.length) btnMapToSave[mid] = ids
  })
  for (const mid in btnMapToSave) {
    await setRoleMenuBtns(id, Number(mid), btnMapToSave[mid])
  }
  // 接口
  await setRoleApis(id, permTargetApis.value)
  ElMessage.success('授权已保存')
  permVisible.value = false
  loadRoles()
}

async function removeRole(row) {
  await ElMessageBox.confirm(`确认删除角色「${row.name}」？`, '删除确认', { type: 'error' })
  await deleteRole(row.id)
  ElMessage.success('角色已删除')
  loadRoles()
}

onMounted(() => {
  loadRoles()
  loadPermissions()
  loadMenusAndApis()
})
</script>

<style scoped>
.role-page {
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
.perm-group {
  margin-bottom: 16px;
}
.perm-group-title {
  font-weight: 600;
  color: #606266;
  margin-bottom: 8px;
  padding-bottom: 6px;
  border-bottom: 1px dashed #ebeef5;
}
.perm-group .el-checkbox {
  margin-right: 16px;
  margin-bottom: 8px;
}
.perm-code {
  font-size: 11px;
  color: #909399;
  margin-left: 4px;
}
.tab-body {
  max-height: 48vh;
  overflow-y: auto;
  padding: 4px;
}
.empty-tip {
  color: #909399;
  text-align: center;
  padding: 24px 0;
  font-size: 13px;
}
.method-tag {
  margin-right: 6px;
}
.api-path {
  font-family: monospace;
  font-size: 12px;
  color: #303133;
}
</style>
