import request from './request'

// 视频列表
export function getVideoList(params) {
  return request.get('/videos', { params })
}

// 视频详情
export function getVideo(id) {
  return request.get(`/videos/${id}`)
}

// 上传视频
export function uploadVideo(formData, onProgress) {
  return request.post('/videos/upload', formData, {
    headers: { 'Content-Type': 'multipart/form-data' },
    onUploadProgress: onProgress
  })
}

// 更新视频
export function updateVideo(id, data) {
  return request.put(`/videos/${id}`, data)
}

// 删除视频
export function deleteVideo(id) {
  return request.delete(`/videos/${id}`)
}

// 视频场景列表
export function getVideoScenes(id) {
  return request.get(`/videos/${id}/scenes`)
}

// 重新处理
export function reprocessVideo(id) {
  return request.post(`/videos/${id}/reprocess`)
}

// 视频语义搜索
export function searchVideos(data) {
  return request.post('/videos/search', data)
}

// 获取视频流地址
export function getVideoStreamUrl(id) {
  return `/api/videos/${id}/stream?token=${localStorage.getItem('aiagent_token')}`
}

// 获取帧截图地址
export function getFrameUrl(id, time) {
  return `/api/videos/${id}/frame/${time}?token=${localStorage.getItem('aiagent_token')}`
}
