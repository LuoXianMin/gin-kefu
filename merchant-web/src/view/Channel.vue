<template>
  <div v-loading="loading">
    <el-card class="card">
      <template #header>H5 接待渠道配置</template>
      <el-form :model="form" label-width="120px">
        <el-form-item label="欢迎语模板">
          <el-input v-model="form.welcomeTemplate" type="textarea" :rows="3" maxlength="512" show-word-limit placeholder="用户进入会话时的开场白" />
        </el-form-item>
        <el-form-item label="域名白名单">
          <el-input v-model="form.domainWhitelist" placeholder="允许嵌入 H5 的域名, 逗号分隔, 如 h5.example.com,shop.example.com" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :loading="saving" @click="save">保存配置</el-button>
        </el-form-item>
      </el-form>
    </el-card>
    <el-card class="card">
      <template #header>接入信息</template>
      <el-descriptions :column="1" border>
        <el-descriptions-item label="访客端 H5">http://127.0.0.1:8090/h5.html（chat-web 工程）</el-descriptions-item>
        <el-descriptions-item label="客服工作台">http://127.0.0.1:8090/workbench.html</el-descriptions-item>
        <el-descriptions-item label="WebSocket 网关">ws://127.0.0.1:8889/ws</el-descriptions-item>
      </el-descriptions>
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { channelGet, channelUpdate } from '@/api'

const loading = ref(false)
const saving = ref(false)
const form = reactive({ welcomeTemplate: '', domainWhitelist: '' })

const load = async () => {
  loading.value = true
  try {
    const data = await channelGet()
    Object.assign(form, { welcomeTemplate: data.welcomeTemplate || '', domainWhitelist: data.domainWhitelist || '' })
  } finally {
    loading.value = false
  }
}

const save = async () => {
  saving.value = true
  try {
    await channelUpdate({ ...form })
    ElMessage.success('配置已保存')
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.card {
  border-radius: 10px;
  margin-bottom: 16px;
  max-width: 760px;
}
</style>
