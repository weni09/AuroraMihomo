<script setup lang="ts">
/**
 * 右上角「章鱼猫探头」角标：直角三角半包围 + Octocat，fixed 不占文档流。
 * 路径取自 tholman/github-corners（MIT），颜色跟主题 token，避免写死黑白。
 *
 * 窄屏：下移到移动顶栏之下且 z 低于顶栏，避免挡住主题「跟随系统」；
 * ≥lg：贴回视口右上角（桌面无顶栏冲突）。
 */
withDefaults(
  defineProps<{
    href: string
    /** 桌面边长（px），≥lg 生效；窄屏固定 56 */
    size?: number
  }>(),
  { size: 88 },
)
</script>

<template>
  <a
    :href="href"
    target="_blank"
    rel="noopener noreferrer"
    class="github-corner group fixed right-0 top-16 z-10 block h-14 w-14 leading-none lg:top-0 lg:z-30 lg:h-[var(--gh-corner)] lg:w-[var(--gh-corner)]"
    :style="{ '--gh-corner': `${size}px` }"
    aria-label="在 GitHub 打开本项目源码"
    title="GitHub"
  >
    <!-- viewBox 与经典 github-corners 一致：三角铺满右上，猫从斜边探出 -->
    <svg
      width="100%"
      height="100%"
      viewBox="0 0 250 250"
      class="overflow-visible"
      aria-hidden="true"
    >
      <!-- 直角三角半包围：右上原点 → 斜边 → 右下/顶边闭合 -->
      <path
        d="M0,0 L115,115 L130,115 L142,142 L250,250 L250,0 Z"
        class="fill-fg"
      />
      <!-- 章鱼猫：currentColor 吃 text-surface，在深/浅底上都是反色剪影 -->
      <path
        d="M128.3,109.0 C113.8,99.7 119.0,89.3 119.0,89.3 C122.0,82.7 120.5,78.6 120.5,78.6 C119.2,72.0 123.4,76.3 123.4,76.3 C127.3,80.9 125.5,87.3 125.5,87.3 C122.9,97.6 130.6,101.9 134.4,103.2"
        fill="currentColor"
        class="octo-arm origin-[130px_106px] text-surface"
      />
      <path
        d="M115.0,115.0 C114.9,115.1 118.7,116.5 119.8,115.4 L133.7,101.6 C136.9,99.2 139.9,98.4 142.2,98.6 C133.8,88.0 127.5,74.4 143.8,58.0 C148.5,53.4 154.0,51.2 159.7,51.0 C160.3,49.4 163.2,43.6 171.4,40.1 C171.4,40.1 176.1,42.9 178.8,56.2 C183.1,58.6 188.2,61.8 190.9,65.4 C194.5,69.0 197.7,73.2 200.1,77.6 C213.8,80.2 216.3,84.9 216.3,84.9 C212.7,93.1 206.9,96.0 205.4,96.6 C205.1,102.4 203.0,107.8 198.3,112.5 C181.9,128.9 168.3,122.5 157.7,114.1 C157.9,116.9 156.7,120.9 152.7,124.9 L141.0,136.5 C139.8,137.7 141.6,141.9 141.8,141.8 Z"
        fill="currentColor"
        class="octo-body text-surface"
      />
    </svg>
  </a>
</template>

<style scoped>
/* 悬停时挥手：经典 github-corners 动效，读屏不依赖动画 */
.github-corner:hover .octo-arm,
.github-corner:focus-visible .octo-arm {
  animation: octocat-wave 560ms ease-in-out;
}

@keyframes octocat-wave {
  0%,
  100% {
    transform: rotate(0);
  }
  20%,
  60% {
    transform: rotate(-25deg);
  }
  40%,
  80% {
    transform: rotate(10deg);
  }
}

@media (max-width: 500px) {
  .github-corner:hover .octo-arm {
    animation: none;
  }
  .github-corner .octo-arm {
    animation: octocat-wave 560ms ease-in-out;
  }
}
</style>
