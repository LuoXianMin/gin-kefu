// e2e-notify.mjs — 会话实时事件链路验收
// 覆盖: ①客服租户通道订阅 ②新会话 created 事件(待接入实时) ③claim 后 h5 收到 claimed(自动转已接入)
//       ④member 消息经租户通道以 tenant_message 送达未订阅该会话的客服(未读提醒数据源) ⑤close 事件
// 依赖: node>=21(内置 WebSocket)、docker compose 全栈(8888/8889)、dev 短信自动回填
import { execSync } from 'node:child_process'

const BASE = 'http://127.0.0.1:8888'
const WS = 'ws://127.0.0.1:8889/ws'

let pass = 0
let fail = 0
const ok = (m) => { pass++; console.log('  \u2705', m) }
const bad = (m, d) => { fail++; console.log('  \u274c', m, '->', d ?? '') }

async function jpost(path, body, token) {
  const r = await fetch(BASE + path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...(token ? { 'x-token': token } : {}) },
    body: JSON.stringify(body || {})
  })
  const t = await r.text()
  try {
    return JSON.parse(t)
  } catch {
    throw new Error(`${path} 非JSON响应 ${r.status}: ${t.slice(0, 120)}`)
  }
}
async function jget(path, token) {
  const r = await fetch(BASE + path, { headers: token ? { 'x-token': token } : {} })
  return r.json()
}
function clearKeys(pattern) {
  try {
    const ks = execSync(`docker exec gva-redis redis-cli --scan --pattern '${pattern}'`).toString().trim()
    if (ks) execSync(`echo "${ks.replace(/\n/g, ' ')}" | xargs docker exec gva-redis redis-cli DEL`)
  } catch { /* 本地无 docker 时忽略 */ }
}
async function agentLogin(user) {
  clearKeys('cs:sms:last:*')
  clearKeys('cs:rl:*login*')
  let cr = await jpost('/agent/captcha')
  const sms = await jpost('/agent/sms-code', { username: user, captcha: cr.data.devCaptcha, captchaId: cr.data.captchaId })
  cr = await jpost('/agent/captcha')
  const res = await jpost('/agent/login', { username: user, password: '123456', captcha: cr.data.devCaptcha, captchaId: cr.data.captchaId, smsCode: sms.data.devCode })
  if (!res.data?.token) throw new Error(user + ' 登录失败: ' + JSON.stringify(res))
  return res.data
}

class WsHandle {
  constructor(name) {
    this.name = name
    this.frames = []
    this.waiters = []
  }
  connect(token) {
    return new Promise((resolve, reject) => {
      this.ws = new WebSocket(`${WS}?token=${encodeURIComponent(token)}`)
      this.ws.onopen = () => resolve(this)
      this.ws.onerror = (e) => reject(new Error(this.name + ' ws error ' + e.message))
      this.ws.onmessage = (e) => {
        let p
        try { p = JSON.parse(e.data) } catch { return }
        this.frames.push(p)
        this.waiters = this.waiters.filter((w) => {
          if (w.pred(p)) {
            w.resolve(p)
            return false
          }
          return true
        })
      }
    })
  }
  send(obj) { this.ws.send(JSON.stringify(obj)) }
  waitFor(pred, ms = 8000, desc = '') {
    const hit = this.frames.find(pred)
    if (hit) return Promise.resolve(hit)
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error(`超时未收到(${this.name}${desc ? ': ' + desc : ''})`)), ms)
      this.waiters.push({ pred: (p) => !!pred(p), resolve: (p) => { clearTimeout(timer); resolve(p) } })
    })
  }
  close() { try { this.ws.close() } catch { /* noop */ } }
}

function mkWs(name) {
  const h = new WsHandle(name)
  // 覆盖 waiter 结算逻辑: find 先行, 再挂监听
  h.ws = null
  return h
}

const convEvent = (op, convId) => (p) => p.type === 'conv_event' && p.body?.op === op && (!convId || p.body.conversationId === convId)
const msgOf = (convId) => (p) => (p.type === 'message' || p.type === 'sync') && p.body?.conversationId === convId
const tenantMsgOf = (convId) => (p) => p.type === 'tenant_message' && p.body?.conversationId === convId

async function main() {
  console.log('[1] 登录 member001 / agent001')
  const mres = await jpost('/member/login', { username: 'member001', password: '123456', tenantCode: 'demo' })
  if (!mres.data?.token) { bad('member登录', JSON.stringify(mres).slice(0, 160)); return }
  const MT = mres.data.token
  ok('member登录')
  const ainfo = await agentLogin('agent001')
  const AT = ainfo.token
  ok('agent001登录(MFA)')

  // 清理旧会话保证本轮 fresh pending(当值者可能不是 agent001, 轮试 claim/close)
  const a2info = await agentLogin('agent002')
  const tryClose = async (id, tok) => {
    await jpost(`/agent/conversations/${id}/claim`, {}, tok).catch(() => {})
    const c = await jpost(`/agent/conversations/${id}/close`, { reason: 'notify_e2e_reset' }, tok)
    return c.code === 0
  }
  let c0 = await jpost('/member/conversations', { title: 'NOTIFY-E2E', source: 'H5' }, MT)
  if (c0.data?.id && c0.data.status !== 'closed') {
    const id0 = c0.data.id
    const done = (await tryClose(id0, AT)) || (await tryClose(id0, a2info.token))
    if (!done) { bad('清理旧会话' + id0, '无法结束, 请先人工处理'); return }
    c0 = await jpost('/member/conversations', { title: 'NOTIFY-E2E', source: 'H5' }, MT)
  }
  const convId = c0.data?.id
  if (!convId || c0.data.status !== 'pending') { bad('准备新会话', JSON.stringify(c0).slice(0, 200)); return }

  console.log('[2] 建立 WS 连接')
  const aw = mkWs('agent001')
  await aw.connect(AT)
  aw.send({ action: 'subscribeTenant' })
  await aw.waitFor((p) => p.type === 'tenant_subscribed', 5000, 'tenant_subscribed')
  ok('客服租户通道订阅成功')
  const mw = mkWs('member001')
  await mw.connect(MT)
  mw.send({ action: 'subscribe', conversationId: convId, lastSeq: 0 })
  await mw.waitFor((p) => p.type === 'subscribed', 5000, 'subscribed')
  ok('用户会话通道订阅成功')

  console.log('[3] created 事件(此前已建的会话无事件, 重建一个验证)')
  // 结束当前会话并新建, 触发 created(pending 会话需先 claim 拿归属才能 close)
  await jpost(`/agent/conversations/${convId}/claim`, {}, AT)
  await jpost(`/agent/conversations/${convId}/close`, { reason: 'notify_e2e_next' }, AT)
  const c1 = await jpost('/member/conversations', { title: 'NOTIFY-E2E-2', source: 'H5' }, MT)
  convId2 = c1.data?.id
  if (!convId2) { bad('新建会话2', JSON.stringify(c1).slice(0, 200)); return }
  const created = await aw.waitFor(convEvent('created', convId2), 8000)
  ok(`租户通道收到 created 事件(会话${convId2}, 待接入数应+1) body=${JSON.stringify(created.body)}`)

  console.log('[4] claim → 用户端收到 claimed(待接入→已接入)')
  mw.send({ action: 'subscribe', conversationId: convId2, lastSeq: 0 })
  await mw.waitFor((p) => p.type === 'subscribed' && p.body?.conversationId === convId2, 5000)
  const claim = await jpost(`/agent/conversations/${convId2}/claim`, {}, AT)
  if (claim.code !== 0) { bad('抢单', JSON.stringify(claim).slice(0, 160)); return }
  const claimed = await mw.waitFor(convEvent('claimed', convId2), 8000)
  ok(`用户端收到 claimed 事件 agentEmployeeNo=${claimed.body.agentEmployeeNo}`)
  const claimedAgent = await aw.waitFor(convEvent('claimed', convId2), 8000)
  ok('客服租户通道同步收到 claimed(列表/待接入实时刷新源)')

  console.log('[5] member 发消息 → 未订阅该会话的客服连接收 tenant_message(未读提醒数据源)')
  const msgRes = await jpost(`/member/conversations/${convId2}/messages`, { clientMsgId: 'notify-' + Date.now(), type: 'text', body: '实时通知验证消息' }, MT)
  if (msgRes.code !== 0) { bad('member发消息', JSON.stringify(msgRes).slice(0, 160)); return }
  const tm = await aw.waitFor(tenantMsgOf(convId2), 8000)
  ok(`租户通道收到 tenant_message senderType=${tm.body.senderType} body=${tm.body.body}`)
  
  console.log('[6] close → 用户端收到 closed')
  await jpost(`/agent/conversations/${convId2}/close`, { reason: 'notify_e2e_done' }, AT)
  const closed = await mw.waitFor(convEvent('closed', convId2), 8000)
  ok('用户端收到 closed 事件')
  const closedAgent = await aw.waitFor(convEvent('closed', convId2), 8000)
  ok('客服租户通道同步收到 closed 事件')

  aw.close()
  mw.close()
}

let convId2 = 0
main()
  .catch((e) => bad('流程异常中断', e.message))
  .finally(() => {
    console.log(`\n结果: ${pass} 通过 / ${fail} 失败`)
    process.exit(fail ? 1 : 0)
  })
