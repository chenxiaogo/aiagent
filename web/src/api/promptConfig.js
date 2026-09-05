import request from './request'

// Prompt 配置列表
export function getPromptConfigList(params) {
  return request.get('/prompt-configs', { params })
}

// Prompt 配置详情
export function getPromptConfig(id) {
  return request.get(`/prompt-configs/${id}`)
}

// 创建 Prompt 配置
export function createPromptConfig(data) {
  return request.post('/prompt-configs', data)
}

// 更新 Prompt 配置
export function updatePromptConfig(id, data) {
  return request.put(`/prompt-configs/${id}`, data)
}

// 删除 Prompt 配置
export function deletePromptConfig(id) {
  return request.delete(`/prompt-configs/${id}`)
}
