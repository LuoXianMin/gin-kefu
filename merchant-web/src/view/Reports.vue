<template>
  <div v-loading="loading">
    <el-row :gutter="14">
      <el-col v-for="c in cards" :key="c.label" :span="6">
        <el-card class="stat">
          <div class="num" :style="{ color: c.color }">{{ c.value }}</div>
          <div class="label">{{ c.label }}</div>
        </el-card>
      </el-col>
    </el-row>

    <el-card class="perf">
      <template #header>客服接待绩效</template>
      <el-table :data="perf" border stripe>
        <el-table-column prop="employeeNo" label="工号" width="100" />
        <el-table-column prop="realName" label="姓名" width="120" />
        <el-table-column prop="served" label="累计接待" width="110" sortable />
        <el-table-column prop="active" label="进行中" width="90" />
        <el-table-column prop="closed" label="已结束" width="90" />
        <el-table-column label="接待量对比">
          <template #default="{ row }">
            <div class="bar-wrap">
              <div class="bar" :style="{ width: barWidth(row.served) }" />
            </div>
          </template>
        </el-table-column>
      </el-table>
    </el-card>
    <el-button :icon="Refresh" circle class="refresh" @click="load" />
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { reportOverview, reportAgents } from '@/api'

const loading = ref(false)
const ov = ref({})
const perf = ref([])

const cards = computed(() => [
  { label: '客服总数', value: ov.value.agentTotal ?? '-', color: '#409eff' },
  { label: '在线接待中', value: ov.value.agentOnline ?? '-', color: '#67c23a' },
  { label: '排队会话', value: ov.value.convPending ?? '-', color: '#e6a23c' },
  { label: '进行中会话', value: ov.value.convActive ?? '-', color: '#409eff' },
  { label: '今日结束', value: ov.value.convClosedToday ?? '-', color: '#67c23a' },
  { label: '累计会话', value: ov.value.convTotal ?? '-', color: '#909399' }
])

const maxServed = computed(() => Math.max(1, ...perf.value.map((p) => p.served || 0)))
const barWidth = (n) => `${Math.round(((n || 0) / maxServed.value) * 100)}%`

const load = async () => {
  loading.value = true
  try {
    ov.value = await reportOverview()
    perf.value = (await reportAgents()) || []
  } finally {
    loading.value = false
  }
}
onMounted(load)
</script>

<style scoped>
.stat {
  border-radius: 10px;
  text-align: center;
  margin-bottom: 14px;
}
.num {
  font-size: 26px;
  font-weight: 700;
}
.label {
  font-size: 13px;
  color: #7a869c;
  margin-top: 4px;
}
.perf {
  border-radius: 10px;
}
.bar-wrap {
  background: #eef1f7;
  border-radius: 6px;
  height: 12px;
  overflow: hidden;
}
.bar {
  background: linear-gradient(90deg, #6ac1ff, #409eff);
  height: 100%;
  border-radius: 6px;
  transition: width 0.4s;
}
.refresh {
  margin-top: 12px;
}
</style>
