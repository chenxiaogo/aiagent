import request from './request'

// ---------- MCP 服务器（Agent 接入的外部 MCP） ----------

export function listMcpServers(agentId) {
  return request.get(`/agents/${agentId}/mcp`)
}

export function createMcpServer(agentId, data) {
  return request.post(`/agents/${agentId}/mcp`, data)
}

export function updateMcpServer(id, data) {
  return request.put(`/agents/mcp/${id}`, data)
}

export function deleteMcpServer(id) {
  return request.delete(`/agents/mcp/${id}`)
}

// 测试连通性并列出该 MCP Server 提供的工具
export function testMcpServer(id) {
  return request.post(`/agents/mcp/${id}/test`)
}

// 从平台 MCP 注册表导入（支持批量，按 registryId 幂等）
export function importMcpFromRegistry(agentId, data) {
  return request.post(`/agents/${agentId}/mcp/import`, data)
}

// ---------- 技能 Skills ----------

export function listSkills(agentId) {
  return request.get(`/agents/${agentId}/skills`)
}

export function createSkill(agentId, data) {
  return request.post(`/agents/${agentId}/skills`, data)
}

export function updateSkill(id, data) {
  return request.put(`/agents/skills/${id}`, data)
}

export function deleteSkill(id) {
  return request.delete(`/agents/skills/${id}`)
}
