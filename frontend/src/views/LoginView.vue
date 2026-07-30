<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import api from '../api'
import AppLogo from '../components/AppLogo.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

const router = useRouter()
const route = useRoute()
const password = ref('')
const loading = ref(false)
const error = ref('')

const handleLogin = async () => {
  if (!password.value) return
  loading.value = true
  error.value = ''
  try {
    const res = await api.post('/auth/login', { password: password.value })
    localStorage.setItem('aurora_token', res.data.token)
    // 回到被拦截前的目标页面
    const redirect = route.query.redirect
    router.push(typeof redirect === 'string' && redirect ? redirect : '/')
  } catch (err: any) {
    // 后端登录接口出错时返回纯文本 body（如「密码错误」「尝试次数过多，请在 X 后重试」），
    // 不是 JSON，因此不能读 .message；否则真实原因会被吞掉，只剩一句无信息量的「登录失败」
    const data = err.response?.data
    const text = typeof data === 'string' ? data.trim() : ''
    error.value = text || data?.message || '登录失败'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <!-- min-h-dvh 而非 h-full：外壳已不再锁死一屏高度，登录页没有侧边栏，
       需要自己撑满视口才能让下面的卡片垂直居中。 -->
  <div class="min-h-dvh bg-elevated flex items-center justify-center p-4">
    <div class="max-w-md w-full bg-surface rounded-lg shadow-xl p-8 border border-line">
      <div class="text-center mb-8">
        <div class="flex justify-center mb-4">
          <AppLogo :size="64" />
        </div>
        <h1 class="text-2xl font-bold text-fg">AuroraMihomo</h1>
        <p class="text-fg-muted mt-2">请输入管理员密码</p>
      </div>
      
      <form @submit.prevent="handleLogin" class="space-y-6">
        <div>
          <Label for="login-password" class="sr-only">管理员密码</Label>
          <Input
            id="login-password"
            v-model="password"
            type="password"
            class="w-full px-4 py-3 h-auto"
            placeholder="管理员密码"
            autocomplete="current-password"
            required
          />
        </div>
        
        <div v-if="error" class="text-rose-600 dark:text-rose-400 text-sm text-center">
          {{ error }}
        </div>

        <Button type="submit" :disabled="loading" class="w-full py-3 h-auto">
          {{ loading ? '登录中...' : '登录' }}
        </Button>
      </form>
    </div>
  </div>
</template>
