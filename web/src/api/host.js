import request from './request'

// ---------- 主机组 ----------

export function listHostGroups(params) {
  return request.get('/hosts/groups', { params })
}
export function createHostGroup(data) {
  return request.post('/hosts/groups', data)
}
export function updateHostGroup(id, data) {
  return request.put(`/hosts/groups/${id}`, data)
}
export function deleteHostGroup(id) {
  return request.delete(`/hosts/groups/${id}`)
}

// ---------- 主机 ----------

export function listHosts(params) {
  return request.get('/hosts', { params })
}
export function getHost(id) {
  return request.get(`/hosts/${id}`)
}
export function createHost(data) {
  return request.post('/hosts', data)
}
export function updateHost(id, data) {
  return request.put(`/hosts/${id}`, data)
}
export function deleteHost(id) {
  return request.delete(`/hosts/${id}`)
}

// ---------- 命令记录 ----------

export function listHostCommands(id, limit = 20) {
  return request.get(`/hosts/${id}/commands`, { params: { limit } })
}

// ---------- 操作审计（参考 1Shell auditService） ----------

export function listHostAudits(params = {}) {
  return request.get('/hosts/audit', { params })
}

// ---------- 文件管理（列目录 / 上传 / 下载 / 建目录 / 删除 / 重命名） ----------

export function listHostFiles(id, path) {
  return request.get(`/hosts/${id}/files`, { params: { path } })
}

// 下载走浏览器原生请求（流式），token 放 query：后端 Auth 中间件支持
export function hostFileDownloadUrl(id, path) {
  const token = localStorage.getItem('aiagent_token') || ''
  return `/api/hosts/${id}/files/download?path=${encodeURIComponent(path)}&token=${encodeURIComponent(token)}`
}

export function uploadHostFile(id, path, file, onProgress) {
  const form = new FormData()
  form.append('file', file)
  form.append('path', path)
  return request.post(`/hosts/${id}/files/upload`, form, {
    headers: { 'Content-Type': 'multipart/form-data' },
    timeout: 0, // 大文件不设超时
    onUploadProgress: onProgress,
  })
}

export function mkdirHostDir(id, path) {
  return request.post(`/hosts/${id}/files/mkdir`, { path })
}

// type: 'file' | 'dir'（目录只允许删空目录）
export function deleteHostFile(id, path, type = 'file') {
  return request.delete(`/hosts/${id}/files`, { params: { path, type } })
}

export function renameHostFile(id, filePath, newName) {
  return request.post(`/hosts/${id}/files/rename`, { path: filePath, newName })
}

// ---------- 主机角色（环境 / 用途分类，参考 1Shell host.role） ----------

export const HOST_ROLES = [
  { value: 'prod', label: '生产' },
  { value: 'test', label: '测试' },
  { value: 'dev', label: '开发' },
  { value: 'bastion', label: '堡垒机' },
  { value: 'other', label: '其他' },
]

export function hostRoleText(role) {
  return HOST_ROLES.find(r => r.value === role)?.label || '未分类'
}
