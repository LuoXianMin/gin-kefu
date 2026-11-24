// chat-web 公共请求层: 各端(member/agent)独立 cstoken 域, 不与平台 sysToken 混用
export const API_BASE = '/api'

const host = () => `${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.hostname}:8889/ws`

export const tokenKey = (scope) => `cs_${scope}_token`
export const getToken = (scope) => localStorage.getItem(tokenKey(scope)) || ''
export const setToken = (scope, t) => localStorage.setItem(tokenKey(scope), t)
export const clearToken = (scope) => localStorage.removeItem(tokenKey(scope))

export function uuid() {
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0
    return (c === 'x' ? r : (r & 0x3) | 0x8).toString(16)
  })
}

// req 统一 JSON 协议: code!==0 抛错(带 code/msg)
export async function req(scope, path, { method = 'GET', body, quiet = false } = {}) {
  const headers = {}
  const t = getToken(scope)
  if (t) headers['x-token'] = t
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  const res = await fetch(API_BASE + path, { method, headers, body: body === undefined ? undefined : JSON.stringify(body) })
  let json
  try {
    json = await res.json()
  } catch {
    throw new Error('服务响应异常(' + res.status + ')')
  }
  if (json.code !== 0) {
    const e = new Error(json.msg || '请求失败')
    e.code = json.code
    if (!quiet && json.code === 7 && /令牌|登录|无效/.test(json.msg || '')) clearToken(scope)
    throw e
  }
  return json.data
}

// uploadForm 附件上传(multipart), 不手动设 Content-Type
export async function uploadForm(scope, path, formData) {
  const headers = {}
  const t = getToken(scope)
  if (t) headers['x-token'] = t
  const res = await fetch(API_BASE + path, { method: 'POST', headers, body: formData })
  const json = await res.json()
  if (json.code !== 0) {
    const e = new Error(json.msg || '上传失败')
    e.code = json.code
    throw e
  }
  return json.data
}

// 图片消息 body 约定: JSON {fileId,fileName,size}; 展示时换取短时效签名 URL(5分钟)
export function parseImageBody(body) {
  try {
    const o = JSON.parse(body)
    return o && o.fileId ? o : null
  } catch {
    return null
  }
}

const fileUrlCache = new Map() // fileId -> {url, exp}; 提前 30s 视为过期
export async function fileViewUrl(scope, fileId) {
  const hit = fileUrlCache.get(fileId)
  if (hit && hit.exp - 30 > Date.now() / 1000) return hit.url
  const data = await req(scope, `/${scope === 'member' ? 'member' : 'agent'}/files/${fileId}/url`, { quiet: true })
  const full = API_BASE + data.url
  fileUrlCache.set(fileId, { url: full, exp: Math.floor(new Date(data.expireAt).getTime() / 1000) })
  return full
}

export { host as wsUrl }
