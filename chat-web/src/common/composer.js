import { req, uploadForm, uuid } from './api'

// 图片消息公共流程: 选择 -> 上传 -> 轮询扫描状态 -> clean 后发送 image 消息
// onStatus(text) 回显进度; 返回 {fileId, ack}
export async function sendImageMessage(scope, convId, file, onStatus = () => {}) {
  onStatus('上传中…')
  const fd = new FormData()
  fd.append('file', file)
  const info = await uploadForm(scope, `/${scope}/conversations/${convId}/files`, fd)
  if (info.scanStatus === 'infected') throw new Error('文件被安全扫描拦截, 发送失败')
  if (info.scanStatus !== 'clean') {
    onStatus('安全扫描中…')
    let cur = info
    for (let i = 0; i < 30 && cur.scanStatus !== 'clean'; i++) {
      await new Promise((r) => setTimeout(r, 1000))
      cur = await req(scope, `/${scope}/files/${info.id}`, { quiet: true })
      if (cur.scanStatus === 'infected') throw new Error('文件未通过安全扫描, 发送失败')
    }
    if (cur.scanStatus !== 'clean') throw new Error('安全扫描超时, 请稍后重试')
  }
  onStatus('发送中…')
  const body = JSON.stringify({ fileId: info.id, fileName: info.fileName, size: info.size })
  const ack = await req(scope, `/${scope}/conversations/${convId}/messages`, {
    method: 'POST',
    body: { clientMsgId: uuid(), type: 'image', body }
  })
  // body 一并回传, 供本地气泡即时渲染
  return { fileId: info.id, body, ack }
}

// 文本消息发送, 返回 ack
export async function sendTextMessage(scope, convId, text) {
  return req(scope, `/${scope}/conversations/${convId}/messages`, {
    method: 'POST',
    body: { clientMsgId: uuid(), type: 'text', body: text }
  })
}
