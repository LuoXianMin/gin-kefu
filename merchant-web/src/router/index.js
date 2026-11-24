import { createRouter, createWebHashHistory } from 'vue-router'
import { TOKEN_KEY } from '@/api'

const routes = [
  { path: '/login', name: 'Login', component: () => import('@/view/Login.vue') },
  {
    path: '/',
    component: () => import('@/view/Layout.vue'),
    children: [
      { path: '', redirect: '/agents' },
      { path: 'agents', name: 'Agents', component: () => import('@/view/Agents.vue'), meta: { title: '客服管理' } },
      { path: 'members', name: 'Members', component: () => import('@/view/Members.vue'), meta: { title: '访客账号' } },
      { path: 'channel', name: 'Channel', component: () => import('@/view/Channel.vue'), meta: { title: '渠道配置' } },
      { path: 'reports', name: 'Reports', component: () => import('@/view/Reports.vue'), meta: { title: '接待报表' } },
      { path: 'audits', name: 'Audits', component: () => import('@/view/Audits.vue'), meta: { title: '审计日志' } }
    ]
  }
]

const router = createRouter({ history: createWebHashHistory(), routes })

router.beforeEach((to, from, next) => {
  if (to.path !== '/login' && !localStorage.getItem(TOKEN_KEY)) {
    next('/login')
    return
  }
  next()
})

export default router
