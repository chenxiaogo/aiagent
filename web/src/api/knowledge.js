import request from './request'

// 知识库列表（按当前用户权限返回；agentId 非空时按智能体隔离）
export function listKnowledgeBases(params) {
  return request.get('/knowledge', { params })
}

export function getKnowledgeBase(id) {
  return request.get(`/knowledge/${id}`)
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

export function listKnowledgeFiles(id) {
  return request.get(`/knowledge/${id}/files`)
}
