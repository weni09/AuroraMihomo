import axios from 'axios';
import router from './router';
import { useNotifyStore } from './stores/notify';

const api = axios.create({
  baseURL: '/api/v1',
});

// Request interceptor for API calls
api.interceptors.request.use(
  config => {
    const token = localStorage.getItem('aurora_token');
    if (token) {
      config.headers['Authorization'] = `Bearer ${token}`;
    }
    return config;
  },
  error => {
    return Promise.reject(error);
  }
);

// 从各种后端错误格式中提取可读信息
function extractMessage(error: any): string {
  const data = error.response?.data;
  if (typeof data === 'string' && data.trim()) return data.trim();
  if (data?.message) return String(data.message);
  if (data?.msg) return String(data.msg);
  if (error.code === 'ECONNABORTED') return '请求超时，请稍后重试';
  if (!error.response) return '无法连接服务端，请检查服务是否运行';
  return `请求失败（HTTP ${error.response.status}）`;
}

// Response interceptor for API calls
api.interceptors.response.use(
  (response) => {
    return response;
  },
  async function (error) {
    const status = error.response?.status;
    const isLoginRequest = String(error.config?.url || '').includes('/auth/login');

    if (status === 401 && !isLoginRequest) {
      localStorage.removeItem('aurora_token');
      // 已在登录页时不再跳转，避免 NavigationDuplicated；
      // 带上原路径，登录后可回到用户原本要去的页面
      if (router.currentRoute.value.name !== 'login') {
        router.push({ name: 'login', query: { redirect: router.currentRoute.value.fullPath } });
      }
    } else {
      // 统一上报错误，避免各处 action 未 catch 导致「点了没反应」
      try {
        useNotifyStore().error(extractMessage(error));
      } catch {
        // Pinia 尚未初始化（极早期请求），退化为控制台输出
        console.error(error);
      }
    }
    return Promise.reject(error);
  }
);

export default api;
