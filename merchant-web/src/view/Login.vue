<template>
  <div class="login-wrap">
    <el-card class="login-card">
      <div class="brand">
        <el-icon :size="28"><ChatDotRound /></el-icon>
        <div>
          <h3>商户租户后台</h3>
          <p>在线客服系统 · 企业管理端</p>
        </div>
      </div>
      <el-form :model="form" label-width="0" size="large">
        <el-form-item>
          <el-input v-model="form.username" placeholder="管理员账号" :prefix-icon="User" />
        </el-form-item>
        <el-form-item>
          <el-input v-model="form.password" type="password" placeholder="密码" show-password :prefix-icon="Lock" />
        </el-form-item>
        <el-form-item>
          <div class="captcha-row">
            <el-input v-model="form.captcha" placeholder="图形验证码" :prefix-icon="Picture" />
            <img v-if="pic.b64" :src="pic.b64" class="captcha-img" title="点击刷新" @click="loadCaptcha" />
          </div>
        </el-form-item>
        <el-form-item>
          <div class="sms-row">
            <el-input v-model="form.smsCode" placeholder="短信验证码" :prefix-icon="Message" />
            <el-button :disabled="!form.username || counting > 0" @click="sendSms">
              {{ counting > 0 ? `${counting}s` : '发送短信' }}
            </el-button>
          </div>
        </el-form-item>
        <el-button type="primary" class="submit" :loading="loading" @click="doLogin">登 录</el-button>
        <el-alert v-if="devHint" :title="devHint" type="info" :closable="false" class="dev-hint" />
      </el-form>
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { User, Lock, Picture, Message } from '@element-plus/icons-vue'
import { captcha as apiCaptcha, sendSmsCode, login, TOKEN_KEY } from '@/api'

const router = useRouter()
const form = reactive({ username: 'merchantadmin', password: '', captcha: '', smsCode: '' })
const pic = ref({ b64: '', id: '', dev: '' })
const loading = ref(false)
const counting = ref(0)
const devHint = ref('')

const applyCaptcha = (data) => {
  pic.value = { b64: 'data:image/png;base64,' + (data.picPath || ''), id: data.captchaId, dev: data.devCaptcha || '' }
  if (data.devCaptcha) form.captcha = data.devCaptcha // 开发环境自动回填
}
const loadCaptcha = async () => applyCaptcha(await apiCaptcha())

const sendSms = async () => {
  if (!form.captcha) {
    ElMessage.warning('请先填写图形验证码')
    return
  }
  const data = await sendSmsCode({ username: form.username, captcha: form.captcha, captchaId: pic.value.id })
  if (data.devCode) {
    form.smsCode = data.devCode
    devHint.value = `开发环境短信验证码: ${data.devCode}`
  } else {
    devHint.value = '短信已发送，请查收'
  }
  counting.value = 60
  const t = setInterval(() => {
    counting.value -= 1
    if (counting.value <= 0) clearInterval(t)
  }, 1000)
  await loadCaptcha() // 图形验证码一次性, 发送后刷新供登录使用
}

const doLogin = async () => {
  loading.value = true
  try {
    // 图形码已被消费时自动换一张再登录(仅开发环境可无感补填)
    const data = await login({ ...form, captchaId: pic.value.id })
    localStorage.setItem(TOKEN_KEY, data.token)
    ElMessage.success('登录成功')
    router.push('/agents')
  } catch (e) {
    await loadCaptcha()
  } finally {
    loading.value = false
  }
}

onMounted(loadCaptcha)
</script>

<style scoped>
.login-wrap {
  height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(135deg, #1f2a44 0%, #2f4370 60%, #3d5a99 100%);
}
.login-card {
  width: 400px;
  border-radius: 14px;
}
.brand {
  display: flex;
  gap: 12px;
  align-items: center;
  margin-bottom: 22px;
}
.brand h3 {
  margin: 0;
}
.brand p {
  margin: 2px 0 0;
  font-size: 12px;
  color: #98a3bb;
}
.captcha-row,
.sms-row {
  display: flex;
  gap: 8px;
  width: 100%;
}
.captcha-img {
  height: 40px;
  border-radius: 6px;
  cursor: pointer;
}
.submit {
  width: 100%;
}
.dev-hint {
  margin-top: 12px;
}
</style>
