import { createRouter, createWebHashHistory } from 'vue-router'

const routes = [
  { path: '/', redirect: '/home' },
  // 路由页统一动态导入，让 Vite 以页面为边界拆包，避免首屏一次加载全部业务模块。
  { path: '/home', component: () => import('../components/Home.vue'), name: 'home' },
  { path: '/stock', component: () => import('../components/stock.vue'), name: 'stock' },
  { path: '/fund', component: () => import('../components/fund.vue'), name: 'fund' },
  { path: '/settings', component: () => import('../components/settings.vue'), name: 'settings' },
  { path: '/about', component: () => import('../components/about.vue'), name: 'about' },
  { path: '/market', component: () => import('../components/market.vue'), name: 'market' },
  { path: '/agent', component: () => import('../components/agent-chat.vue'), name: 'agent' },
  { path: '/research', component: () => import('../components/researchIndex.vue'), name: 'research' },
  { path: '/cron-tasks', component: () => import('../components/cron-task-manager.vue'), name: 'cronTasks' },
  { path: '/mcp-servers', component: () => import('../components/mcp-server-manager.vue'), name: 'mcpServers' },
  { path: '/kline-analysis', component: () => import('../components/kline-analysis.vue'), name: 'klineAnalysis' },
  { path: '/ai-configs', component: () => import('../components/ai-config-manager.vue'), name: 'aiConfigs' }
]

const router = createRouter({
  history: createWebHashHistory(),
  routes
})

export default router
