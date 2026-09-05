import request from './request'

// 各类可检索数据的规模
export function reindexStats() {
  return request.get('/reindex/stats')
}

// 触发重建；types 为 files / videos / cameras，留空表示全部
export function runReindex(data) {
  return request.post('/reindex', data)
}

// 查询最近一次重建任务的进度与结果
export function reindexStatus() {
  return request.get('/reindex/status')
}
