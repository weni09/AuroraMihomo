import { defineStore } from 'pinia'
import api from '../api'

export interface TaskItem {
  id: number
  name: string
  cron: string
  enabled: boolean
  lastRun: string
  nextRun: string
  status: string
  message: string
}

const TASK_LABELS: Record<string, string> = {
  // subscription_update / version_check 已下线（启动清理 + 列表过滤）；
  // 标签保留仅为兼容尚未升级后端的瞬时响应
  subscription_update: '订阅自动更新（已下线）',
  version_check: '版本检查（已下线）',
  config_merge: '配置合并',
  mihomo_reload: '内核重载',
  // 以下来自系统设置驱动的 Scheduler；GET /tasks 合并返回（见 listTasksLogic）。
  // 组件自动更新的开关在「系统设置 → 自动更新」，不再有并列的「版本检查」行。
  applog_cleanup: '日志归档清理',
  auto_update: '组件自动更新',
  remote_config_pull: '远程配置拉取',
  adguard_auto_update: 'AdGuard 自动更新',
}

export const useTaskStore = defineStore('task', {
  state: () => ({
    tasks: [] as TaskItem[],
    loading: false,
    message: '',
  }),
  getters: {
    labeled: (s) =>
      s.tasks.map((t) => ({ ...t, label: TASK_LABELS[t.name] || t.name })),
  },
  actions: {
    async fetch() {
      this.loading = true
      try {
        const res = await api.get<TaskItem[]>('/tasks')
        this.tasks = res.data || []
      } catch (e) {
        console.error(e)
        this.message = '加载任务列表失败'
      } finally {
        this.loading = false
      }
    },
  },
})
