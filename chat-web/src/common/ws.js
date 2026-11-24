import { wsUrl, getToken } from './api'

// createWs 网关订阅客户端: 断线指数退避重连, 重连后自动重新 subscribe(带 lastSeq 补拉)
// opts: onMessage/onSync/onSubscribed/onStatus + onConvEvent(会话生命周期事件) / onTenantMessage(租户通道其他会话新消息)
export function createWs(scope, { onMessage, onSync, onSubscribed, onStatus, onConvEvent, onTenantMessage } = {}) {
  let ws = null
  let retry = 0
  let closedByUser = false
  let sub = null // {conversationId, lastSeq}
  let tenantWanted = false // 重连后自动重发 subscribeTenant
  let textPingTimer = null

  const open = () => {
    const token = getToken(scope)
    if (!token) return
    // 网关升级阶段校验 query token, 缺失会被直接拒 handshake
    ws = new WebSocket(wsUrl() + '?token=' + encodeURIComponent(token))
    onStatus && onStatus('connecting')
    ws.onopen = () => {
      retry = 0
      onStatus && onStatus('online')
      if (sub) send({ action: 'subscribe', conversationId: sub.conversationId, lastSeq: sub.lastSeq })
      if (tenantWanted) send({ action: 'subscribeTenant' })
      // 文本层 pong 兜底(协议层 ping 浏览器会自动回应, 此处再保活一次)
      textPingTimer = setInterval(() => send({ action: 'pong' }), 30000)
    }
    ws.onmessage = (e) => {
      let p
      try {
        p = JSON.parse(e.data)
      } catch {
        return
      }
      if (p.type === 'message') {
        if (sub && p.body && p.body.seq > sub.lastSeq) sub.lastSeq = p.body.seq
        onMessage && onMessage(p.body)
      } else if (p.type === 'sync') {
        if (sub && p.body && p.body.seq > sub.lastSeq) sub.lastSeq = p.body.seq
        onSync && onSync(p.body)
      } else if (p.type === 'subscribed') {
        onSubscribed && onSubscribed(p.body)
      } else if (p.type === 'conv_event') {
        onConvEvent && onConvEvent(p.body)
      } else if (p.type === 'tenant_message') {
        onTenantMessage && onTenantMessage(p.body)
      }
    }
    ws.onclose = ws.onerror = () => {
      clearInterval(textPingTimer)
      onStatus && onStatus('offline')
      if (closedByUser) return
      const delay = Math.min(1000 * 2 ** retry, 15000)
      retry += 1
      setTimeout(open, delay)
    }
  }

  const send = (obj) => {
    if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify(obj))
  }

  return {
    start: open,
    subscribe(conversationId, lastSeq = 0) {
      sub = { conversationId, lastSeq }
      send({ action: 'subscribe', conversationId, lastSeq: sub.lastSeq })
    },
    subscribeTenant() {
      tenantWanted = true
      send({ action: 'subscribeTenant' })
    },
    noteSeq(seq) {
      if (sub && seq > sub.lastSeq) sub.lastSeq = seq
    },
    close() {
      closedByUser = true
      clearInterval(textPingTimer)
      ws && ws.close()
    }
  }
}
