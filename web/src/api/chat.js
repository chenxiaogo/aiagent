import request from './request'

// WebSocket 连接（流式对话）
export function wsChatUrl() {
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
  const token = localStorage.getItem('aiagent_token')
  return `${protocol}//${location.host}/api/chat/ws?token=${token}`
}

// SSE 流式对话（兼容旧方案）
export function streamChat(data) {
  return fetch('/api/chat/stream', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${localStorage.getItem('aiagent_token')}`
    },
    body: JSON.stringify(data)
  })
}

export function sendChat(data) {
  return request.post('/chat/send', data)
}

// Agent 模式对话（多轮工具调用：search_camera / search_videos 真实检索）
export function agentChat(data) {
  return request.post('/chat/agent', data)
}

// params 可带 scopeType / scopeId：运维工作台按主机 / 主机组取会话
export function listSessions(agentId, params = {}) {
  return request.get('/chat/sessions', { params: { agentId, ...params } })
}

export function createSession(data) {
  return request.post('/chat/sessions', data)
}

export function deleteSession(id) {
  return request.delete(`/chat/sessions/${id}`)
}

export function togglePin(id) {
  return request.put(`/chat/sessions/${id}/pin`)
}

export function listMessages(id) {
  return request.get(`/chat/sessions/${id}/messages`)
}

// 人工确认：Agent 要执行危险操作（主机命令、写文件等）时，用户在聊天框提交允许 / 拒绝
// data: { approved: boolean, remember?: boolean, comment?: string }
export function resolveApproval(id, data) {
  return request.post(`/chat/approvals/${id}/resolve`, data)
}

// 取消息的免登录分享地址（返回 { url, fileName }）。
// 不能用带 token 的导出链接去分享——那等于把自己的登录令牌发给对方。
export function getExportUrl(id) {
  return request.get(`/chat/messages/${id}/export`, { params: { json: 1 } })
}