<script setup lang="ts">
import { RouterView, RouterLink } from 'vue-router'
import { Combine, FileText, Rss, Share2 } from 'lucide-vue-next'

const tabs = [
  { to: '/substore/subscriptions', label: '单个订阅', icon: Rss },
  { to: '/substore/collections', label: '组合订阅', icon: Combine },
  { to: '/substore/files', label: '模板文件', icon: FileText },
  { to: '/substore/shares', label: '分享管理', icon: Share2 },
]
</script>

<template>
  <div class="bg-canvas">
    <!-- Sub-Store Header & Tabs。sticky 而非固定一屏高的 flex 分栏：
         整页现在用浏览器原生滚动条滚动，标签栏跟随滚动贴顶即可，
         不必再把内容区关进一个自造的滚动容器里。 -->
    <div class="sticky top-0 z-10 bg-surface border-b border-line px-4 sm:px-6 lg:px-8 pt-4 sm:pt-6 shadow-sm">
      <!-- 移动端顶栏已经显示了「Sub-Store 管理」，此处标题在窄屏隐藏，
           省下的高度留给内容 -->
      <h1 class="hidden lg:block text-2xl font-bold text-fg mb-6">Sub-Store 管理</h1>
      <!-- 四个标签在窄屏可横向滑动，不换行也不压缩字号 -->
      <nav class="flex gap-5 sm:gap-6 -mb-px overflow-x-auto no-scrollbar">
        <RouterLink
          v-for="tab in tabs"
          :key="tab.to"
          :to="tab.to"
          class="flex shrink-0 items-center gap-1.5 pb-3 px-1 text-sm font-medium border-b-2 border-transparent text-fg-muted hover:text-fg hover:border-line-strong transition-colors"
          active-class="!text-primary !border-primary"
        >
          <component :is="tab.icon" class="h-4 w-4" aria-hidden="true" />
          {{ tab.label }}
        </RouterLink>
      </nav>
    </div>

    <!-- Render the active child view -->
    <div class="p-4 sm:p-6 lg:p-8">
      <RouterView />
    </div>
  </div>
</template>
