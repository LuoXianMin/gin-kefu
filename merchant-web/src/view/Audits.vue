<template>
  <div>
    <div class="toolbar">
      <el-input v-model="query.action" placeholder="动作码筛选(如 tenant / file / audit)" clearable class="kw" @keyup.enter="load" />
      <el-button type="primary" :icon="Search" @click="load">查询</el-button>
    </div>
    <el-table :data="list" v-loading="loading" border stripe>
      <el-table-column prop="eventAt" label="时间(UTC)" width="180">
        <template #default="{ row }">{{ fmt(row.eventAt) }}</template>
      </el-table-column>
      <el-table-column prop="actorType" label="操作者类型" width="110" />
      <el-table-column prop="actorId" label="操作者ID" width="90" />
      <el-table-column prop="action" label="动作" width="150" />
      <el-table-column prop="target" label="目标" minwidth="140" show-overflow-tooltip />
      <el-table-column prop="detail" label="详情" minwidth="200" show-overflow-tooltip />
      <el-table-column prop="ip" label="IP" width="120" />
    </el-table>
    <el-pagination class="pager" layout="total, prev, pager, next" :total="total" :page-size="query.pageSize" :current-page="query.page" @current-change="(p) => { query.page = p; load() }" />
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { Search } from '@element-plus/icons-vue'
import { auditList } from '@/api'

const query = reactive({ action: '', page: 1, pageSize: 20 })
const list = ref([])
const total = ref(0)
const loading = ref(false)

const fmt = (t) => (t ? t.replace('T', ' ').replace(/\.\d+Z?$/, '') : '-')

const load = async () => {
  loading.value = true
  try {
    const data = await auditList(query)
    list.value = data.list || []
    total.value = data.total || 0
  } finally {
    loading.value = false
  }
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
  width: 280px;
}
.pager {
  margin-top: 12px;
  justify-content: flex-end;
}
</style>
