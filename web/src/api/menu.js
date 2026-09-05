import request from './request'

export function getMyMenus() {
  return request.get('/menus/my')
}

export function listMenus() {
  return request.get('/menus')
}

export function createMenu(data) {
  return request.post('/menus', data)
}

export function updateMenu(id, data) {
  return request.put(`/menus/${id}`, data)
}

export function saveMenuBtns(id, btns) {
  return request.put(`/menus/${id}/btns`, { btns })
}

export function deleteMenu(id) {
  return request.delete(`/menus/${id}`)
}
