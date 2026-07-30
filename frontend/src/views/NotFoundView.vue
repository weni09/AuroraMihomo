<script setup lang="ts">
import { useRouter } from 'vue-router'
import { Button } from '@/components/ui/button'
const router = useRouter()
</script>

<template>
  <!-- min-h-dvh 撑满一屏用于垂直居中；外壳已不再锁死高度，
       这里若不给最小高度，短短几行文字会贴在页面顶部而不是居中。 -->
  <main class="min-h-dvh flex flex-col items-center justify-center gap-4 text-center px-8">
    <div class="text-6xl font-bold text-fg-subtle">404</div>
    <h1 class="text-xl font-bold text-fg">页面不存在</h1>
    <p class="text-sm text-fg-muted">你访问的地址可能已变更或从未存在。</p>
    <div class="flex gap-3 mt-2">
      <Button @click="router.push('/')">返回控制台</Button>
      <Button variant="outline" @click="router.back()">返回上一页</Button>
    </div>
  </main>
</template>
