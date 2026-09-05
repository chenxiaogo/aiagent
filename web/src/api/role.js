import request from './request'

export function listRoles() {
  return request.get('/roles')
}

export function getRole(id) {
  return request.get(`/roles/${id}`)
}

export function listPermissions() {
  return request.get('/roles/permissions')
}

export function createRole(data) {
  return request.post('/roles', data)
}

export function updateRole(id, data) {
  return request.put(`/roles/${id}`, data)
}

export function setRolePerms(id, permIds) {
  return request.put(`/roles/${id}/perms`, { permIds })
}

export function setRoleMenus(id, menuIds) {
  return request.put(`/roles/${id}/menus`, { menuIds })
}

export function setRoleMenuBtns(id, menuId, btnIds) {
  return request.put(`/roles/${id}/menus/${menuId}/btns`, { btnIds })
}

export function setRoleApis(id, apiIds) {
  return request.put(`/roles/${id}/apis`, { apiIds })
}

export function deleteRole(id) {
  return request.delete(`/roles/${id}`)
}
