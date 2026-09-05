import request from './request'

// 客户列表（带关联用户信息与授权计数）
export function listTenants() {
  return request.get('/tenants')
}

// 可作为客户的系统用户列表（含「已是客户」标记）
export function listTenantCandidates() {
  return request.get('/tenants/candidates')
}

// 从系统用户创建客户；不传 userId 时可按 name 建（兼容）
export function createTenant(data) {
  return request.post('/tenants', data)
}

export function deleteTenant(id) {
  return request.delete(`/tenants/${id}`)
}
