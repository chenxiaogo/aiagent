import request from './request'

export function listUsers() {
  return request.get('/users')
}

export function createUser(data) {
  return request.post('/users', data)
}

export function updateUser(id, data) {
  return request.put(`/users/${id}`, data)
}

export function resetPassword(id, password) {
  return request.put(`/users/${id}/password`, { password })
}

export function deleteUser(id) {
  return request.delete(`/users/${id}`)
}
