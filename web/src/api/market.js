import request from './request'

// ---------- MCP 注册表 ----------
export function listMCPRegistry(params) {
  return request({ url: '/market/mcp-registry', method: 'get', params })
}
export function createMCPRegistry(data) {
  return request({ url: '/market/mcp-registry', method: 'post', data })
}
export function updateMCPRegistry(id, data) {
  return request({ url: `/market/mcp-registry/${id}`, method: 'put', data })
}
export function deleteMCPRegistry(id) {
  return request({ url: `/market/mcp-registry/${id}`, method: 'delete' })
}

// ---------- 技能库 ----------
export function listSkillLibrary(params) {
  return request({ url: '/market/skills', method: 'get', params })
}
export function createSkillLibrary(data) {
  return request({ url: '/market/skills', method: 'post', data })
}
export function updateSkillLibrary(id, data) {
  return request({ url: `/market/skills/${id}`, method: 'put', data })
}
export function deleteSkillLibrary(id) {
  return request({ url: `/market/skills/${id}`, method: 'delete' })
}

// ---------- 工具库 ----------
export function listToolLibrary(params) {
  return request({ url: '/market/tools', method: 'get', params })
}
export function createToolLibrary(data) {
  return request({ url: '/market/tools', method: 'post', data })
}
export function updateToolLibrary(id, data) {
  return request({ url: `/market/tools/${id}`, method: 'put', data })
}
export function deleteToolLibrary(id) {
  return request({ url: `/market/tools/${id}`, method: 'delete' })
}

// ---------- Agent 模板 ----------
export function listAgentTemplates(params) {
  return request({ url: '/market/templates', method: 'get', params })
}
export function getAgentTemplate(id) {
  return request({ url: `/market/templates/${id}`, method: 'get' })
}
export function createAgentTemplate(data) {
  return request({ url: '/market/templates', method: 'post', data })
}
export function updateAgentTemplate(id, data) {
  return request({ url: `/market/templates/${id}`, method: 'put', data })
}
export function deleteAgentTemplate(id) {
  return request({ url: `/market/templates/${id}`, method: 'delete' })
}

// ---------- 模型目录（含价格） ----------
export function listMarketModels(params) {
  return request({ url: '/market/models', method: 'get', params })
}
export function updateModelPrice(id, data) {
  return request({ url: `/market/models/${id}/price`, method: 'put', data })
}

// ---------- 模型路由规则 ----------
export function listRoutingRules() {
  return request({ url: '/market/routing', method: 'get' })
}
export function saveRoutingRule(data) {
  return request({ url: '/market/routing', method: 'post', data })
}
export function deleteRoutingRule(id) {
  return request({ url: `/market/routing/${id}`, method: 'delete' })
}

// ---------- 调用观测 ----------
export function listCallLogs(params) {
  return request({ url: '/ops/call-logs', method: 'get', params })
}
export function summaryCallLogs(params) {
  return request({ url: '/ops/call-logs/summary', method: 'get', params })
}
