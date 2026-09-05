import request from './request'

export function listFiles(params) {
  return request.get('/files', { params })
}

export function uploadFiles(formData) {
  return request.post('/files/upload', formData, {
    headers: { 'Content-Type': 'multipart/form-data' }
  })
}

export function deleteFile(id) {
  return request.delete(`/files/${id}`)
}

export function reindexFile(id) {
  return request.post(`/files/${id}/reindex`)
}

// 查看文件切片（分块内容）
export function listFileChunks(id, params) {
  return request.get(`/files/${id}/chunks`, { params })
}

// 更新文件标签（覆盖式，空数组表示清空）
export function updateFileTags(id, tags) {
  return request.put(`/files/${id}/tags`, { tags })
}

export function listKnowledgeBases(params) {
  return request.get('/knowledge', { params })
}

export function createKnowledgeBase(data) {
  return request.post('/knowledge', data)
}

export function updateKnowledgeBase(id, data) {
  return request.put(`/knowledge/${id}`, data)
}

export function deleteKnowledgeBase(id) {
  return request.delete(`/knowledge/${id}`)
}