// scripts/bench_cs.mjs — 客服系统发消息/收消息 QPS 压测
// 设计: N 个 bench 访客账号各自限流额度内并行发送, 绕过单账号 60/min 的 CsRateLimit,
//       以测底层链路(gin -> redis/pgsql -> kafka -> storage-worker -> ws-gateway)真实容量;
//       每账号自带 WS 观察者订阅本会话, 统计端到端送达与积压排空速率。
// 运行: node scripts/bench_cs.mjs [账号数] [每账号消息数]   默认 30 / 55
const BASE = process.env.BENCH_BASE || 'http://127.0.0.1:8888'
const WS_BASE = process.env.BENCH_WS || 'ws://127.0.0.1:8889/ws'
const N = Number(process.argv[2] || 30)
const PER = Math.min(Number(process.argv[3] || 55), Number(process.env.BENCH_MAX_PER || 58))
const POOL = Math.min(N * PER, 200) // HTTP 并发池
const DRAIN_MS = 60_000

const jpost = async (url, body, token) => {
  const res = await fetch(BASE + url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...(token ? { 'x-token': token } : {}) },
    body: JSON.stringify(body || {})
  })
  const text = await res.text()
  try {
    return JSON.parse(text)
  } catch {
    throw new Error(`非JSON响应(${res.status}) @${url}: ${text.slice(0, 80)}`)
  }
}
// /member/login 限流按 IP 计(10/min), 多账号登录会撞; 压测环境每 8 个重置一次限流键
import { execSync } from 'node:child_process'
const resetLoginLimit = () => {
  try {
    const ks = execSync(`docker exec gva-redis redis-cli --scan --pattern 'cs:rl:*login*'`).toString().trim()
    if (ks) execSync(`echo '${ks.split('\n').join(' ')}' | xargs docker exec gva-redis redis-cli DEL`)
  } catch {}
}
// MFA 短信 per-identity 60s 冷却, 压测前清除
const resetSmsCooldown = () => {
  try {
    const ks = execSync(`docker exec gva-redis redis-cli --scan --pattern 'cs:sms:last:*'`).toString().trim()
    if (ks) execSync(`echo '${ks.split('\n').join(' ')}' | xargs docker exec gva-redis redis-cli DEL`)
  } catch {}
}
// BENCH_RAW=1: 发送期间每 400ms 清消息限流键, 解除 60/min/账号 业务限流, 测基础设施真实容量(仅压测环境)
const resetMsgLimit = () => {
  try {
    const ks = execSync(`docker exec gva-redis redis-cli --scan --pattern 'cs:rl:/member/conversations/:id/messages*'`).toString().trim()
    if (ks) execSync(`echo '${ks.split('\n').join(' ')}' | xargs -n 20 docker exec gva-redis redis-cli DEL`)
  } catch {}
}
const pct = (arr, p) => (arr.length ? arr.sort((a, b) => a - b)[Math.min(arr.length - 1, Math.floor(arr.length * p))] : -1)

// ===== MFA 登录(商户管理员) =====
async function merchantLogin(username, password) {
  resetSmsCooldown()
  let c = await jpost('/merchant/captcha', {})
  const s = await jpost('/merchant/sms-code', { username, captcha: c.data.devCaptcha, captchaId: c.data.captchaId })
  if (s.code !== 0) throw new Error('sms-code: ' + s.msg)
  c = await jpost('/merchant/captcha', {})
  const r = await jpost('/merchant/login', { username, password, captcha: c.data.devCaptcha, captchaId: c.data.captchaId, smsCode: s.data.devCode })
  if (r.code !== 0) throw new Error('merchant login: ' + r.msg)
  return r.data.token
}

async function memberLogin(username, password) {
  const r = await jpost('/member/login', { username, password })
  if (r.code !== 0) throw new Error('member login: ' + r.msg)
  return r.data.token
}

// ===== 准备 bench 账号 =====
const main = async () => {
  console.log(`== 准备 ${N} 个 bench 账号 (merchantadmin 建号, 幂等) ==`)
  const mtok = await merchantLogin('merchantadmin', '123456')
  const names = Array.from({ length: N }, (_, i) => 'bench_' + String(i).padStart(2, '0'))
  for (const [i, u] of names.entries()) {
    const r = await jpost('/merchant/admin/members', { username: u, password: 'bench123456', nickName: u, phone: '139' + String(10000000 + i).slice(0, 8), source: 'bench' }, mtok)
    if (r.code !== 0 && !/已存在|已被占用|duplicate/i.test(r.msg || '')) console.log(`  建号 ${u}: ${r.msg}`)
  }

  console.log('== 登录 + 建会话 + 挂 WS 观察者 ==')
  resetLoginLimit()
  const actors = []
  for (const [i, u] of names.entries()) {
    if (i > 0 && i % 8 === 0) resetLoginLimit()
    const token = await memberLogin(u, 'bench123456')
    const cv = await jpost('/member/conversations', { title: 'BENCH', source: 'bench' }, token)
    if (cv.code !== 0) throw new Error('建会话失败 ' + u + ': ' + cv.msg)
    const convId = cv.data.id || cv.data.conversationId
    const a = { u, token, convId, ws: null, recv: 0, arrived: new Map(), sendAt: new Map(), acks: [] }
    a.ws = new WebSocket(WS_BASE + '?token=' + encodeURIComponent(token))
    await new Promise((res, rej) => {
      a.ws.onopen = () => { a.ws.send(JSON.stringify({ action: 'subscribe', conversationId: convId, lastSeq: 0 })); res() }
      a.ws.onerror = rej
      setTimeout(() => rej(new Error('ws open timeout ' + u)), 5000)
    })
    a.ws.onmessage = (e) => {
      try {
        const p = JSON.parse(e.data)
        if (p.type === 'message' && p.body && p.body.messageId) {
          a.recv++
          a.arrived.set(p.body.messageId, Date.now())
        }
      } catch {}
    }
    actors.push(a)
  }
  console.log(`  ${actors.length} 个 actor 就绪`)

  // ===== 发送压测 =====
  const tasks = []
  for (const a of actors) {
    for (let j = 0; j < PER; j++) {
      tasks.push({ a, clientMsgId: `${a.u}-${j}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}` })
    }
  }
  console.log(`== 压测: ${tasks.length} 条消息, HTTP 并发池=${POOL}${process.env.BENCH_RAW ? ' [RAW:限流已临时解除]' : ''} ==`)
  const rawTimer = process.env.BENCH_RAW ? setInterval(resetMsgLimit, 400) : null
  let done = 0, errCnt = 0, inflight = 0, maxInflight = 0
  const errBreak = new Map() // 失败原因分布
  const bumpErr = (k) => errBreak.set(k, (errBreak.get(k) || 0) + 1)
  const rtt = []
  const t0 = Date.now()
  let cursor = 0
  const worker = async () => {
    while (cursor < tasks.length) {
      const t = tasks[cursor++]
      const s = Date.now()
      inflight++
      maxInflight = Math.max(maxInflight, inflight)
      try {
        const r = await jpost(`/member/conversations/${t.a.convId}/messages`, { clientMsgId: t.clientMsgId, type: 'text', body: 'bench ' + t.clientMsgId }, t.a.token)
        const e = Date.now()
        rtt.push(e - s)
        if (r.code === 0) {
          t.a.acks.push(r.data.messageId)
          t.a.sendAt.set(r.data.messageId, s)
        } else { errCnt++; bumpErr('code' + r.code + ':' + (r.msg || '')) }
      } catch (ex) {
        errCnt++
        bumpErr('ex:' + ex.message.slice(0, 60))
        rtt.push(Date.now() - s)
      }
      inflight--
      done++
      if (done % 500 === 0) console.log(`  已发 ${done}/${tasks.length}, err=${errCnt}, 用时 ${((Date.now() - t0) / 1000).toFixed(1)}s`)
    }
  }
  await Promise.all(Array.from({ length: POOL }, worker))
  if (rawTimer) clearInterval(rawTimer)
  const sendElapsed = (Date.now() - t0) / 1000
  const sent = tasks.length - errCnt

  // ===== 接收排空 =====
  console.log('== 等待 WS 送达排空(最长 60s) ==')
  const sendEnd = Date.now()
  const e2eLat = []
  const collect = () => {
    for (const a of actors) {
      for (const id of a.acks) {
        if (a.arrived.has(id) && !a.seen?.has(id)) {
          a.seen = a.seen || new Set()
          a.seen.add(id)
          e2eLat.push(a.arrived.get(id) - a.sendAt.get(id))
        }
      }
    }
  }
  let totalRecv = 0
  while (Date.now() - sendEnd < DRAIN_MS) {
    await new Promise((r) => setTimeout(r, 1000))
    collect()
    totalRecv = actors.reduce((s, a) => s + a.recv, 0)
    if (totalRecv >= sent) break
  }
  collect()
  totalRecv = actors.reduce((s, a) => s + a.recv, 0)
  const drainEnd = Math.max(...actors.flatMap((a) => [...a.arrived.values()]), Date.now())

  // ===== 汇总 =====
  const out = {
    配置: { 账号数: N, 每账号消息: PER, 总消息: tasks.length, HTTP并发池: POOL, 限流: '60/min/账号(路由级 CsRateLimit)' },
    发送侧: {
      成功: sent, 失败: errCnt, 耗时秒: +sendElapsed.toFixed(2),
      失败分布: Object.fromEntries(errBreak),
      聚合吞吐_QPS: +(sent / sendElapsed).toFixed(1),
      HTTP延迟ms: { p50: pct(rtt, 0.5), p95: pct(rtt, 0.95), p99: pct(rtt, 0.99), max: Math.max(...rtt) }
    },
    接收侧: {
      WS送达条数: totalRecv, 送达率: (100 * totalRecv / sent).toFixed(1) + '%',
      端到端延迟ms: { p50: pct(e2eLat, 0.5), p95: pct(e2eLat, 0.95), p99: pct(e2eLat, 0.99), max: e2eLat.length ? Math.max(...e2eLat) : -1 },
      排空耗时秒: +((drainEnd - sendEnd) / 1000).toFixed(1)
    }
  }
  console.log('\n===== 压测结果 =====')
  console.log(JSON.stringify(out, null, 2))
  actors.forEach((a) => a.ws.close())
  process.exit(0)
}

main().catch((e) => { console.error('压测失败:', e.message); process.exit(1) })
