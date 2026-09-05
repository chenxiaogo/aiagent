import request from './request'

// 模型配置列表
export function getModelConfigList(modelType) {
  const params = modelType ? { modelType } : {}
  return request.get('/model-configs', { params })
}

// 模型配置详情
export function getModelConfig(id) {
  return request.get(`/model-configs/${id}`)
}

// 创建模型配置
export function createModelConfig(data) {
  return request.post('/model-configs', data)
}

// 更新模型配置
export function updateModelConfig(id, data) {
  return request.put(`/model-configs/${id}`, data)
}

// 删除模型配置
export function deleteModelConfig(id) {
  return request.delete(`/model-configs/${id}`)
}

// 激活模型
export function activateModelConfig(id) {
  return request.put(`/model-configs/${id}/activate`)
}
