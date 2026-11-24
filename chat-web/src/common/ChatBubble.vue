<template>
  <!-- 系统事件: 居中细线 -->
  <div v-if="kind === 'sys'" class="sys-line">{{ sysText }}</div>

  <!-- 消息气泡 -->
  <div v-else class="msg-row" :class="isMe ? 'me' : 'other'">
    <div v-if="!isMe" class="avatar" :class="senderCls">{{ avatarText }}</div>
    <div class="bubble">
      <template v-if="msg.type === 'image'">
        <img v-if="src" :src="src" class="chat-img" alt="图片" @click="zoom = true" />
        <div v-else class="img-ph">{{ srcErr || '图片加载中…' }}</div>
        <div v-if="imageName" style="font-size: 11px; opacity: 0.75; margin-top: 2px">🖼 {{ imageName }}</div>
      </template>
      <template v-else>{{ msg.body }}</template>
      <div class="meta">{{ metaText }}</div>
    </div>
    <div v-if="isMe" class="avatar" :class="senderCls">{{ avatarText }}</div>
  </div>

  <!-- 大图预览 -->
  <div v-if="zoom" class="img-viewer" @click="zoom = false">
    <img v-if="src" :src="src" alt="预览" />
  </div>
</template>

<script setup>
import { computed, ref, watch, onMounted } from 'vue'
import { parseImageBody, fileViewUrl } from '../common/api'

const props = defineProps({
  msg: { type: Object, required: true },
  // sys 事件消息: {action, fromEmpNo, toEmpNo, reason, eventTime}
  sysEvent: { type: Object, default: null },
  // viewerScope: 当前查看者身份 member/agent, 决定 me 判定与签名链接口域
  viewerScope: { type: String, required: true }
})

const kind = computed(() => (props.sysEvent ? 'sys' : props.msg.type === 'image' ? 'image' : 'text'))
const isBot = computed(() => props.msg.senderType === 'bot')
const isMe = computed(() => !isBot.value && props.msg.senderType === props.viewerScope)
// 机器人(欢迎语等)与客服同侧展示, 头像文案区分角色
const senderCls = computed(() => (props.msg.senderType === 'agent' || isBot.value ? 'agent' : 'me'))
const avatarText = computed(() => (isBot.value ? '机器人' : props.msg.senderType === 'agent' ? '客服' : '我'))

const fmtTime = (t) => {
  if (!t) return ''
  const d = new Date(t)
  const today = new Date()
  const sameDay = d.toDateString() === today.toDateString()
  return sameDay ? d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' }) : d.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}

const sysText = computed(() => {
  const e = props.sysEvent
  if (!e) return ''
  if (e.action === 'transfer') return `会话由 ${e.fromEmpNo || '客服'} 转接给 ${e.toEmpNo || '客服'}${e.reason ? '(' + e.reason + ')' : ''}`
  if (e.action === 'assign') return `客服 ${e.toEmpNo || ''} 接入会话`
  if (e.action === 'close') return '会话已结束'
  return ''
})

const metaText = computed(() => {
  const parts = []
  if (props.msg.senderType === 'agent' && props.msg.senderEmployeeNo) parts.push(props.msg.senderEmployeeNo)
  parts.push(fmtTime(props.msg.createdAt))
  return parts.join(' · ')
})

// 图片消息: 解析 fileId 后换取签名 URL
const src = ref('')
const srcErr = ref('')
const zoom = ref(false)
const imageName = ref('')
const imgInfo = computed(() => (props.msg.type === 'image' ? parseImageBody(props.msg.body) : null))

const loadSrc = async () => {
  const info = imgInfo.value
  if (!info) {
    srcErr.value = '图片消息格式异常'
    return
  }
  imageName.value = info.fileName || ''
  srcErr.value = ''
  try {
    src.value = await fileViewUrl(props.viewerScope, info.fileId)
  } catch (e) {
    srcErr.value = '无法加载图片'
  }
}
watch(imgInfo, loadSrc, { immediate: true })
onMounted(() => kind.value === 'image' && !src.value && loadSrc())
</script>
