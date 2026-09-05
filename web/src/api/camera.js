import request from './request'

// 摄像头事件列表
export function getCameraEvents(params) {
  return request.get('/camera/events', { params })
}

// 事件详情
export function getCameraEvent(id) {
  return request.get(`/camera/events/${id}`)
}

// 上传事件视频
export function uploadCameraEvent(formData) {
  return request.post('/camera/events', formData, {
    headers: { 'Content-Type': 'multipart/form-data' }
  })
}

// 触发分析（已分析的事件可重新分析）
export function processCameraEvent(id) {
  return request.post(`/camera/events/${id}/process`)
}

// 删除事件（连视频文件一起删除）
export function deleteCameraEvent(id) {
  return request.delete(`/camera/events/${id}`)
}

// 混合搜索
export function searchCameraEvents(data) {
  return request.post('/camera/search', data)
}

// 视频流地址
export function getCameraStreamUrl(id) {
  return `/api/camera/events/${id}/stream?token=${localStorage.getItem('aiagent_token')}`
}