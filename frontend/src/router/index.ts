import { createRouter, createWebHistory } from 'vue-router'
import DashboardView from '../views/DashboardView.vue'
import MihomoView from '../views/MihomoView.vue'
import FilesView from '../views/FilesView.vue'
import SharesView from '../views/SharesView.vue'
import LoginView from '../views/LoginView.vue'
import SubStoreLayout from '../views/SubStoreLayout.vue'
import SubscriptionsView from '../views/SubscriptionsView.vue'
import SettingsView from '../views/SettingsView.vue'
import DiffView from '../views/DiffView.vue'
import CollectionsView from '../views/CollectionsView.vue'
import LogsView from '../views/LogsView.vue'
import ConfigView from '../views/ConfigView.vue'
import ZashboardView from '../views/ZashboardView.vue'
import AdGuardView from '../views/AdGuardView.vue'
import NotFoundView from '../views/NotFoundView.vue'
import { isTokenExpired } from '../utils/token'

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    {
      path: '/substore',
      component: SubStoreLayout,
      // 子路由各自的标题由 SubStoreLayout 内的标签栏体现，
      // 移动端顶栏统一显示父级标题
      meta: { title: 'SubStore功能' },
      children: [
        { path: '', redirect: '/substore/subscriptions' },
        { path: 'subscriptions', name: 'subscriptions', component: SubscriptionsView },
        { path: 'collections', name: 'collections', component: CollectionsView },
        { path: 'files', name: 'files', component: FilesView },
        { path: 'shares', name: 'shares', component: SharesView },
        // 「全局规则与模板」已取消：全局改写规则移除（改由各订阅/组合的
        // 处理管道承担），模板转换并入模板文件。旧书签重定向到分享管理。
        { path: 'rules', redirect: '/substore/shares' },
      ]
    },
    { path: '/login', name: 'login', component: LoginView, meta: { public: true, title: '登录' } },
    { path: '/', name: 'dashboard', component: DashboardView, meta: { title: '控制台' } },
    { path: '/mihomo', name: 'mihomo', component: MihomoView, meta: { title: '内核管理' } },
    { path: '/config', name: 'config', component: ConfigView, meta: { title: '配置中心' } },
    { path: '/diff', name: 'diff', component: DiffView, meta: { title: '配置差异' } },
    { path: '/logs', name: 'logs', component: LogsView, meta: { title: '运行日志' } },
    { path: '/settings', name: 'settings', component: SettingsView, meta: { title: '系统设置' } },
    // 内嵌 zashboard（后端把面板静态资源挂在同源的 /ui/）
    { path: '/zashboard', name: 'zashboard', component: ZashboardView, meta: { title: 'Zashboard' } },
    // 内嵌 AdGuard Home（SPA /adguard；官方 UI 反代 /adguard-ui/）
    { path: '/adguard', name: 'adguard', component: AdGuardView, meta: { title: 'AdGuard' } },
    // 使用文档随程序内置，离线可用（内容在构建时打进产物）。
    // 本项目其余路由都是静态导入，这里刻意用懒加载：文档页要拉进
    // markdown-it 与全文内容（约 150KB），而它不是常用页面，
    // 静态导入会让每个用户的首屏都为此付费。
    {
      path: '/docs',
      name: 'docs',
      component: () => import('../views/DocsView.vue'),
      meta: { title: '使用文档' },
    },
    // 兜底路由，避免未知路径渲染空白页面
    { path: '/:pathMatch(.*)*', name: 'not-found', component: NotFoundView, meta: { public: true, title: '页面不存在' } },
  ],
})

router.beforeEach((to, _from, next) => {
  const token = localStorage.getItem('aurora_token')
  const isAuthenticated = !!token && !isTokenExpired(token)
  if (!to.meta.public && !isAuthenticated) {
    // 过期 token 必须当场清理：否则登录页成功后旧值被覆盖无碍，
    // 但不清的话用户刷新页面又被当「已登录」放进业务页，接口一片 401。
    if (token) localStorage.removeItem('aurora_token')
    // 记住原目标，登录后回到用户本来要去的页面
    next({ name: 'login', query: { redirect: to.fullPath } })
  } else if (to.name === 'login' && isAuthenticated) {
    next('/')
  } else {
    next()
  }
})

// 标签页标题跟随当前路由。SPA 不换文档，不手动设置的话
// 多标签之间无法区分，浏览器历史记录里也全是同一个标题
router.afterEach((to) => {
  const matched = [...to.matched].reverse().find((r) => r.meta?.title)
  const title = (matched?.meta.title as string) || ''
  document.title = title ? `${title} · AuroraMihomo` : 'AuroraMihomo 管理面板'
})

export default router
