<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import MarkdownIt from 'markdown-it'
import guideSource from '../content/user-guide.md?raw'

/**
 * 内置使用文档。
 *
 * 文档内容在构建时打进产物（`?raw` 导入），因此离线环境同样可用，
 * 且版本与程序本体天然一致——不会出现「面板是新版、文档是旧版」。
 * 内容源是 userdocs/user-guide.md，由 scripts/sync-docs.mjs 同步到
 * src/content/ 下，改文档请改 userdocs/ 里的原件（docs/ 是开发设计
 * 文档，与面向用户的文档分开维护）。
 *
 * 渲染时关闭 html 选项：文档是我们自己维护的可信内容，但保持关闭可以
 * 让「以后有人把外部内容接进来」这件事不会顺带变成一个注入点。
 */
// 给标题加锚点 id，供左侧目录跳转。
// markdown-it 默认不生成 id，自己接一层比引插件轻。
const slugify = (text: string) =>
  text
    .trim()
    .toLowerCase()
    // 保留中文：本文档标题以中文为主，去掉它们就没法生成有意义的锚点
    .replace(/[^\w\u4e00-\u9fa5 -]/g, '')
    .replace(/\s+/g, '-')

interface TocItem {
  level: number
  text: string
  id: string
}

/**
 * 去掉 Markdown 正文里手写的「## 目录」小节。
 *
 * 那份目录是给在仓库/GitHub 上直接读文件的人用的，页面里左侧已有一份
 * 自动生成且能跟随滚动高亮的目录，两份并存只是重复。
 * 在渲染时剥离而不是从源文件删掉，两种阅读场景就都合适。
 */
function stripMarkdownToc(src: string): string {
  const lines = src.split('\n')
  const start = lines.findIndex((l) => /^##\s+目录\s*$/.test(l))
  if (start < 0) return src
  // 一直丢到下一个同级或更高级标题为止
  let end = start + 1
  while (end < lines.length && !/^#{1,2}\s/.test(lines[end]!)) end++
  return [...lines.slice(0, start), ...lines.slice(end)].join('\n')
}

/**
 * 渲染文档并顺带收集目录。
 *
 * 文档源是构建时内联的常量，不会变，所以在模块级渲染一次即可，
 * 不需要 computed —— 那只会让人误以为它依赖响应式状态。
 *
 * 关闭 html 选项：文档是我们自己维护的可信内容，但保持关闭可以让
 * 「以后有人把外部内容接进来」不会顺带变成一个注入点。
 */
function renderGuide(): { html: string; toc: TocItem[] } {
  const items: TocItem[] = []
  const seen = new Map<string, number>()

  const renderer = new MarkdownIt({ html: false, linkify: true, typographer: false })

  renderer.renderer.rules.heading_open = (tokens, idx) => {
    const tag = tokens[idx]!.tag // h1 / h2 / ...
    const level = Number(tag.slice(1))
    const inline = tokens[idx + 1]
    const text = inline?.content ?? ''

    let id = slugify(text)
    // 同名标题会产出重复 id，跳转只会落到第一个；加序号区分
    const n = seen.get(id) ?? 0
    seen.set(id, n + 1)
    if (n > 0) id = `${id}-${n}`

    // 只把 h2/h3 放进目录：h1 是文档标题，h4 以下太细碎
    if (level === 2 || level === 3) {
      items.push({ level, text, id })
    }
    return `<${tag} id="${id}">`
  }

  // 表格加一层包裹以便窄屏横向滚动，不然宽表会把页面撑破
  renderer.renderer.rules.table_open = () => '<div class="doc-table-wrap"><table>'
  renderer.renderer.rules.table_close = () => '</table></div>'

  // 外链新标签打开
  const defaultLinkOpen =
    renderer.renderer.rules.link_open ||
    ((tokens, idx, options, _env, self) => self.renderToken(tokens, idx, options))
  renderer.renderer.rules.link_open = (tokens, idx, options, env, self) => {
    const href = tokens[idx]!.attrGet('href') ?? ''
    if (/^https?:/.test(href)) {
      tokens[idx]!.attrSet('target', '_blank')
      // noopener 防止新页面通过 window.opener 反向操作本页
      tokens[idx]!.attrSet('rel', 'noopener noreferrer')
    }
    return defaultLinkOpen(tokens, idx, options, env, self)
  }

  const out = renderer.render(stripMarkdownToc(guideSource))
  return { html: out, toc: items }
}

const { html, toc } = renderGuide()

const activeId = ref('')
const contentEl = ref<HTMLElement | null>(null)

// 目录高亮跟随滚动。用 IntersectionObserver 而非 scroll 事件：
// 后者在长文档里每帧都要遍历一遍标题算位置。
let observer: IntersectionObserver | null = null

onMounted(() => {
  // computed 求值后 toc 才有内容，等 DOM 渲染完再挂观察器
  requestAnimationFrame(() => {
    const root = contentEl.value
    if (!root) return
    const headings = root.querySelectorAll('h2[id], h3[id]')
    if (headings.length === 0) return

    observer = new IntersectionObserver(
      (entries) => {
        // 取当前可见的最靠上那个标题
        const visible = entries
          .filter((e) => e.isIntersecting)
          .sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top)
        if (visible.length > 0) {
          activeId.value = visible[0]!.target.id
        }
      },
      // 顶部留出 80px：标题刚滑到视口顶端时就该高亮，而不是等它到中间
      { rootMargin: '-80px 0px -70% 0px', threshold: 0 },
    )
    headings.forEach((h) => observer!.observe(h))
  })
})

onUnmounted(() => observer?.disconnect())

const jumpTo = (id: string) => {
  const el = document.getElementById(id)
  if (!el) return
  el.scrollIntoView({ behavior: 'smooth', block: 'start' })
  activeId.value = id
}

// 窄屏目录默认折叠：长文档的目录本身就有两屏高，展开会把正文顶下去
const tocOpen = ref(false)
</script>

<template>
  <main class="p-4 sm:p-6 lg:p-8">
    <div class="mx-auto max-w-7xl">
      <div class="mb-4 flex flex-wrap items-center justify-between gap-2">
        <h1 class="text-2xl font-bold sm:text-3xl">使用文档</h1>
        <p class="text-xs text-fg-subtle">
          随程序内置，离线可用，版本与当前程序一致。
        </p>
      </div>

      <!-- 窄屏目录开关 -->
      <button
        class="btn btn-secondary mb-3 w-full lg:hidden"
        :aria-expanded="tocOpen"
        aria-controls="doc-toc"
        @click="tocOpen = !tocOpen"
      >
        {{ tocOpen ? '收起目录' : '展开目录' }}（{{ toc.length }} 节）
      </button>

      <div class="flex flex-col gap-6 lg:flex-row lg:items-start">
        <!-- 目录 -->
        <nav
          id="doc-toc"
          :class="[
            'thin-scrollbar shrink-0 rounded-lg border border-line bg-surface p-2 lg:sticky lg:top-6 lg:w-64 lg:max-h-[calc(100dvh-6rem)] lg:overflow-y-auto',
            tocOpen ? 'block' : 'hidden lg:block',
          ]"
          aria-label="文档目录"
        >
          <ul class="space-y-0.5 text-sm">
            <li v-for="item in toc" :key="item.id">
              <button
                :class="[
                  // 选中项用左侧色条 + 实色文字标记。
                  // 不能用 text-accent：项目里 --accent 是背景色 token
                  // （等于 --c-elevated），配 bg-elevated 就是同色压同色，
                  // 文字会完全看不见。强调色要用 accent-solid。
                  'relative block w-full rounded-md py-1.5 pr-2 text-left transition-colors',
                  item.level === 3 ? 'pl-6 text-xs' : 'pl-3 font-medium',
                  activeId === item.id
                    ? 'bg-accent-solid/10 font-semibold text-accent-text dark:bg-accent-solid/20'
                    : item.level === 3
                      ? 'text-fg-muted hover:bg-elevated hover:text-fg'
                      : 'text-fg hover:bg-elevated',
                ]"
                :aria-current="activeId === item.id ? 'location' : undefined"
                @click="jumpTo(item.id); tocOpen = false"
              >
                <span
                  v-if="activeId === item.id"
                  class="absolute inset-y-1 left-0 w-0.5 rounded-full bg-accent-text"
                  aria-hidden="true"
                />
                {{ item.text }}
              </button>
            </li>
          </ul>
        </nav>

        <!-- 正文 -->
        <article
          ref="contentEl"
          class="doc-body min-w-0 flex-1 rounded-lg border border-line bg-surface p-4 shadow sm:p-6"
          v-html="html"
        />
      </div>
    </div>
  </main>
</template>

<style scoped>
/* Markdown 正文样式。
   用 :deep 因为内容由 v-html 注入，scoped 选择器默认作用不到。
   没有引 typography 插件：只有这一个页面需要，手写一份更可控。 */
.doc-body :deep(h1) {
  @apply mb-4 mt-0 text-2xl font-bold;
}
.doc-body :deep(h2) {
  /* scroll-margin-top 让锚点跳转后标题不被顶部遮住 */
  @apply mb-3 mt-8 scroll-mt-20 border-b border-line pb-2 text-xl font-bold;
}
.doc-body :deep(h3) {
  @apply mb-2 mt-6 scroll-mt-20 text-lg font-semibold;
}
.doc-body :deep(h4) {
  @apply mb-2 mt-4 scroll-mt-20 font-semibold;
}
.doc-body :deep(p) {
  @apply my-3 leading-relaxed;
}
.doc-body :deep(ul) {
  @apply my-3 list-disc space-y-1 pl-6;
}
.doc-body :deep(ol) {
  @apply my-3 list-decimal space-y-1 pl-6;
}
.doc-body :deep(li) {
  @apply leading-relaxed;
}
.doc-body :deep(a) {
  /* 用 accent-text 而非 accent：后者在本项目里是背景色 token，
     当文字色会与 surface / elevated 系背景撞色 */
  @apply text-accent-text underline underline-offset-2;
}
.doc-body :deep(strong) {
  @apply font-semibold;
}
.doc-body :deep(code) {
  @apply rounded bg-elevated px-1.5 py-0.5 font-mono text-[0.85em];
}
.doc-body :deep(pre) {
  @apply my-4 overflow-x-auto rounded-lg bg-elevated p-3;
}
.doc-body :deep(pre code) {
  @apply bg-transparent p-0 text-sm;
}
.doc-body :deep(blockquote) {
  @apply my-4 border-l-4 border-accent/40 bg-elevated/50 py-2 pl-4 pr-2;
}
.doc-body :deep(blockquote p) {
  @apply my-1;
}
.doc-body :deep(hr) {
  @apply my-6 border-line;
}
/* 宽表在窄屏横向滚动，不撑破布局 */
.doc-body :deep(.doc-table-wrap) {
  @apply my-4 overflow-x-auto;
}
.doc-body :deep(table) {
  @apply w-full border-collapse text-sm;
}
.doc-body :deep(th) {
  @apply border border-line bg-elevated px-3 py-2 text-left font-semibold;
}
.doc-body :deep(td) {
  @apply border border-line px-3 py-2 align-top;
}
</style>
