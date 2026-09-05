<template>
  <div class="ops-files">
    <!-- 路径栏 -->
    <div class="file-toolbar">
      <el-button link size="small" :disabled="!canGoUp" title="上级目录" @click="goUp">
        <el-icon><ArrowUp /></el-icon>
      </el-button>
      <el-button link size="small" :disabled="!homePath" title="回到账号主目录" @click="goHome">
        <el-icon><House /></el-icon>
      </el-button>
      <el-button link size="small" title="刷新" @click="loadList">
        <el-icon><Refresh /></el-icon>
      </el-button>
      <div class="path-crumb" :title="currentPath">{{ currentPath }}</div>

      <div class="toolbar-right">
        <el-button size="small" :icon="FolderAdd" @click="openMkdir">新建目录</el-button>
        <el-button size="small" type="primary" :icon="UploadFilled" @click="pickFile">上传文件</el-button>
        <input
          ref="fileInput"
          type="file"
          multiple
          style="display: none"
          @change="onFilePicked"
        />
      </div>
    </div>

    <!-- 上传进度 -->
    <div v-if="uploading" class="upload-progress">
      <el-progress :percentage="uploadPercent" :stroke-width="6" />
      <span class="upload-name">{{ uploadingName }}</span>
    </div>

    <!-- 文件列表 -->
    <div class="file-table" v-loading="loading">
      <el-table
        :data="entries"
        size="small"
        height="100%"
        @row-dblclick="onRowClick"
      >
        <el-table-column label="名称" min-width="220">
          <template #default="{ row }">
            <span class="file-icon">
              <el-icon>
                <Folder v-if="row.type === 'dir'" />
                <Link v-else-if="row.type === 'link'" />
                <Document v-else />
              </el-icon>
            </span>
            <span class="file-name" :class="{ 'is-dir': row.type === 'dir' }">{{ row.name }}</span>
          </template>
        </el-table-column>
        <el-table-column label="大小" width="100" align="right">
          <template #default="{ row }">
            {{ row.type === 'dir' ? '-' : formatSize(row.size) }}
          </template>
        </el-table-column>
        <el-table-column label="权限" width="110" prop="permission" />
        <el-table-column label="属主" width="110">
          <template #default="{ row }">{{ row.owner }}<span v-if="row.group">:{{ row.group }}</span></template>
        </el-table-column>
        <el-table-column label="修改时间" width="150" prop="modified" />
        <el-table-column label="操作" width="150" align="right">
          <template #default="{ row }">
            <el-button v-if="row.type !== 'dir'" link size="small" type="primary" @click="download(row)">下载</el-button>
            <el-button link size="small" @click="openRename(row)">重命名</el-button>
            <el-button link size="small" type="danger" @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
        <template #empty>
          <div class="table-empty">{{ loading ? '' : (entries.length === 0 ? '空目录' : '') }}</div>
        </template>
      </el-table>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowUp, Refresh, FolderAdd, UploadFilled, Folder, Document, Link, House } from '@element-plus/icons-vue'
import {
  listHostFiles, uploadHostFile, hostFileDownloadUrl, mkdirHostDir, deleteHostFile, renameHostFile
} from '@/api/host'

const props = defineProps({
  hostId: { type: Number, required: true },
})

const loading = ref(false)
const entries = ref([])
// 空字符串表示「让后端返回该 SSH 账号的家目录」：
// 直接用 / 会让非 root 账号（ubuntu / deploy）整页报权限错误
const currentPath = ref('')
// 首次加载拿到的家目录，供「主目录」按钮回跳
const homePath = ref('')

const canGoUp = computed(() => !!currentPath.value && currentPath.value !== '/')

const fileInput = ref(null)
const uploading = ref(false)
const uploadPercent = ref(0)
const uploadingName = ref('')

async function loadList() {
  if (!props.hostId) return
  loading.value = true
  try {
    const res = await listHostFiles(props.hostId, currentPath.value)
    if (res.code === 0) {
      entries.value = res.data?.entries || []
      const serverPath = res.data?.path || '/'
      // 第一次返回的目录就是账号家目录
      if (!homePath.value) homePath.value = serverPath
      currentPath.value = serverPath
    } else {
      ElMessage.error(dirErrorTip(res.message))
      entries.value = []
    }
  } catch (e) {
    ElMessage.error('读取目录失败')
    entries.value = []
  } finally {
    loading.value = false
  }
}

function onRowClick(row) {
  if (row.type === 'dir') enterDir(row.name)
}

function enterDir(name) {
  currentPath.value = currentPath.value.endsWith('/')
    ? currentPath.value + name
    : currentPath.value + '/' + name
  loadList()
}

function goUp() {
  if (!canGoUp.value) return
  const parts = currentPath.value.split('/').filter(Boolean)
  parts.pop()
  currentPath.value = '/' + parts.join('/')
  loadList()
}

function goHome() {
  if (!homePath.value) return
  currentPath.value = homePath.value
  loadList()
}

function download(row) {
  const full = currentPath.value.endsWith('/')
    ? currentPath.value + row.name
    : currentPath.value + '/' + row.name
  const a = document.createElement('a')
  a.href = hostFileDownloadUrl(props.hostId, full)
  a.download = row.name
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
}

function pickFile() {
  fileInput.value?.click()
}

async function onFilePicked(ev) {
  const files = Array.from(ev.target.files || [])
  ev.target.value = '' // 允许重复选同一文件
  if (!files.length) return

  for (const file of files) {
    uploading.value = true
    uploadingName.value = file.name
    uploadPercent.value = 0
    try {
      const res = await uploadHostFile(props.hostId, currentPath.value, file, (e) => {
        if (e.total) uploadPercent.value = Math.round((e.loaded / e.total) * 100)
      })
      if (res.code !== 0) {
        ElMessage.error(res.message || `上传 ${file.name} 失败`)
      }
    } catch {
      ElMessage.error(`上传 ${file.name} 失败`)
    }
  }
  uploading.value = false
  uploadPercent.value = 0
  uploadingName.value = ''
  await loadList()
}

async function openMkdir() {
  try {
    const { value } = await ElMessageBox.prompt('目录名（当前目录下创建）', '新建目录', {
      inputPlaceholder: '例如 logs',
      inputPattern: /^[^/\\]+$/,
      inputErrorMessage: '目录名不能包含 / 或 \\',
    })
    const full = currentPath.value.endsWith('/')
      ? currentPath.value + value
      : currentPath.value + '/' + value
    const res = await mkdirHostDir(props.hostId, full)
    if (res.code !== 0) return ElMessage.error(res.message || '创建失败')
    ElMessage.success('已创建')
    loadList()
  } catch { /* 用户取消 */ }
}

async function openRename(row) {
  try {
    const { value } = await ElMessageBox.prompt('新名称', '重命名', {
      inputValue: row.name,
      inputPattern: /^[^/\\]+$/,
      inputErrorMessage: '名称不能包含 / 或 \\',
    })
    const full = currentPath.value.endsWith('/')
      ? currentPath.value + row.name
      : currentPath.value + '/' + row.name
    const res = await renameHostFile(props.hostId, full, value)
    if (res.code !== 0) return ElMessage.error(res.message || '重命名失败')
    ElMessage.success('已重命名')
    loadList()
  } catch { /* 用户取消 */ }
}

async function remove(row) {
  const full = currentPath.value.endsWith('/')
    ? currentPath.value + row.name
    : currentPath.value + '/' + row.name
  const tip = row.type === 'dir'
    ? `确定删除空目录「${row.name}」？（只能删除空目录）`
    : `确定删除文件「${row.name}」？该操作不可恢复。`
  try {
    await ElMessageBox.confirm(tip, '删除确认', { type: 'warning' })
  } catch { return }

  const res = await deleteHostFile(props.hostId, full, row.type === 'dir' ? 'dir' : 'file')
  if (res.code !== 0) return ElMessage.error(res.message || '删除失败')
  ElMessage.success('已删除')
  loadList()
}

// 权限不足时给可操作的指引，而不是甩一句「读取目录失败」
function dirErrorTip(msg) {
  const text = String(msg || '')
  if (/permission denied|not permitted|denied/i.test(text)) {
    return '该账号无权访问这个目录，点工具栏的「主目录」回到自己的目录'
  }
  return text || '读取目录失败'
}

function formatSize(bytes) {
  const n = Number(bytes || 0)
  if (!n) return '0 B'
  if (n < 1024) return n + ' B'
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB'
  if (n < 1024 * 1024 * 1024) return (n / 1024 / 1024).toFixed(1) + ' MB'
  return (n / 1024 / 1024 / 1024).toFixed(2) + ' GB'
}

// 换主机时回到新账号的家目录
watch(() => props.hostId, () => {
  currentPath.value = ''
  homePath.value = ''
  loadList()
}, { immediate: true })
</script>

<style scoped>
.ops-files { display: flex; flex-direction: column; height: 100%; padding: 12px 16px; gap: 10px; }

.file-toolbar { display: flex; align-items: center; gap: 6px; }
.path-crumb {
  flex: 1;
  min-width: 0;
  padding: 5px 10px;
  border-radius: 6px;
  background: var(--bg-subtle);
  font-family: 'JetBrains Mono', Consolas, monospace;
  font-size: 12.5px;
  color: var(--text-secondary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.toolbar-right { display: flex; gap: 8px; flex-shrink: 0; }

.upload-progress { display: flex; align-items: center; gap: 10px; }
.upload-progress .el-progress { flex: 1; }
.upload-name { font-size: 12px; color: var(--text-secondary); max-width: 240px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.file-table { flex: 1; min-height: 0; }

.file-icon { margin-right: 6px; color: var(--text-muted); vertical-align: -2px; }
.file-name { font-size: 13px; }
.file-name.is-dir { font-weight: 600; cursor: pointer; }
.file-name.is-dir:hover { color: var(--primary); }

.table-empty { color: var(--text-muted); font-size: 13px; padding: 20px 0; }
</style>
