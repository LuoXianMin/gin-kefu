<template>
  <el-container class="layout">
    <el-aside width="220px" class="aside">
      <div class="logo">
        <el-icon :size="22"><ChatDotRound /></el-icon>
        <span>商户客服后台</span>
      </div>
      <el-menu :default-active="route.path" router background-color="#1f2a44" text-color="#aab6d3" active-text-color="#ffd66b">
        <el-menu-item index="/agents"><el-icon><Service /></el-icon>客服管理</el-menu-item>
        <el-menu-item index="/members"><el-icon><User /></el-icon>访客账号</el-menu-item>
        <el-menu-item index="/channel"><el-icon><Cellphone /></el-icon>渠道配置</el-menu-item>
        <el-menu-item index="/reports"><el-icon><TrendCharts /></el-icon>接待报表</el-menu-item>
        <el-menu-item index="/audits"><el-icon><Document /></el-icon>审计日志</el-menu-item>
      </el-menu>
    </el-aside>
    <el-container>
      <el-header class="header">
        <span class="title">{{ route.meta.title || '商户后台' }}</span>
        <el-button text type="danger" @click="doLogout">退出登录</el-button>
      </el-header>
      <el-main class="main">
        <router-view />
      </el-main>
    </el-container>
  </el-container>
</template>

<script setup>
import { useRoute } from 'vue-router'
import { logout } from '@/api'
const route = useRoute()
const doLogout = () => logout()
</script>

<style scoped>
.layout {
  height: 100%;
}
.aside {
  background: #1f2a44;
}
.logo {
  display: flex;
  align-items: center;
  gap: 8px;
  color: #fff;
  font-size: 16px;
  font-weight: 600;
  padding: 18px 16px;
}
.header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  background: #fff;
  border-bottom: 1px solid #e8ebf2;
}
.title {
  font-size: 15px;
  font-weight: 600;
  color: #333;
}
.main {
  padding: 18px;
  overflow: auto;
}
</style>
