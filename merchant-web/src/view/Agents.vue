<template>
  <div>
    <div class="toolbar">
      <el-input v-model="query.keyword" placeholder="搜索账号/姓名/工号" clearable class="kw" @keyup.enter="load" />
      <el-select v-model="query.role" placeholder="角色" clearable class="sel">
        <el-option label="客服" value="agent" />
        <el-option label="主管" value="supervisor" />
      </el-select>
      <el-button type="primary" :icon="Search" @click="load">查询</el-button>
      <el-button type="success" :icon="Plus" @click="openCreate">新建客服</el-button>
    </div>

    <el-table :data="list" v-loading="loading" border stripe class="table">
      <el-table-column prop="username" label="登录名" minwidth="110" />
      <el-table-column prop="realName" label="姓名" minwidth="90" />
      <el-table-column prop="employeeNo" label="工号" minwidth="80" />
      <el-table-column prop="phone" label="手机号" minwidth="110" />
      <el-table-column label="角色" minwidth="80">
        <template #default="{ row }">{{ row.role === 'supervisor' ? '主管' : '客服' }}</template>
      </el-table-column>
      <el-table-column label="接待中/上限" minwidth="100">
        <template #default="{ row }">{{ row.currentCount }} / {{ row.maxActive }}</template>
      </el-table-column>
      <el-table-column prop="skillTags" label="技能标签" minwidth="120" show-overflow-tooltip />
      <el-table-column label="状态" minwidth="80">
        <template #default="{ row }">
          <el-tag :type="row.enable === 1 ? 'success' : 'info'">{{ row.enable === 1 ? '正常' : '停用' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="230" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
          <el-button link :type="row.enable === 1 ? 'warning' : 'success'" @click="toggle(row)">
            {{ row.enable === 1 ? '停用' : '启用' }}
          </el-button>
          <el-button link type="danger" @click="resetPwd(row)">重置密码</el-button>
        </template>
      </el-table-column>
    </el-table>
    <el-pagination class="pager" layout="total, prev, pager, next" :total="total" :page-size="query.pageSize" :current-page="query.page" @current-change="(p) => { query.page = p; load() }" />

    <el-dialog v-model="dialog" :title="form.id ? '编辑客服' : '新建客服'" width="460px">
      <el-form :model="form" label-width="90px">
        <el-form-item v-if="!form.id" label="登录名" required>
          <el-input v-model="form.username" maxlength="64" />
        </el-form-item>
        <el-form-item v-if="!form.id" label="初始密码" required>
          <el-input v-model="form.password" type="password" show-password placeholder="6-64 位" />
        </el-form-item>
        <el-form-item v-if="!form.id" label="手机号" required>
          <el-input v-model="form.phone" maxlength="11" />
        </el-form-item>
        <el-form-item label="姓名">
          <el-input v-model="form.realName" />
        </el-form-item>
        <el-form-item label="工号">
          <el-input v-model="form.employeeNo" placeholder="留空自动生成" />
        </el-form-item>
        <el-form-item label="角色">
          <el-radio-group v-model="form.role">
            <el-radio value="agent">客服</el-radio>
            <el-radio value="supervisor">主管</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="接待上限">
          <el-input-number v-model="form.maxActive" :min="1" :max="8" />
          <span class="tip">系统硬上限 8</span>
        </el-form-item>
        <el-form-item label="技能标签">
          <el-input v-model="form.skillTags" placeholder="逗号分隔" />
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
import { agentList, agentCreate, agentUpdate, agentStatus, agentResetPwd } from '@/api'

const query = reactive({ keyword: '', role: '', page: 1, pageSize: 10 })
const list = ref([])
const total = ref(0)
const loading = ref(false)
const dialog = ref(false)
const saving = ref(false)
const emptyForm = { id: 0, username: '', password: '', phone: '', realName: '', employeeNo: '', role: 'agent', maxActive: 8, skillTags: '' }
const form = reactive({ ...emptyForm })

const load = async () => {
  loading.value = true
  try {
    const data = await agentList(query)
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
  Object.assign(form, { ...row, password: '', phone: row.phone })
  dialog.value = true
}

const save = async () => {
  saving.value = true
  try {
    if (form.id) {
      await agentUpdate({ id: form.id, phone: form.phone, realName: form.realName, employeeNo: form.employeeNo, role: form.role, maxActive: form.maxActive, skillTags: form.skillTags })
    } else {
      await agentCreate({ ...form })
    }
    ElMessage.success('保存成功')
    dialog.value = false
    load()
  } finally {
    saving.value = false
  }
}

const toggle = async (row) => {
  await ElMessageBox.confirm(`确认${row.enable === 1 ? '停用' : '启用'} ${row.username} ?`, '提示', { type: 'warning' })
  await agentStatus({ id: row.id, enable: row.enable === 1 ? 2 : 1 })
  ElMessage.success('操作成功')
  load()
}

const resetPwd = async (row) => {
  const { value } = await ElMessageBox.prompt(`为 ${row.username} 设置新密码`, '重置密码', { inputType: 'password', inputPattern: /^.{6,64}$/, inputErrorMessage: '密码 6-64 位' })
  await agentResetPwd({ id: row.id, password: value })
  ElMessage.success('密码已重置')
}

onMounted(load)
</script>

<style scoped>
.toolbar {
  display: flex;
  gap: 10px;
  margin-bottom: 14px;
}
.kw {
  width: 220px;
}
.sel {
  width: 120px;
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
  margin-left: 10px;
  font-size: 12px;
  color: #98a3bb;
}
</style>
