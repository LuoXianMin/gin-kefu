<template>
  <div class="wb-root">
    <!-- ===== MFA 登录 ===== -->
    <div v-if="!token" class="login-wrap">
      <div class="glass login-card">
        <div class="brand">{{ brandName }}</div>
        <p class="sub">在线客服系统 · 坐席端（账号密码 + 短信验证）</p>
        <input v-model="form.username" placeholder="客服账号" />
        <input v-model="form.password" type="password" placeholder="密码" />
        <div class="row">
          <input v-model="form.captcha" placeholder="图形验证码" />
          <img v-if="pic.b64" :src="pic.b64" class="captcha-img" title="点击刷新" @click="loadCaptcha" />
        </div>
        <div class="row">
          <input v-model="form.smsCode" placeholder="短信验证码" />
          <button class="btn-ghost" :disabled="!form.username || counting > 0" @click="sendSms">
            {{ counting > 0 ? counting + 's' : '发送短信' }}
          </button>
        </div>
        <button class="btn-primary" :disabled="loading || tenantClosed" @click="doLogin">{{ tenantClosed ? '商户已停用' : loading ? '登录中…' : '登 录' }}</button>
        <div v-if="devHint" class="dev-hint">{{ devHint }}</div>
        <div v-if="err" class="err">{{ err }}</div>
      </div>
    </div>

    <!-- ===== 工作台 ===== -->
    <div v-else class="wb-main">
      <div v-if="err" class="toast-err" @click="err = ''">{{ err }}</div>
      <!-- 左栏 -->
      <aside class="side-left glass">
        <div class="me-bar">
          <div class="avatar agent">{{ me.employeeNo || '客服' }}</div>
          <div class="me-info">
            <b>{{ me.employeeNo }}</b>
            <span>{{ (tenant && tenant.name ? tenant.name + ' · ' : '') + '名额 ' + activeCount + '/' + (me.maxActive || 8) }}</span>
          </div>
          <button class="btn-ghost small" @click="logout">退出</button>
        </div>
        <div class="tabs">
          <span :class="{ on: tab === 'pending' }" @click="tab = 'pending'">待接入 <i :class="{ bump: pendingBump }">{{ pending.length }}</i><em v-if="pendingUnreadTotal" class="unread-badge">{{ pendingUnreadTotal > 99 ? '99+' : pendingUnreadTotal }}</em></span>
          <span :class="{ on: tab === 'mine' }" @click="tab = 'mine'">我的会话 <i>{{ mine.length }}</i><em v-if="mineUnreadTotal" class="unread-badge">{{ mineUnreadTotal > 99 ? '99+' : mineUnreadTotal }}</em></span>
        </div>
        <div class="conv-list">
          <div v-for="c in shown" :key="c.id" class="conv-item" :class="{ on: cur && cur.id === c.id }" @click="openConv(c.id)">
            <div class="ct">
              {{ c.memberName || c.title || '会话 #' + c.id }}
              <em v-if="unreadMap[c.id]" class="unread-dot">{{ unreadMap[c.id] > 99 ? '99+' : unreadMap[c.id] }}</em>
            </div>
            <div class="cm">
              <span v-if="c.memberPhone" class="phone">{{ c.memberPhone }}</span>
              <span>{{ c.source }}</span>
              <span>{{ c.status === 'active' ? '进行中' : c.status === 'pending' ? '待接入' : '已结束' }}</span>
            </div>
            <button v-if="tab === 'pending' && c.status === 'pending'" class="btn-primary small" @click.stop="claim(c.id)">抢单接入</button>
          </div>
          <div v-if="!shown.length" class="empty">暂无{{ tab === 'pending' ? '待接入' : '进行中' }}会话</div>
        </div>
      </aside>

      <!-- 中栏: 会话 -->
      <section class="center glass">
        <template v-if="cur">
          <header class="conv-head">
            <div>
              <b>{{ cur.title || '会话 #' + cur.id }}</b>
              <div class="prev">
                <template v-if="cur.isFirstService">首次接待</template>
                <template v-else-if="cur.lastEmployeeNo">
                  上一接待: {{ cur.lastEmployeeNo }}
                  <span v-if="cur.firstAgentAt"> · {{ fmt(cur.firstAgentAt) }}</span>
                </template>
              </div>
            </div>
            <div class="ops">
              <span class="dot" :class="wsStatus"></span>
              <select v-model="toAgentId">
                <option value="0">转接给…</option>
                <option v-for="a in agents" :key="a.id" :value="a.id">{{ a.employeeNo }} {{ a.realName }}</option>
              </select>
              <button class="btn-ghost small" @click="doTransfer">转接</button>
              <button class="btn-ghost small danger" @click="doClose">结束</button>
            </div>
          </header>
          <div v-if="cur.status === 'closed'" class="closed-tip">该会话已结束（只读）</div>

          <div ref="scroller" class="msg-list">
            <ChatBubble v-for="m in messages" :key="m.messageId" :msg="m" viewer-scope="agent" />
          </div>

          <div class="composer">
            <div v-if="showEmoji" class="emoji-wrap"><EmojiPicker @pick="appendEmoji" /></div>
            <div class="bar">
              <button class="ico" @click="showEmoji = !showEmoji">😊</button>
              <button class="ico" @click="fileEl && fileEl.click()">🖼️</button>
              <input ref="fileEl" type="file" accept="image/*" hidden @change="onPickFile" />
              <textarea v-model="draft" rows="1" placeholder="Enter 发送 / Shift+Enter 换行" @keydown="onKey"></textarea>
              <button class="btn-primary send" :disabled="sending" @click="sendText">发送</button>
            </div>
            <div v-if="uploadStatus" class="up-status">{{ uploadStatus }}</div>
          </div>
        </template>
        <div v-else class="placeholder">← 从左侧选择或接入会话</div>
      </section>

      <!-- 右栏: 用户资料 -->
      <aside class="side-right glass">
        <template v-if="user">
          <h4>用户资料</h4>
          <div class="kv"><span>姓名</span><b>{{ user.name || '—' }}</b></div>
          <div class="kv"><span>电话</span><b>{{ user.phone || '—' }}</b></div>
          <div class="kv"><span>来源</span><b>{{ user.source || '—' }}</b></div>
          <div class="kv"><span>注册时间</span><b>{{ fmt(user.createdAt) }}</b></div>

          <h4>跨会话接待汇总</h4>
          <div class="kv"><span>历史会话数</span><b>{{ user.summary ? user.summary.totalConversations : 0 }}</b></div>
          <ul class="servers">
            <li v-for="s in (user.summary && user.summary.servers) || []" :key="s.employeeNo">
              <span class="avatar agent mini">{{ s.employeeNo }}</span>
              <span>首次接待 {{ fmt(s.servedAt) }}</span>
            </li>
          </ul>

          <h4>本会话接待轨迹</h4>
          <ul class="events">
            <li v-for="e in events" :key="e.id">
              <b>{{ actionName(e.action) }}</b>
              <span>{{ e.fromEmpNo ? e.fromEmpNo + ' → ' : '' }}{{ e.toEmpNo || '' }}</span>
              <span class="et">{{ fmt(e.eventTime) }}</span>
              <span v-if="e.reason" class="er">{{ e.reason }}</span>
            </li>
            <li v-if="!events.length" class="empty">暂无轨迹</li>
          </ul>
        </template>
        <div v-else class="empty">选择会话后显示</div>
      </aside>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, nextTick, watch, onMounted, onBeforeUnmount } from 'vue'
import { req, getToken, setToken, clearToken } from '../common/api'
import { sendTextMessage, sendImageMessage } from '../common/composer'
import { createWs } from '../common/ws'
import ChatBubble from '../common/ChatBubble.vue'
import EmojiPicker from '../common/EmojiPicker.vue'

const SCOPE = 'agent'
const token = ref(getToken(SCOPE))
const loading = ref(false)
const sending = ref(false)
const err = ref('')
const devHint = ref('')
const form = reactive({ username: '', password: '', captcha: '', smsCode: '' })
const pic = ref({ b64: '', id: '' })
const counting = ref(0)
const me = ref(JSON.parse(localStorage.getItem('cs_agent_me') || '{}'))

// B2B 多渠道: 工作台链接 ?t=<租户code> 指定商户, 持久化后登录请求带 tenantCode
const urlParams = new URLSearchParams(location.search)
const tenantCode = ref(urlParams.get('t') || urlParams.get('tenant') || urlParams.get('code') || localStorage.getItem('cs_agent_tenant') || '')
if (tenantCode.value) localStorage.setItem('cs_agent_tenant', tenantCode.value)
const tenant = ref(JSON.parse(localStorage.getItem('cs_agent_tenant_info') || 'null'))
const tenantClosed = computed(() => !!tenant.value && tenant.value.status !== 1)
const brandName = computed(() => (tenant.value && tenant.value.name ? tenant.value.name + ' · 客服工作台' : '客服工作台'))
async function loadTenant() {
  if (!tenantCode.value) return
  try {
    const data = await req(SCOPE, `/member/tenant?code=${encodeURIComponent(tenantCode.value)}`, { quiet: true })
    if (data && data.code) {
      tenant.value = data
      localStorage.setItem('cs_agent_tenant_info', JSON.stringify(data))
    }
  } catch {
    /* 商户信息拉取失败不阻断登录页 */
  }
}

const tab = ref('pending')
const pending = ref([])
const mine = ref([])
const agents = ref([])
const toAgentId = ref(0)
const cur = ref(null) // conversation
const user = ref(null) // sidebar
const events = ref([])
const messages = ref([])
const seqSet = new Set()
const lastSeq = ref(0)
const draft = ref('')
const showEmoji = ref(false)
const uploadStatus = ref('')
const wsStatus = ref('offline')
const scroller = ref(null)
const fileEl = ref(null)

const shown = computed(() => (tab.value === 'pending' ? pending.value : mine.value))
const activeCount = computed(() => mine.value.length)
const fmt = (t) => (t ? new Date(t).toLocaleString('zh-CN', { hour12: false }) : '')
const actionName = (a) => ({ assign: '接入', transfer: '转接', close: '结束' }[a] || a)

// ===== 租户级实时通道: 待接入数/会话状态自动刷新 + 其他会话未读提醒 =====
const unreadMap = reactive({}) // conversationId -> 未读数(前端累计, 打开会话即清零)
const unreadTotal = computed(() => Object.values(unreadMap).reduce((a, b) => a + b, 0))
// 角标归属按会话当前列表动态判定: 待接入会话的消息计入"待接入"页签, 我接待的计入"我的会话"(列表刷新后自动归位)
const mineIds = computed(() => new Set(mine.value.map((c) => c.id)))
const mineUnreadTotal = computed(() => Object.entries(unreadMap).reduce((s, [id, n]) => (mineIds.value.has(Number(id)) ? s + n : s), 0))
const pendingUnreadTotal = computed(() => unreadTotal.value - mineUnreadTotal.value)
const pendingBump = ref(false)
let gws = null
let refreshTimer = null
let bumpTimer = null

function scheduleRefresh() {
  if (refreshTimer) return
  refreshTimer = setTimeout(() => {
    refreshTimer = null
    refreshLists().catch(() => {})
  }, 250)
}

function startGlobalWs() {
  if (gws || !token.value) return
  gws = createWs(SCOPE, { onConvEvent: handleConvEvent, onTenantMessage: handleTenantMsg })
  gws.start()
  gws.subscribeTenant()
}

function handleConvEvent(ev) {
  if (!ev || !ev.op) return
  // created: 新访客排队 → 待接入角标实时变化; claimed/transferred/closed: 归属与列表变化 → 重拉
  scheduleRefresh()
  if (ev.op === 'created') {
    pendingBump.value = true
    clearTimeout(bumpTimer)
    bumpTimer = setTimeout(() => (pendingBump.value = false), 1600)
  }
  if (cur.value && cur.value.id === ev.conversationId) {
    if (ev.op === 'closed') cur.value.status = 'closed'
    if ((ev.op === 'claimed' || ev.op === 'transferred') && ev.agentId && me.value.id && ev.agentId !== me.value.id) {
      // 自己正在看的会话被他人接手/转走, 降级为只读提示(刷新列表后引导重开)
      err.value = '会话 #' + ev.conversationId + ' 已由 ' + (ev.agentEmployeeNo || '其他客服') + ' 接待'
    }
  }
  // 会话已脱离我的待接入/我的列表(被他人接入/转走/关闭) → 清除残留未读, 避免待接入角标挂着不归零
  const stillMine = (ev.op === 'claimed' || ev.op === 'transferred') && ev.agentId && ev.agentId === me.value.id
  if ((ev.op === 'closed' || ev.op === 'claimed' || ev.op === 'transferred') && !stillMine) {
    if (unreadMap[ev.conversationId]) delete unreadMap[ev.conversationId]
  }
}

function handleTenantMsg(m) {
  if (!m || !m.conversationId) return
  if (cur.value && m.conversationId === cur.value.id) return // 当前会话由会话通道处理
  if (m.senderType !== 'member') return // 只对访客消息计未读提醒(bot 欢迎语/客服自身消息不打扰)
  unreadMap[m.conversationId] = (unreadMap[m.conversationId] || 0) + 1
  playPing()
}

let audioCtx = null
function playPing() {
  try {
    audioCtx = audioCtx || new (window.AudioContext || window.webkitAudioContext)()
    if (audioCtx.state === 'suspended') audioCtx.resume()
    const o = audioCtx.createOscillator()
    const gn = audioCtx.createGain()
    o.connect(gn)
    gn.connect(audioCtx.destination)
    o.frequency.value = 880
    gn.gain.setValueAtTime(0.0001, audioCtx.currentTime)
    gn.gain.exponentialRampToValueAtTime(0.12, audioCtx.currentTime + 0.02)
    gn.gain.exponentialRampToValueAtTime(0.0001, audioCtx.currentTime + 0.35)
    o.start()
    o.stop(audioCtx.currentTime + 0.4)
  } catch {
    /* 浏览器未交互前禁止自动播放则静默, 角标仍生效 */
  }
}

watch(unreadTotal, (n) => {
  const base = tenant.value && tenant.value.name ? tenant.value.name + ' · 客服工作台' : '客服工作台'
  document.title = n > 0 ? `(${n > 99 ? '99+' : n}) ${base}` : base
})

let ws = null

function mergeMsg(m) {
  if (!m || seqSet.has(m.seq)) return
  seqSet.add(m.seq)
  messages.value.push(m)
  messages.value.sort((a, b) => a.seq - b.seq)
  if (m.seq > lastSeq.value) {
    lastSeq.value = m.seq
    reportRead()
  }
  scrollBottom()
}
function scrollBottom() {
  nextTick(() => {
    if (scroller.value) scroller.value.scrollTop = scroller.value.scrollHeight
  })
}
async function reportRead() {
  if (!cur.value || !lastSeq.value) return
  req(SCOPE, `/agent/conversations/${cur.value.id}/read`, { method: 'POST', body: { lastReadSeq: lastSeq.value }, quiet: true }).catch(() => {})
}

// ===== 登录 =====
async function loadCaptcha() {
  const data = await req(SCOPE, '/agent/captcha', { method: 'POST' })
  pic.value = { b64: 'data:image/png;base64,' + (data.picPath || ''), id: data.captchaId }
  if (data.devCaptcha) form.captcha = data.devCaptcha
}
async function sendSms() {
  err.value = ''
  try {
    const data = await req(SCOPE, '/agent/sms-code', { method: 'POST', body: { username: form.username, captcha: form.captcha, captchaId: pic.value.id, tenantCode: tenantCode.value } })
    if (data.devCode) {
      form.smsCode = data.devCode
      devHint.value = '开发环境短信码: ' + data.devCode
    } else devHint.value = '短信已发送'
    counting.value = 60
    const t = setInterval(() => (counting.value <= 0 ? clearInterval(t) : counting.value--), 1000)
    await loadCaptcha()
  } catch (e) {
    err.value = e.message
    await loadCaptcha()
  }
}
async function doLogin() {
  err.value = ''
  loading.value = true
  try {
    const data = await req(SCOPE, '/agent/login', { method: 'POST', body: { ...form, captchaId: pic.value.id, tenantCode: tenantCode.value } })
    setToken(SCOPE, data.token)
    token.value = data.token
    me.value = data.agent || {}
    localStorage.setItem('cs_agent_me', JSON.stringify(me.value))
    // 登录响应携带归属商户, 与接入链接互相印证
    if (data.tenant) {
      tenant.value = data.tenant
      localStorage.setItem('cs_agent_tenant_info', JSON.stringify(data.tenant))
      if (!tenantCode.value) {
        tenantCode.value = data.tenant.code
        localStorage.setItem('cs_agent_tenant', data.tenant.code)
      }
    }
    await refreshLists()
    startGlobalWs()
  } catch (e) {
    err.value = e.message
    await loadCaptcha()
  } finally {
    loading.value = false
  }
}
function logout() {
  clearToken(SCOPE)
  token.value = ''
  cur.value = null
  ws && ws.close()
  ws = null
  gws && gws.close()
  gws = null
  Object.keys(unreadMap).forEach((k) => delete unreadMap[k])
}

// ===== 会话 =====
async function refreshLists() {
  const p = await req(SCOPE, '/agent/conversations/pending')
  pending.value = p.list || []
  const m = await req(SCOPE, '/agent/conversations/mine')
  mine.value = m.list || []
}
async function loadAgents() {
  try {
    agents.value = (await req(SCOPE, '/agent/agents/transferable')) || []
  } catch {
    agents.value = []
  }
}
async function claim(id) {
  err.value = ''
  try {
    const c = await req(SCOPE, `/agent/conversations/${id}/claim`, { method: 'POST' })
    await refreshLists()
    await openConv(c.id)
  } catch (e) {
    err.value = e.message
  }
}
async function openConv(id) {
  const d = await req(SCOPE, `/agent/conversations/${id}`)
  cur.value = d.conversation
  user.value = d.user
  if (unreadMap[id]) unreadMap[id] = 0 // 进入会话即清零未读
  events.value = (await req(SCOPE, `/agent/conversations/${id}/events`)) || []
  messages.value = []
  seqSet.clear()
  lastSeq.value = 0
  const his = await req(SCOPE, `/agent/conversations/${id}/messages?limit=200`)
  ;(his.messages || []).forEach(mergeMsg)
  scrollBottom()
  ws && ws.close()
  ws = createWs(SCOPE, {
    onMessage: mergeMsg,
    onSync: mergeMsg,
    onStatus: (s) => (wsStatus.value = s)
  })
  ws.start()
  ws.subscribe(id, lastSeq.value)
  loadAgents()
}
async function doTransfer() {
  if (!toAgentId.value || !cur.value) return
  const reason = prompt('转接原因(可留空)') || ''
  try {
    await req(SCOPE, `/agent/conversations/${cur.value.id}/transfer`, { method: 'POST', body: { toAgentId: toAgentId.value, reason } })
    if (unreadMap[cur.value.id]) delete unreadMap[cur.value.id] // 转给他人后残留未读不再属于我
    cur.value = null
    user.value = null
    toAgentId.value = 0
    await refreshLists()
  } catch (e) {
    err.value = e.message
  }
}
async function doClose() {
  if (!cur.value) return
  if (!confirm('确认结束该会话?')) return
  try {
    await req(SCOPE, `/agent/conversations/${cur.value.id}/close`, { method: 'POST', body: { reason: 'agent_close' } })
    cur.value.status = 'closed'
    await refreshLists()
  } catch (e) {
    err.value = e.message
  }
}

// ===== 发送 =====
function onKey(e) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    sendText()
  }
}
function appendEmoji(ch) {
  draft.value += ch
}
async function sendText() {
  const text = draft.value.trim()
  if (!text || !cur.value || cur.value.status === 'closed') return
  sending.value = true
  try {
    const ack = await sendTextMessage(SCOPE, cur.value.id, text)
    if (!ack.deduplicated) mergeMsg({ messageId: ack.messageId, seq: ack.seq, senderType: 'agent', senderEmployeeNo: me.value.employeeNo, type: 'text', body: text, createdAt: ack.serverTime })
    draft.value = ''
    showEmoji.value = false
  } catch (e) {
    err.value = e.message
  } finally {
    sending.value = false
  }
}
async function onPickFile(ev) {
  const file = ev.target.files && ev.target.files[0]
  ev.target.value = ''
  if (!file || !cur.value || cur.value.status === 'closed') return
  try {
    const { body, ack } = await sendImageMessage(SCOPE, cur.value.id, file, (t) => (uploadStatus.value = t))
    mergeMsg({ messageId: ack.messageId, seq: ack.seq, senderType: 'agent', senderEmployeeNo: me.value.employeeNo, type: 'image', body, createdAt: ack.serverTime })
  } catch (e) {
    err.value = e.message
  } finally {
    setTimeout(() => (uploadStatus.value = ''), 1500)
  }
}

onMounted(async () => {
  await loadTenant()
  await loadCaptcha()
  if (token.value) {
    try {
      await refreshLists()
      startGlobalWs()
    } catch {
      logout()
    }
  }
})
onBeforeUnmount(() => {
  ws && ws.close()
  gws && gws.close()
  clearTimeout(refreshTimer)
  clearTimeout(bumpTimer)
})
</script>

<style scoped>
.wb-root {
  height: 100%;
  background: linear-gradient(150deg, #dfe8fb 0%, #eef0ff 45%, #e3f4ff 100%);
}
.toast-err {
  position: fixed;
  top: 14px;
  left: 50%;
  transform: translateX(-50%);
  background: var(--danger);
  color: #fff;
  font-size: 13px;
  padding: 8px 18px;
  border-radius: 999px;
  box-shadow: var(--shadow);
  z-index: 60;
  cursor: pointer;
  animation: msg-in 0.2s ease;
}
/* 登录 */
.login-wrap {
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
}
.login-card {
  width: 380px;
  padding: 30px 28px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.brand {
  font-size: 22px;
  font-weight: 800;
  background: linear-gradient(135deg, var(--brand), var(--accent));
  -webkit-background-clip: text;
  background-clip: text;
  color: transparent;
}
.sub {
  margin: 0;
  font-size: 12px;
  color: var(--ink-soft);
}
.login-card input {
  padding: 11px 13px;
  border: 1px solid var(--line);
  border-radius: 10px;
  font-size: 14px;
  outline: none;
  flex: 1;
  min-width: 0;
}
.login-card input:focus {
  border-color: var(--brand);
}
.row {
  display: flex;
  gap: 8px;
}
.captcha-img {
  height: 42px;
  border-radius: 8px;
  cursor: pointer;
  align-self: center;
}
.dev-hint {
  font-size: 12px;
  color: var(--brand-deep);
}
.err {
  font-size: 13px;
  color: var(--danger);
}
/* 工作台布局 */
.wb-main {
  height: 100%;
  display: grid;
  grid-template-columns: 280px 1fr 280px;
  gap: 14px;
  padding: 14px;
}
.side-left,
.center,
.side-right {
  min-height: 0;
  display: flex;
  flex-direction: column;
}
.me-bar {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 14px;
  border-bottom: 1px solid var(--line);
}
.me-info {
  display: flex;
  flex-direction: column;
  flex: 1;
}
.me-info span {
  font-size: 12px;
  color: var(--ink-soft);
}
.btn-ghost.small,
.btn-primary.small {
  padding: 5px 10px;
  font-size: 12px;
}
.btn-ghost.danger {
  color: var(--danger);
  background: rgba(242, 91, 91, 0.12);
}
.tabs {
  display: flex;
  border-bottom: 1px solid var(--line);
}
.tabs span {
  flex: 1;
  text-align: center;
  padding: 10px 0;
  font-size: 13px;
  cursor: pointer;
  color: var(--ink-soft);
}
.tabs span.on {
  color: var(--brand-deep);
  font-weight: 700;
  box-shadow: inset 0 -2px var(--brand);
}
.tabs i {
  font-style: normal;
  font-size: 11px;
  background: var(--chip);
  border-radius: 8px;
  padding: 1px 6px;
  margin-left: 3px;
}
.conv-list {
  flex: 1;
  overflow: auto;
  padding: 8px;
}
.conv-item {
  border-radius: 10px;
  padding: 10px 12px;
  cursor: pointer;
  margin-bottom: 6px;
}
.conv-item:hover {
  background: var(--chip);
}
.conv-item.on {
  background: var(--chip);
  outline: 1px solid var(--brand);
}
.conv-item .ct {
  font-size: 13px;
  font-weight: 600;
}
.conv-item .cm {
  display: flex;
  justify-content: space-between;
  font-size: 11px;
  color: var(--ink-soft);
  margin: 3px 0 6px;
}
.conv-item .cm .phone {
  color: var(--ink);
  letter-spacing: 0.5px;
}
/* 中栏 */
.center {
  overflow: hidden;
}
.conv-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 16px;
  border-bottom: 1px solid var(--line);
  gap: 10px;
}
.prev {
  font-size: 12px;
  color: var(--ink-soft);
  margin-top: 2px;
}
.ops {
  display: flex;
  align-items: center;
  gap: 8px;
}
.ops select {
  border: 1px solid var(--line);
  border-radius: 8px;
  padding: 6px 8px;
  font-size: 12px;
  outline: none;
  background: #fff;
}
.dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--danger);
}
.dot.online {
  background: var(--ok);
}
.dot.connecting {
  background: var(--warn);
}
.closed-tip {
  text-align: center;
  font-size: 12px;
  color: #a86b00;
  background: rgba(245, 166, 35, 0.14);
  padding: 5px;
}
.msg-list {
  flex: 1;
  overflow: auto;
  padding: 12px 16px;
}
.placeholder,
.empty {
  color: var(--ink-soft);
  font-size: 13px;
  text-align: center;
  padding: 24px 10px;
}
/* 输入区 */
.composer {
  position: relative;
  border-top: 1px solid var(--line);
  padding: 10px 12px;
  background: rgba(255, 255, 255, 0.5);
}
.emoji-wrap {
  position: relative;
  height: 0;
}
.emoji-wrap .emoji-pop {
  bottom: 60px;
}
.bar {
  display: flex;
  align-items: flex-end;
  gap: 8px;
}
.ico {
  background: transparent;
  font-size: 22px;
  padding: 4px;
  line-height: 1;
}
.bar textarea {
  flex: 1;
  resize: none;
  border: 1px solid var(--line);
  border-radius: 12px;
  padding: 10px 12px;
  font-size: 14px;
  max-height: 120px;
  outline: none;
}
.bar textarea:focus {
  border-color: var(--brand);
}
.up-status {
  font-size: 12px;
  color: var(--brand-deep);
  margin-top: 6px;
}
/* 右栏 */
.side-right {
  padding: 14px;
  overflow: auto;
}
.side-right h4 {
  margin: 14px 0 8px;
  font-size: 13px;
  color: var(--brand-deep);
}
.side-right h4:first-child {
  margin-top: 0;
}
.kv {
  display: flex;
  justify-content: space-between;
  font-size: 13px;
  padding: 4px 0;
  border-bottom: 1px dashed var(--line);
}
.kv span {
  color: var(--ink-soft);
}
.servers,
.events {
  list-style: none;
  margin: 0;
  padding: 0;
  font-size: 12px;
}
.servers li {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 5px 0;
  color: var(--ink-soft);
}
.avatar.mini {
  width: auto;
  height: auto;
  padding: 2px 8px;
  border-radius: 999px;
  font-size: 11px;
}
.events li {
  padding: 6px 0;
  border-bottom: 1px dashed var(--line);
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.events b {
  color: var(--ink);
}
.events .et {
  color: var(--ink-soft);
  width: 100%;
}
.events .er {
  color: var(--warn);
  width: 100%;
}
/* 未读角标与新待接入提醒 */
.unread-badge,
.unread-dot {
  font-style: normal;
  background: #f56c6c;
  color: #fff;
  border-radius: 9px;
  padding: 0 6px;
  font-size: 11px;
  line-height: 16px;
  margin-left: 5px;
  display: inline-block;
  min-width: 16px;
  text-align: center;
}
.tabs i.bump {
  animation: bump 0.8s ease-in-out 2;
  color: var(--warn);
}
@keyframes bump {
  0%,
  100% {
    transform: scale(1);
  }
  50% {
    transform: scale(1.5);
  }
}
@media (max-width: 1100px) {
  .wb-main {
    grid-template-columns: 240px 1fr;
  }
  .side-right {
    display: none;
  }
}
</style>
