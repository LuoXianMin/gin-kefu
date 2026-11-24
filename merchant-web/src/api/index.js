import axios from 'axios'
import { ElMessage } from 'element-plus'

// 商户后台独立令牌域: cs token(scope=merchant), 不与平台 sysToken 混用
export const TOKEN_KEY = 'cs_merchant_token'

const service = axios.create({
  baseURL: '/api',
  timeout: 15000
})

service.interceptors.request.use((config) => {
  const token = localStorage.getItem(TOKEN_KEY)
  if (token) config.headers['x-token'] = token
  return config
})

service.interceptors.response.use(
  (res) => {
    const { code, msg, data } = res.data || {}
    if (code === 0) return data
    if (code === 7 && /令牌|登录|无效/.test(msg || '')) {
      localStorage.removeItem(TOKEN_KEY)
      window.location.hash = '#/login'
    }
    ElMessage.error(msg || '请求失败')
    return Promise.reject(new Error(msg))
  },
  (err) => {
    ElMessage.error(err.message || '网络异常')
    return Promise.reject(err)
  }
)

// ---- 认证(MFA: 图形码 + 短信码) ----
export const captcha = () => service.post('/merchant/captcha')
export const sendSmsCode = (data) => service.post('/merchant/sms-code', data)
export const login = (data) => service.post('/merchant/login', data)
export const logout = () => {
  localStorage.removeItem(TOKEN_KEY)
  window.location.hash = '#/login'
}

// ---- 客服账号 ----
export const agentList = (params) => service.get('/merchant/admin/agents', { params })
export const agentCreate = (data) => service.post('/merchant/admin/agents', data)
export const agentUpdate = (data) => service.put('/merchant/admin/agents', data)
export const agentStatus = (data) => service.put('/merchant/admin/agents/status', data)
export const agentResetPwd = (data) => service.put('/merchant/admin/agents/password', data)

// ---- 访客(用户端)账号 ----
export const memberList = (params) => service.get('/merchant/admin/members', { params })
export const memberCreate = (data) => service.post('/merchant/admin/members', data)
export const memberUpdate = (data) => service.put('/merchant/admin/members', data)
export const memberResetPwd = (data) => service.put('/merchant/admin/members/password', data)

// ---- H5 渠道 ----
export const channelGet = () => service.get('/merchant/admin/channel')
export const channelUpdate = (data) => service.put('/merchant/admin/channel', data)

// ---- 报表 ----
export const reportOverview = () => service.get('/merchant/admin/reports/overview')
export const reportAgents = () => service.get('/merchant/admin/reports/agents')

// ---- 审计 ----
export const auditList = (params) => service.get('/merchant/admin/audits', { params })

export default service
