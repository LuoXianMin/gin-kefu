<template>
  <div class="h5-root">
    <!-- 未登录引导 -->
    <div v-if="!token" class="login-wrap">
      <div class="glass login-card">
        <div class="brand">{{ brandName }}</div>
        <p class="sub">{{ !tenantCode ? '未识别到商户渠道, 请通过商户客服链接(带 t=渠道编码)访问' : tenantClosed ? '该商户客服暂时停止服务, 请稍后再来' : '登录后即可与客服沟通, 支持文字、emoji 与图片。' }}</p>
        <input v-model="form.username" placeholder="账号 / 手机号" />
        <input v-model="form.password" type="password" placeholder="密码" />
        <button class="btn-primary" :disabled="loading || ssoPending || tenantClosed || !tenantCode" @click="doLogin">
          {{ ssoPending ? '自动登录中…' : loading ? '登录中…' : '登 录' }}
        </button>
        <div v-if="err" class="err">{{ err }}</div>
      </div>
    </div>

    <!-- 已登录: 会话聊天 -->
    <div v-else class="chat-wrap">
      <header class="chat-head">
        <div class="who">
          <span class="dot" :class="wsStatus"></span>
          <span>{{ agentLabel }}</span>
        </div>
        <button class="btn-ghost" @click="logout">退出</button>
      </header>

      <div v-if="windowLimited" class="tips">仅显示最近 7 天消息</div>
      <div v-if="conv && conv.status === 'closed'" class="tips warn">会话已结束，发送新消息将开启新会话</div>
      <div v-if="sysTip" class="tips">{{ sysTip }}</div>
      <div v-if="err" class="tips err-line">{{ err }}</div>

      <div ref="scroller" class="msg-list">
        <ChatBubble v-for="m in messages" :key="m.messageId" :msg="m" viewer-scope="member" />
      </div>

      <!-- 输入区 -->
      <div class="composer">
        <div v-if="showEmoji" class="emoji-wrap">
          <EmojiPicker @pick="appendEmoji" />
        </div>
        <div class="bar">
          <button class="ico" title="表情" @click="toggleEmoji">😊</button>
          <button class="ico" title="图片" @click="fileEl && fileEl.click()">🖼️</button>
          <input ref="fileEl" type="file" accept="image/*" hidden @change="onPickFile" />
          <textarea
            v-model="draft"
            rows="1"
            placeholder="输入消息…"
            @keydown.enter.exact.prevent="sendText"
          ></textarea>
          <button class="btn-primary send" :disabled="sending || !draft.trim()" @click="sendText">发送</button>
        </div>
        <div v-if="uploadStatus" class="up-status">{{ uploadStatus }}</div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, nextTick, onMounted, onBeforeUnmount } from 'vue'
import { req, getToken, setToken, clearToken } from '../common/api'
import { sendTextMessage, sendImageMessage } from '../common/composer'
import { createWs } from '../common/ws'
import ChatBubble from '../common/ChatBubble.vue'
import EmojiPicker from '../common/EmojiPicker.vue'

const SCOPE = 'member'
const token = ref(getToken(SCOPE))
const loading = ref(false)
const sending = ref(false)
const err = ref('')
const form = reactive({ username: '', password: '' })

// B2B 多渠道接入: URL 带商户标识(t|tenant|code), 持久化后刷新不丢
const urlParams = new URLSearchParams(location.search)
const tenantCode = ref(
  urlParams.get('t') || urlParams.get('tenant') || urlParams.get('code') || localStorage.getItem('cs_h5_tenant') || ''
)
if (tenantCode.value) localStorage.setItem('cs_h5_tenant', tenantCode.value)
// SSO 免密进入: 链接携带 ?user=<AES加密用户信息>, 后端解密自动建档登录
const ssoUser = urlParams.get('user') || ''
const ssoPending = ref(false)
const tenant = ref(null)
const tenantClosed = computed(() => !!tenant.value && tenant.value.status !== 1)
const brandName = computed(() => (tenant.value && tenant.value.name ? tenant.value.name + ' 在线客服' : '在线客服'))

const conv = ref(null)
const messages = ref([]) // 已按 seq 排序去重
const windowLimited = ref(false)
const seqSet = new Set()
const draft = ref('')
const showEmoji = ref(false)
const uploadStatus = ref('')
const wsStatus = ref('offline')
const sysTip = ref('')
let sysTipTimer = null
function showSysTip(t) {
  sysTip.value = t
  clearTimeout(sysTipTimer)
  sysTipTimer = setTimeout(() => (sysTip.value = ''), 6000)
}
const scroller = ref(null)
const fileEl = ref(null)
const lastSeq = ref(0)

const agentLabel = computed(() => {
  if (conv.value && conv.value.currentEmployeeNo) return conv.value.currentEmployeeNo + ' 号客服'
  return '客服(待接入)'
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
  if (!conv.value || !lastSeq.value) return
  await req(SCOPE, `/${SCOPE}/conversations/${conv.value.id}/read`, { method: 'POST', body: { lastReadSeq: lastSeq.value }, quiet: true }).catch(() => {})
}

async function openConversation() {
  const c = await req(SCOPE, `/${SCOPE}/conversations`, { method: 'POST', body: { source: localStorage.getItem('cs_h5_source') || 'h5' } })
  conv.value = c
  // 先建 WS 订阅再拉历史: 机器人欢迎语等新消息无论先落库还是先广播都能收到(网关按 lastSeq 补拉, 客户端 seq 去重)
  ws && ws.close()
  ws = createWs(SCOPE, {
    onMessage: mergeMsg,
    onSync: mergeMsg,
    onStatus: (s) => (wsStatus.value = s),
    // 会话生命周期事件: 客服接入/转接/结束时无需刷新页面, 待接入自动切换为已接入
    onConvEvent: (ev) => {
      if (!ev || !conv.value || ev.conversationId !== conv.value.id) return
      if (ev.op === 'claimed' || ev.op === 'transferred') {
        conv.value = { ...conv.value, status: 'active', currentEmployeeNo: ev.agentEmployeeNo || conv.value.currentEmployeeNo }
        showSysTip(ev.agentEmployeeNo ? `客服 ${ev.agentEmployeeNo} 已接入，请直接描述您的问题` : '客服已接入')
      } else if (ev.op === 'closed') {
        conv.value = { ...conv.value, status: 'closed' }
        showSysTip('客服已结束本次会话')
      }
    }
  })
  ws.start()
  ws.subscribe(c.id, lastSeq.value)
  const his = await req(SCOPE, `/${SCOPE}/conversations/${c.id}/messages?limit=50`)
  windowLimited.value = his.windowLimited
  ;(his.messages || []).forEach(mergeMsg)
  scrollBottom()
}

async function loadTenant() {
  if (!tenantCode.value) return
  try {
    tenant.value = await req(SCOPE, `/member/tenant?code=${encodeURIComponent(tenantCode.value)}`, { quiet: true })
  } catch {
    // 渠道码无效时静默, 交给登录时报错
  }
}

async function doLogin() {
  err.value = ''
  loading.value = true
  try {
    const data = await req(SCOPE, `/${SCOPE}/login`, {
      method: 'POST',
      body: { username: form.username, password: form.password, tenantCode: tenantCode.value }
    })
    setToken(SCOPE, data.token)
    token.value = data.token
    if (!tenant.value && data.tenant) tenant.value = { ...data.tenant, status: 1 }
    await openConversation()
  } catch (e) {
    err.value = e.message
  } finally {
    loading.value = false
  }
}

function logout() {
  clearToken(SCOPE)
  token.value = ''
  conv.value = null
  messages.value = []
  seqSet.clear()
  lastSeq.value = 0
  ws && ws.close()
  ws = null
}

function toggleEmoji() {
  showEmoji.value = !showEmoji.value
}
function appendEmoji(e) {
  draft.value += e
}

async function ensureOpen() {
  // 会话已结束时, 重新 CreateOrResume 开新会话(含 WS 重新订阅)
  if (!conv.value || conv.value.status !== 'closed') return
  seqSet.clear()
  messages.value = []
  lastSeq.value = 0
  await openConversation()
}

async function sendText() {
  const text = draft.value.trim()
  if (!text) return
  sending.value = true
  try {
    await ensureOpen()
    const ack = await sendTextMessage(SCOPE, conv.value.id, text)
    if (!ack.deduplicated) {
      mergeMsg({ messageId: ack.messageId, seq: ack.seq, senderType: 'member', type: 'text', body: text, createdAt: ack.serverTime })
    }
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
  if (!file) return
  ev.target.value = ''
  try {
    await ensureOpen()
    const { body, ack } = await sendImageMessage(SCOPE, conv.value.id, file, (t) => (uploadStatus.value = t))
    mergeMsg({ messageId: ack.messageId, seq: ack.seq, senderType: 'member', type: 'image', body, createdAt: ack.serverTime })
  } catch (e) {
    err.value = e.message
  } finally {
    setTimeout(() => (uploadStatus.value = ''), 1500)
  }
}

onMounted(async () => {
  loadTenant()
  if (token.value) {
    openConversation().catch(() => logout())
    return
  }
  // SSO: 有 user 参数且已有渠道 → 调后端解密建号+发令牌, 成功直接进会话; 失败回退手动登录卡片
  if (ssoUser && tenantCode.value) {
    ssoPending.value = true
    try {
      const data = await req(SCOPE, '/member/sso-login', {
        method: 'POST',
        body: { tenantCode: tenantCode.value, user: ssoUser }
      })
      setToken(SCOPE, data.token)
      token.value = data.token
      if (!tenant.value && data.tenant) tenant.value = { ...data.tenant, status: 1 }
      // 清掉地址栏密文, 避免分享/回退重复消费
      history.replaceState(null, '', location.pathname + '?t=' + encodeURIComponent(tenantCode.value))
      await openConversation()
    } catch (e) {
      err.value = '自动登录失败: ' + e.message
    } finally {
      ssoPending.value = false
    }
  }
})
onBeforeUnmount(() => ws && ws.close())
</script>

<style scoped>
.h5-root {
  height: 100%;
  display: flex;
  flex-direction: column;
  background: linear-gradient(160deg, #eef2ff, #f7f5ff 40%, #eaf6ff);
}
.login-wrap {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 20px;
}
.login-card {
  width: 100%;
  max-width: 360px;
  padding: 30px 26px;
  display: flex;
  flex-direction: column;
  gap: 14px;
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
  font-size: 13px;
  color: var(--ink-soft);
}
.login-card input {
  padding: 12px 14px;
  border: 1px solid var(--line);
  border-radius: 10px;
  font-size: 14px;
  outline: none;
}
.login-card input:focus {
  border-color: var(--brand);
}
.err {
  color: var(--danger);
  font-size: 13px;
}
.chat-wrap {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-height: 0;
}
.chat-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 16px;
  background: var(--glass);
  backdrop-filter: blur(12px);
  border-bottom: 1px solid var(--line);
}
.who {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 600;
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
.tips {
  text-align: center;
  font-size: 12px;
  color: var(--ink-soft);
  padding: 6px;
  background: rgba(91, 140, 255, 0.08);
}
.tips.warn {
  color: #a86b00;
  background: rgba(245, 166, 35, 0.14);
}
.tips.err-line {
  color: var(--danger);
  background: rgba(242, 91, 91, 0.1);
}
.msg-list {
  flex: 1;
  overflow: auto;
  padding: 12px 14px;
}
.composer {
  position: relative;
  border-top: 1px solid var(--line);
  background: var(--glass);
  backdrop-filter: blur(12px);
  padding: 10px 12px;
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
.send {
  padding: 10px 16px;
}
.up-status {
  font-size: 12px;
  color: var(--brand-deep);
  margin-top: 6px;
}
</style>
