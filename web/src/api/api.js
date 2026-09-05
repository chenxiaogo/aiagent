import request from './request'

export function listApis() {
  return request.get('/apis')
}

export function createApi(data) {
  return request.post('/apis', data)
}

export function updateApi(id, data) {
  return request.put(`/apis/${id}`, data)
}

export function deleteApi(id) {
  return request.delete(`/apis/${id}`)
}
