import request from './request'

// 报告列表
export function getReportList(params) {
  return request.get('/reports', { params })
}

// 报告详情
export function getReport(id) {
  return request.get(`/reports/${id}`)
}

// 创建报告
export function createReport(data) {
  return request.post('/reports', data)
}

// 更新报告
export function updateReport(id, data) {
  return request.put(`/reports/${id}`, data)
}

// 删除报告
export function deleteReport(id) {
  return request.delete(`/reports/${id}`)
}

// 下载 HTML 报告
export function downloadReportHTML(id) {
  return request.get(`/reports/${id}/html`, { responseType: 'blob' })
}
