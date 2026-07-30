<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(defineProps<{ size?: number | string }>(), { size: 32 })

// 内联 SVG 的渐变 id 在整个文档里是全局的，同页多个实例会互相覆盖，
// 因此每个实例生成独立后缀。
const uid = Math.random().toString(36).slice(2, 8)
const bandA = computed(() => `auroraA-${uid}`)
const bandB = computed(() => `auroraB-${uid}`)
const bandC = computed(() => `auroraC-${uid}`)

// 32px 以下时三道细光带会糊成一团，简化为两道并加粗
const compact = computed(() => Number(props.size) > 0 && Number(props.size) <= 24)
</script>

<template>
  <svg
    :width="size"
    :height="size"
    viewBox="0 0 64 64"
    xmlns="http://www.w3.org/2000/svg"
    role="img"
    aria-label="AuroraMihomo"
  >
    <defs>
      <linearGradient :id="bandA" x1="32" y1="4" x2="32" y2="50" gradientUnits="userSpaceOnUse">
        <stop offset="0" stop-color="#c084fc" />
        <stop offset="0.55" stop-color="#22d3ee" />
        <stop offset="1" stop-color="#34d399" />
      </linearGradient>
      <linearGradient :id="bandB" x1="32" y1="4" x2="32" y2="50" gradientUnits="userSpaceOnUse">
        <stop offset="0" stop-color="#a78bfa" />
        <stop offset="1" stop-color="#38bdf8" />
      </linearGradient>
      <linearGradient :id="bandC" x1="32" y1="6" x2="32" y2="48" gradientUnits="userSpaceOnUse">
        <stop offset="0" stop-color="#818cf8" />
        <stop offset="1" stop-color="#22d3ee" />
      </linearGradient>
    </defs>

    <!-- 小尺寸：两道加粗光带，保证 16px 下仍可辨识 -->
    <template v-if="compact">
      <path
        d="M23 46C18 33 24 21 32 12"
        fill="none"
        :stroke="`url(#${bandB})`"
        stroke-width="9"
        stroke-linecap="round"
      />
      <path
        d="M40 45C36 33 41 23 48 16"
        fill="none"
        :stroke="`url(#${bandA})`"
        stroke-width="8"
        stroke-linecap="round"
      />
      <circle cx="30" cy="53" r="4.5" fill="#22d3ee" />
    </template>

    <!-- 常规尺寸：三道极光帷幕，层次更丰富 -->
    <template v-else>
      <path
        d="M13 46C8 31 15 17 25 6"
        fill="none"
        :stroke="`url(#${bandC})`"
        stroke-width="7"
        stroke-linecap="round"
        opacity="0.5"
      />
      <path
        d="M29 47C23 31 31 16 41 4"
        fill="none"
        :stroke="`url(#${bandB})`"
        stroke-width="8"
        stroke-linecap="round"
        opacity="0.75"
      />
      <path
        d="M45 45C40 30 47 17 56 8"
        fill="none"
        :stroke="`url(#${bandA})`"
        stroke-width="7"
        stroke-linecap="round"
      />
      <circle cx="32" cy="56" r="5" fill="#22d3ee" />
    </template>
  </svg>
</template>
