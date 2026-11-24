<template>
  <div>
    <div class="toolbar">
      <el-input v-model="query.keyword" placeholder="搜索账号/昵称/手机号" clearable class="kw" @keyup.enter="load" />
      <el-button type="primary" :icon="Search" @click="load">查询</el-button>
      <el-button type="success" :icon="Plus" @click="openCreate">新建访客</el-button>
      <span class="tip">访客凭账号密码从商户接入链接(h5.html?t=商户编码)登录咨询</span>
    </div>

    <el-table :data="list" v-loading="loading" border stripe class="table">
      <el-table-column prop="username" label="登录名" minwidth="110" />
      <el-table-column prop="nickName" label="昵称" minwidth="90" />
      <el-table-column prop="phone" label="手机号" minwidth="110" />
      <el-table-column prop="source" label="来源渠道" minwidth="90" />
      <el-table-column label="状态" minwidth="80">
        <template #default="{ row }">
          <el-tag :type="row.status === 1 ? 'success' : 'info'">{{ row.status === 1 ? '正常' : '停用' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="创建时间" minwidth="150">
        <template #default="{ row }">{{ fmt(row.createdAt) }}</template>
      </el-table-column>
      <el-table-column label="操作" width="220" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
          <el-button link :type="row.status === 1 ? 'warning' : 'success'" @click="toggle(row)">
            {{ row.status === 1 ? '停用' : '启用' }}
          </el-button>
          <el-button link type="danger" @click="resetPwd(row)">重置密码</el-button>
        </template>
      </el-table-column>
    </el-table>
    <el-pagination
      class="pager"
      layout="total, prev, pager, next"
      :total="total"
      :page-size="query.pageSize"
      :current-page="query.page"
      @current-change="(p) => { query.page = p; load() }"
    />

    <el-dialog v-model="dialog" :title="form.id ? '编辑访客' : '新建访客'" width="440px">
      <el-form :model="form" label-width="90px">
        <el-form-item v-if="!form.id" label="登录名" required>
          <el-input v-model="form.username" maxlength="64" />
        </el-form-item>
        <el-form-item v-if="!form.id" label="初始密码" required>
          <el-input v-model="form.password" type="password" show-password placeholder="6-64 位" />
        </el-form-item>
        <el-form-item label="昵称">
          <el-input v-model="form.nickName" maxlength="64" />
        </el-form-item>
        <el-form-item label="手机号">
          <el-input v-model="form.phone" maxlength="11" />
        </el-form-item>
        <el-form-item label="来源渠道">
          <el-input v-model="form.source" placeholder="H5/落地页/业务入口" maxlength="64" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialog = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Search, Plus } from '@element-plus/icons-vue'
import { memberList, memberCreate, memberUpdate, memberResetPwd } from '@/api'

const query = reactive({ keyword: '', page: 1, pageSize: 10 })
const list = ref([])
const total = ref(0)
const loading = ref(false)
const dialog = ref(false)
const saving = ref(false)
const emptyForm = { id: 0, username: '', password: '', nickName: '', phone: '', source: 'H5' }
const form = reactive({ ...emptyForm })

const fmt = (t) => (t ? new Date(t).toLocaleString('zh-CN', { hour12: false }) : '')

const load = async () => {
  loading.value = true
  try {
    const data = await memberList(query)
    list.value = data.list || []
    total.value = data.total || 0
  } finally {
    loading.value = false
  }
}

const openCreate = () => {
  Object.assign(form, emptyForm, { id: 0 })
  dialog.value = true
}
const openEdit = (row) => {
  Object.assign(form, { id: row.id, username: row.username, password: '', nickName: row.nickName, phone: row.phone, source: row.source })
  dialog.value = true
}

const save = async () => {
  saving.value = true
  try {
    if (form.id) {
      await memberUpdate({ id: form.id, nickName: form.nickName, phone: form.phone, source: form.source })
    } else {
      await memberCreate({ username: form.username, password: form.password, nickName: form.nickName, phone: form.phone, source: form.source })
    }
    ElMessage.success('保存成功')
    dialog.value = false
    load()
  } finally {
    saving.value = false
  }
}

const toggle = async (row) => {
  await ElMessageBox.confirm(`确认${row.status === 1 ? '停用' : '启用'}访客 ${row.username} ?`, '提示', { type: 'warning' })
  await memberUpdate({ id: row.id, nickName: row.nickName, phone: row.phone, source: row.source, status: row.status === 1 ? 2 : 1 })
  ElMessage.success('操作成功')
  load()
}

const resetPwd = async (row) => {
  const { value } = await ElMessageBox.prompt(`为 ${row.username} 设置新密码`, '重置密码', { inputType: 'password', inputPattern: /^.{6,64}$/, inputErrorMessage: '密码 6-64 位' })
  await memberResetPwd({ id: row.id, password: value })
  ElMessage.success('密码已重置')
}

onMounted(load)
</script>

<style scoped>
.toolbar {
  display: flex;
  gap: 10px;
  margin-bottom: 14px;
  align-items: center;
}
.kw {
  width: 220px;
}
.table {
  background: #fff;
  border-radius: 8px;
}
.pager {
  margin-top: 12px;
  justify-content: flex-end;
}
.tip {
  font-size: 12px;
  color: #8a94ad;
}
</style>
