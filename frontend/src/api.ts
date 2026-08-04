import axios from 'axios'
import router from './router'
import { useNotifyStore } from './stores/notify'
import { apiErrorMessageDefault } from './utils/apiError'

declare module 'axios' {
  export interface AxiosRequestConfig {
    /**
     * 为 true 时响应错误拦截器不再全局 toast。
     * 用于：登录表单内联错误、需要领域 fallback 文案的 store（自行 toast 一次）。
     */
    skipErrorToast?: boolean
  }
}

const api = axios.create({
  baseURL: '/api/v1',
})

// Request interceptor for API calls
api.interceptors.request.use(
  config => {
    const token = localStorage.getItem('aurora_token')
    if (token) {
      config.headers['Authorization'] = `Bearer ${token}`
    }
    return config
  },
  error => {
    return Promise.reject(error)
  },
)

// Response interceptor for API calls
api.interceptors.response.use(
  response => {
    return response
  },
  async function (error: unknown) {
    const status = axios.isAxiosError(error) ? error.response?.status : undefined
    const url = axios.isAxiosError(error) ? String(error.config?.url || '') : ''
    const isLoginRequest = url.includes('/auth/login')
    const skipToast =
      axios.isAxiosError(error) && error.config?.skipErrorToast === true

    if (status === 401 && !isLoginRequest) {
      localStorage.removeItem('aurora_token')
      // 已在登录页时不再跳转，避免 NavigationDuplicated；
      // 带上原路径，登录后可回到用户原本要去的页面
      if (router.currentRoute.value.name !== 'login') {
        router.push({
          name: 'login',
          query: { redirect: router.currentRoute.value.fullPath },
        })
      }
    } else if (!skipToast) {
      // 统一上报错误，避免各处 action 未 catch 导致「点了没反应」
      try {
        useNotifyStore().error(apiErrorMessageDefault(error))
      } catch {
        // Pinia 尚未初始化（极早期请求），退化为控制台输出
        console.error(error)
      }
    }
    return Promise.reject(error)
  },
)

export default api
