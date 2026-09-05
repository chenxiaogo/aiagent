import axios from 'axios'
import { ElMessage } from 'element-plus'

const request = axios.create({
  baseURL: '/api',
  timeout: 30000
})

request.interceptors.request.use((config) => {
  const token = localStorage.getItem('aiagent_token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

request.interceptors.response.use(
  (response) => response.data,
  (error) => {
    const status = error.response?.status
    const msg = error.response?.data?.message || error.message || '请求失败'
    if (status === 401) {
      ElMessage.error('未授权或登录已过期')
      localStorage.removeItem('aiagent_token')
      window.location.href = '/#/login'
    } else {
      ElMessage.error(msg)
    }
    return Promise.reject(error)
  }
)

export default request