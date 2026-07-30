import { describe, expect, it } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'

/**
 * 移动端适配的强制约束（docs/AuroraMihomo-Frontend-Design.md 第 14 章）。
 *
 * 这些约束靠人工 review 很难守住：改动看起来完全正常，只有在真机窄屏上
 * 才暴露。这里用静态检查把三条最容易退化的规则钉死——
 * 触控热区、安全区让位、视口单位。
 */
const SRC = join(import.meta.dirname, '../src')

/** 递归收集 src 下的 .vue 文件 */
function vueFiles(dir = SRC): string[] {
  const out: string[] = []
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const p = join(dir, entry.name)
    if (entry.isDirectory()) out.push(...vueFiles(p))
    else if (entry.name.endsWith('.vue')) out.push(p)
  }
  return out
}

const rel = (p: string) => p.slice(SRC.length + 1).replace(/\\/g, '/')

describe('移动端约束：视口单位', () => {
  it('高度一律用 dvh，不得使用 vh', () => {
    // 移动浏览器地址栏会伸缩，vh 取的是最大视口高度，
    // 实际渲染会超出屏幕，底部内容被截掉。
    const offenders: string[] = []
    for (const f of vueFiles()) {
      const src = readFileSync(f, 'utf8')
      src.split('\n').forEach((line, i) => {
        // 匹配 h-[80vh] / max-h-[85vh] / min-h-[100vh] 这类任意值写法，
        // 但排除 dvh（负向前查确保 v 前不是 d）
        if (/\b(?:min-|max-)?[hw]-\[[^\]]*(?<!d)vh\b/.test(line)) {
          offenders.push(`${rel(f)}:${i + 1}`)
        }
      })
    }
    expect(offenders, `以下位置应把 vh 改为 dvh：\n${offenders.join('\n')}`).toEqual([])
  })
})

describe('移动端约束：触控热区', () => {
  it('纯图标按钮必须带 .tap-target', () => {
    // 图标按钮没有文字撑开尺寸，只靠 size="icon"/"icon-sm" 的固定尺寸会停在
    // 28~32px，低于 44px 下限。.tap-target 在 pointer:coarse 下补足。
    // 按钮已全部迁移到 shadcn <Button>，图标按钮的标志是 size="icon"/"icon-sm"。
    const offenders: string[] = []
    for (const f of vueFiles()) {
      // ui/ 下是 shadcn 结构组件本身，不含业务图标按钮
      if (rel(f).startsWith('components/ui/')) continue
      const src = readFileSync(f, 'utf8')
      const re = /<Button[\s\S]{0,500}?(?:\/>|<\/Button>)/g
      let m: RegExpExecArray | null
      while ((m = re.exec(src))) {
        const blk = m[0]
        if (!/size="icon(-sm)?"/.test(blk)) continue
        if (!blk.includes('tap-target')) {
          offenders.push(`${rel(f)}:${src.slice(0, m.index).split('\n').length}`)
        }
      }
    }
    expect(offenders, `以下纯图标按钮缺少 .tap-target：\n${offenders.join('\n')}`).toEqual([])
  })

  it('.tap-target 与 shadcn Button 在触屏下有 44px 下限', () => {
    const css = readFileSync(join(SRC, 'assets/main.css'), 'utf8')
    // 取出 pointer: coarse 块
    const i = css.indexOf('@media (pointer: coarse)')
    expect(i, 'main.css 缺少 @media (pointer: coarse) 块').toBeGreaterThan(-1)
    const block = css.slice(i, i + 700)
    // 按钮类操作已全部迁移到 shadcn Button（[data-slot="button"]），
    // 不再有独立的 .btn / .btn-sm 类
    for (const sel of ['.tap-target', "[data-slot='button']"]) {
      expect(block, `${sel} 应在 pointer:coarse 下声明最小尺寸`).toContain(sel)
    }
    // min-h-11 = 2.75rem = 44px
    expect(block).toContain('min-h-11')
    expect(block).toContain('min-w-11')
  })
})

describe('移动端约束：安全区', () => {
  it('viewport 带 viewport-fit=cover 且未禁用缩放', () => {
    const html = readFileSync(join(SRC, '../index.html'), 'utf8')
    expect(html).toContain('viewport-fit=cover')
    // 禁用缩放会拦掉视障用户的放大操作
    expect(html).not.toContain('user-scalable=no')
    expect(html).not.toContain('maximum-scale')
  })

  it('safe-area 辅助类均已定义', () => {
    const css = readFileSync(join(SRC, 'assets/main.css'), 'utf8')
    for (const cls of ['.safe-pt', '.safe-pb', '.safe-py', '.safe-pl', '.safe-inset-tr', '.safe-overlay']) {
      expect(css, `${cls} 未定义`).toContain(cls)
    }
    // 必须带 0px 回退：不支持 env() 的环境下整个 calc 会失效
    expect(css).toContain('env(safe-area-inset-top, 0px)')
  })

  it('贴边的固定/吸附元素已让开安全区', () => {
    // viewport-fit=cover 下这些元素会延伸到刘海与手势条之下
    const cases: Array<[string, string, string]> = [
      ['App.vue', 'fixed inset-y-0 left-0', 'safe-py'],
      ['App.vue', 'sticky top-0 z-20', 'safe-pt'],
      ['components/ToastHost.vue', 'fixed z-[60]', 'safe-inset-tr'],
      // 遮罩现在由 shadcn Dialog（ui/dialog/DialogContent.vue 里的 DialogOverlay）渲染，
      // ModalDialog.vue 自身只是薄封装，不再直接持有 fixed inset-0 元素
      ['components/ui/dialog/DialogContent.vue', 'fixed inset-0', 'safe-overlay'],
    ]
    for (const [file, anchor, expected] of cases) {
      const src = readFileSync(join(SRC, file), 'utf8')
      const line = src.split('\n').find((l: string) => l.includes(anchor))
      expect(line, `${file} 里找不到锚点 "${anchor}"，用例需同步更新`).toBeDefined()
      expect(line, `${file} 的 "${anchor}" 元素应带 ${expected}`).toContain(expected)
    }
  })
})
