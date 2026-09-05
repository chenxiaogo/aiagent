import request from './request'

// 智能体列表
export function getAgentList(params) {
  return request.get('/agents', { params })
}

// 智能体详情
export function getAgent(id) {
  return request.get(`/agents/${id}`)
}

// 创建智能体
export function createAgent(data) {
  return request.post('/agents', data)
}

// 更新智能体
export function updateAgent(id, data) {
  return request.put(`/agents/${id}`, data)
}

// 删除智能体
export function deleteAgent(id) {
  return request.delete(`/agents/${id}`)
}

// 拖拽排序：ids 为新顺序，start 为当前分页起始位
export function reorderAgents(ids, start = 0) {
  return request.post('/agents/reorder', { ids, start })
}

// 更新智能体状态
export function updateAgentStatus(id, status) {
  return request.put(`/agents/${id}/status`, { status })
}

// 预设问题列表
export function getPresetQuestions(agentId) {
  return request.get(`/agents/${agentId}/preset-questions`)
}

// 创建预设问题
export function createPresetQuestion(agentId, data) {
  return request.post(`/agents/${agentId}/preset-questions`, data)
}

// 更新预设问题
export function updatePresetQuestion(qid, data) {
  return request.put(`/agents/preset-questions/${qid}`, data)
}

// 删除预设问题
export function deletePresetQuestion(qid) {
  return request.delete(`/agents/preset-questions/${qid}`)
}

// ---------- 多模型绑定 ----------

// 已绑定模型 + 可选模型清单
export function getAgentModels(id) {
  return request.get(`/agents/${id}/models`)
}

// 整表保存模型绑定
export function saveAgentModels(id, models) {
  return request.put(`/agents/${id}/models`, { models })
}

// ---------- 运行资源与有效工具 ----------

export function getAgentResources(id) {
  return request.get(`/agents/${id}/resources`)
}

export function saveAgentResources(id, data) {
  return request.put(`/agents/${id}/resources`, data)
}

export function getAgentTools(id) {
  return request.get(`/agents/${id}/tools`)
}

export function saveToolLibMounts(id, toolIds) {
  return request.put(`/agents/${id}/tool-lib-mounts`, { toolIds })
}

// ---------- 对外交付：版本 ----------

// 版本列表（含草稿差异与发布校验）
export function getAgentVersions(id) {
  return request.get(`/agents/${id}/versions`)
}

// 发布新版本
export function publishAgentVersion(id, changelog) {
  return request.post(`/agents/${id}/versions`, { changelog })
}

// 回滚到指定版本
export function rollbackAgentVersion(id, releaseId) {
  return request.post(`/agents/${id}/versions/${releaseId}/rollback`)
}

// 轻量发布状态：当前版本、是否已发布、有无待发布改动及改动清单
export function getAgentReleaseStatus(id) {
  return request.get(`/agents/${id}/release-status`)
}

// ---------- 对外交付：发布与交付 ----------

// 交付总览（产品信息、端点、示例、工具、凭据、授权、用量）
export function getAgentDelivery(id) {
  return request.get(`/agents/${id}/delivery`)
}

// 更新产品信息与交付方式
export function updateAgentDelivery(id, data) {
  return request.put(`/agents/${id}/delivery`, data)
}

// ---------- 对外交付：客户授权 ----------

export function getAgentSubscriptions(id) {
  return request.get(`/agents/${id}/subscriptions`)
}

export function createAgentSubscription(id, data) {
  return request.post(`/agents/${id}/subscriptions`, data)
}

export function updateAgentSubscription(sid, data) {
  return request.put(`/agents/subscriptions/${sid}`, data)
}

export function deleteAgentSubscription(sid) {
  return request.delete(`/agents/subscriptions/${sid}`)
}

// ---------- 对外交付：访问凭据 ----------

export function getAgentClients(id) {
  return request.get(`/agents/${id}/clients`)
}

// 新建凭据，响应里的 plainKey 仅在创建时返回一次
export function createAgentClient(id, data) {
  return request.post(`/agents/${id}/clients`, data)
}

export function revokeAgentClient(id, cid) {
  return request.post(`/agents/${id}/clients/${cid}/revoke`)
}

export function deleteAgentClient(id, cid) {
  return request.delete(`/agents/${id}/clients/${cid}`)
}

// ---------- 对外交付：用量 ----------

export function getAgentUsage(id, days = 7) {
  return request.get(`/agents/${id}/usage`, { params: { days } })
}
