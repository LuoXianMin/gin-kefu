// 验收: 未读提醒定向投递(有当值者只提醒当值客服; 待接入提醒全租户; agent/bot 消息不打扰)
// 场景: member001(tenant1) + agent001/agent002 双客服全局租户通道
import { execSync } from 'node:child_process'

const BASE = 'http://127.0.0.1:8888'
const WS = 'ws://127.0.0.1:8889/ws'
let pass = 0
let fail = 0
const ok = (m) => { pass++; console.log('  ✅', m) }
const bad = (m, d) => { fail++; console.log('  ❌', m, d ?? '') }

const jpost = async (path, body, token) => {
  const r = await fetch(BASE + path, { method: 'POST', headers: { 'Content-Type': 'application/json', ...(token ? { 'x-token': token } : {}) }, body: JSON.stringify(body || {}) })
  return r.json()
}
const uuid = () => execSync('python3 -c "import uuid;print(uuid.uuid4())"').toString().trim()

async function agentLogin(username) {
  execSync(`docker exec gva-redis sh -c "redis-cli --scan --pattern 'cs:sms:last:*' | xargs -r redis-cli DEL"`)
  execSync(`docker exec gva-redis sh -c "redis-cli --scan --pattern 'cs:rl:*login*' | xargs -r redis-cli DEL"`)
  let cr = await jpost('/agent/captcha')
  const sms = await jpost('/agent/sms-code', { username, captcha: cr.data.devCaptcha, captchaId: cr.data.captchaId })
  cr = await jpost('/agent/captcha')
  const r = await jpost('/agent/login', { username, password: '123456', captcha: cr.data.devCaptcha, captchaId: cr.data.captchaId, smsCode: sms.data.devCode })
  if (!r.data?.token) throw new Error('agent 登录失败: ' + JSON.stringify(r))
  return r.data.token
}

class WsHandle {
  constructor(token, label) {
    this.label = label
    this.frames = []
    this.ws = new WebSocket(WS + '?token=' + token)
    this.ws.onmessage = (e) => this.frames.push(JSON.parse(e.data))
  }
  get opened() { return new Promise((r) => (this.ws.onopen = r)) }
  subTenant() { this.ws.send(JSON.stringify({ action: 'subscribeTenant' })) }
  sub(convId) { this.ws.send(JSON.stringify({ action: 'subscribe', conversationId: convId, lastSeq: 0 })) }
  tenantMsgs() { return this.frames.filter((f) => f.type === 'tenant_message').map((f) => f.body) }
  close() { try { this.ws.close() } catch {} }
}
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

async function freshConversation(MT, AT1, AT2) {
  let cv = await jpost('/member/conversations', { title: 'TARGETED-T', source: 'H5' }, MT)
  if (cv.data?.status !== 'closed') {
    const id = cv.data.id
    for (const at of [AT1, AT2]) {
      await jpost(`/agent/conversations/${id}/claim`, {}, at)
      const r = await jpost(`/agent/conversations/${id}/close`, { reason: 'targeted_reset' }, at)
      if (r.code === 0) break
    }
    cv = await jpost('/member/conversations', { title: 'TARGETED-T', source: 'H5' }, MT)
  }
  return cv.data
}

async function main() {
  console.log('[1] 登录三端')
  const m = await jpost('/member/login', { username: 'member001', password: '123456', tenantCode: 'demo' })
  if (!m.data?.token) return bad('member 登录失败', JSON.stringify(m))
  const MT = m.data.token
  const AT1 = await agentLogin('agent001')
  const AT2 = await agentLogin('agent002')
  ok('member001/agent001/agent002 登录成功')

  console.log('[2] 场景A: 会话已由 agent001 接待, 访客消息只定向提醒 agent001')
  const g1 = new WsHandle(AT1, 'agent001-global')
  const g2 = new WsHandle(AT2, 'agent002-global')
  await Promise.race([g1.opened, sleep(8000)])
  await Promise.race([g2.opened, sleep(8000)])
  g1.subTenant()
  g2.subTenant()
  await sleep(300)
  let conv = await freshConversation(MT, AT1, AT2)
  await jpost(`/agent/conversations/${conv.id}/claim`, {}, AT1)
  await sleep(300) // 等 claimed 事件帧先入缓存, 与 tenant_message 区分
  g1.frames.length = 0
  g2.frames.length = 0
  await jpost(`/member/conversations/${conv.id}/messages`, { clientMsgId: uuid(), type: 'text', body: '定向消息A(只该打扰当值者)' }, MT)
  await sleep(2000)
  const t1 = g1.tenantMsgs().filter((x) => x.body === '定向消息A(只该打扰当值者)')
  const t2 = g2.tenantMsgs().filter((x) => x.body === '定向消息A(只该打扰当值者)')
  t1.length === 1 ? ok('当值 agent001 收到 tenant_message') : bad('agent001 未收到定向提醒', JSON.stringify(t1))
  t2.length === 0 ? ok('非当值 agent002 未被打扰(bug 修复点)') : bad('agent002 仍收到提醒', JSON.stringify(t2))
  const m2 = await jpost(`/agent/conversations/${conv.id}/messages`, { clientMsgId: uuid(), type: 'text', body: '客服回复A' }, AT1)
  await sleep(1500)
  const noise = [...g1.tenantMsgs(), ...g2.tenantMsgs()].filter((x) => x.body === '客服回复A')
  noise.length === 0 ? ok('客服自己发送的消息不触发租户提醒') : bad('agent 消息误触发提醒', JSON.stringify(noise))

  console.log('[3] 场景B: 待接入会话(无当值), 访客消息提醒全租户客服')
  await jpost(`/agent/conversations/${conv.id}/close`, { reason: 'round_done' }, AT1)
  conv = await freshConversation(MT, AT1, AT2)
  g1.frames.length = 0
  g2.frames.length = 0
  await jpost(`/member/conversations/${conv.id}/messages`, { clientMsgId: uuid(), type: 'text', body: '待接入消息B(该提醒所有人)' }, MT)
  await sleep(2000)
  const b1 = g1.tenantMsgs().some((x) => x.body === '待接入消息B(该提醒所有人)')
  const b2 = g2.tenantMsgs().some((x) => x.body === '待接入消息B(该提醒所有人)')
  b1 && b2 ? ok('pending 会话访客消息提醒了全体客服') : bad('pending 未接入提醒丢失', `agent001=${b1} agent002=${b2}`)
  const botNoise = [...g1.tenantMsgs(), ...g2.tenantMsgs()].filter((x) => x.senderType === 'bot')
  botNoise.length === 0 ? ok('bot 欢迎语不占用租户提醒通道') : bad('bot 消息误入提醒通道', JSON.stringify(botNoise))

  g1.close()
  g2.close()
  console.log(`\n结果: ${pass} 通过 / ${fail} 失败`)
  process.exit(fail ? 1 : 0)
}
main().catch((e) => bad('流程异常中断', e.stack)).finally(() => {})
