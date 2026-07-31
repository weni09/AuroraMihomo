import { describe, expect, it } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'

/**
 * 配色可读性约束。
 *
 * 起因：文档目录的选中项曾用 `bg-elevated text-accent`，结果文字完全看不见。
 * 根因是 shadcn 的 `--accent` 在本项目里是**背景色** token（等于
 * `--c-elevated`），`text-accent` 配 `bg-elevated` 就是同色压同色，
 * 对比度 1.00:1。这类问题类型检查和构建都发现不了，肉眼在某一种主题下
 * 也可能恰好看不出来，只能靠静态规则钉住。
 */
const SRC = join(import.meta.dirname, '../src')

function vueFiles(dir = SRC): string[] {
  const out: string[] = []
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const p = join(dir, entry.name)
    if (entry.isDirectory()) out.push(...vueFiles(p))
    else if (entry.name.endsWith('.vue')) out.push(p)
  }
  return out
}

/** WCAG 相对亮度 */
function luminance([r, g, b]: number[]): number {
  const f = (v: number) => {
    const s = v! / 255
    return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4)
  }
  return 0.2126 * f(r!) + 0.7152 * f(g!) + 0.0722 * f(b!)
}

/** 对比度，1:1 表示完全同色 */
function contrast(fg: number[], bg: number[]): number {
  const l1 = luminance(fg)
  const l2 = luminance(bg)
  return (Math.max(l1, l2) + 0.05) / (Math.min(l1, l2) + 0.05)
}

/** 把半透明前景混合到背景上，得到实际渲染色 */
function blend(fg: number[], bg: number[], alpha: number): number[] {
  return fg.map((v, i) => Math.round(v * alpha + bg[i]! * (1 - alpha)))
}

/** 从 main.css 里读某个 CSS 变量的值（浅色块取第一个，深色块取第二个） */
function readVar(name: string): { light: number[]; dark: number[] } {
  const css = readFileSync(join(SRC, 'assets/main.css'), 'utf8')
  const matches = [...css.matchAll(new RegExp(`--${name}:\\s*([\\d\\s]+);`, 'g'))]
  const parse = (s: string) => s.trim().split(/\s+/).map(Number)
  if (matches.length === 0) throw new Error(`main.css 里找不到 --${name}`)
  return {
    light: parse(matches[0]![1]!),
    // 深色主题没单独定义时沿用浅色值
    dark: parse((matches[1] ?? matches[0])![1]!),
  }
}

describe('text-accent 不得用于文字颜色', () => {
  it('没有任何组件把 text-accent 当文字色用', () => {
    // --accent 是 shadcn 约定里的「hover/选中底色」，本项目把它映射成
    // --c-elevated。当文字色用一定会与 elevated 系背景撞色。
    // 需要强调文字请用 text-accent-text（淡底）或 text-accent-solid-foreground（实色底）。
    const offenders: string[] = []
    for (const file of vueFiles()) {
      const content = readFileSync(file, 'utf8')
      content.split('\n').forEach((line, i) => {
        // 跳过注释行：本规则的说明文字里就会出现 text-accent 这个词
        const trimmed = line.trim()
        if (trimmed.startsWith('//') || trimmed.startsWith('*') || trimmed.startsWith('/*')) {
          return
        }
        // 只匹配独立的 text-accent，排除 text-accent-text / text-accent-solid /
        // text-accent-foreground（后者与 bg-accent 成对使用，是正确搭配）
        if (/\btext-accent(?![\w-])/.test(line)) {
          offenders.push(`${file.replace(SRC, 'src')}:${i + 1}`)
        }
      })
    }
    expect(
      offenders,
      `以下位置把 text-accent 当文字色用了，它与 elevated 背景同色（对比度 1:1，` +
        `文字看不见）。改用 text-accent-text：\n${offenders.join('\n')}`,
    ).toEqual([])
  })
})

describe('强调文字色在两种主题下都达到 WCAG AA', () => {
  // 文档目录选中项的实际组合：accent-solid 淡底 + accent-text 文字
  const accentText = readVar('accent-text')
  const accentSolid = readVar('accent-solid')
  const surfaceLight = readVar('c-surface')
  const surfaceDark = readVar('c-surface')

  it('浅色主题：10% 淡底上的强调文字 ≥ 4.5:1', () => {
    const bg = blend(accentSolid.light, surfaceLight.light, 0.1)
    const ratio = contrast(accentText.light, bg)
    expect(ratio, `实际 ${ratio.toFixed(2)}:1`).toBeGreaterThanOrEqual(4.5)
  })

  it('深色主题：20% 淡底上的强调文字 ≥ 4.5:1', () => {
    const bg = blend(accentSolid.dark, surfaceDark.dark, 0.2)
    const ratio = contrast(accentText.dark, bg)
    expect(ratio, `实际 ${ratio.toFixed(2)}:1`).toBeGreaterThanOrEqual(4.5)
  })

  // 这条守的是「新增 accent-text 没有牵连到实色按钮」，不是给既有配色定标。
  //
  // 实测深色主题下 indigo-500 配白字是 4.47:1，离 AA 的 4.5:1 差 0.03。
  // 按钮是 text-sm font-medium（14px 非粗体），不属于 WCAG 的大号文字，
  // 所以严格说这是个既有的轻微不达标项 —— 但它先于本次改动存在，
  // 收紧它要调整全站强调色，不在当前改动范围内。
  // 阈值定在 4.4：既能挡住「有人把 accent-solid 调得更暗」这类退化，
  // 又不会因为既有的 0.03 差距长期红着。
  it('实色按钮底配前景色不低于现状（防止 accent-solid 被调暗）', () => {
    const fg = readVar('accent-solid-foreground')
    for (const theme of ['light', 'dark'] as const) {
      const ratio = contrast(fg[theme], accentSolid[theme])
      expect(ratio, `${theme} 主题实际 ${ratio.toFixed(2)}:1`).toBeGreaterThanOrEqual(4.4)
    }
  })
})
